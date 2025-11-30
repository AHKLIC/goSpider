package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"github/AHKLIC/Spider/work/config"
	"log/slog"
	"reflect"

	"time"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type DbStorage struct {
	mongoClient *mongo.Client // MongoDB 客户端
	mongodbName string        // MongoDB 数据库名
	redisClient *redis.Client
	maxBatches  int // 每个source在redis里保留的最大批次（这里设为6）

}

func (db *DbStorage) MongoDeleteManage(deleteInterval time.Duration, stop <-chan struct{}) {

	if db.mongoClient == nil {
		slog.Error("MongoDeleteManage初始化失败", "原因", "mongoClient未初始化")
		panic("mongoClient未初始化，无法启动定时清理")
	}
	if db.mongodbName == "" {
		slog.Error("MongoDeleteManage初始化失败", "原因", "mongodbName为空")
		panic("mongodbName为空，无法启动定时清理")
	}

	// 初始化定时器：首次执行延迟1秒（避免服务启动时立即执行），之后每deleteInterval执行一次
	ticker := time.NewTicker(deleteInterval)
	defer ticker.Stop() // 函数退出时停止定时器，避免资源泄漏

	slog.Info(
		"MongoDeleteManage启动成功",
		"数据库名", db.mongodbName,
		"清理间隔", deleteInterval.String(),
		"保留数据范围", "仅当天（Asia/Shanghai时区）数据",
	)

	if err := db.cleanNonTodayData(); err != nil {
		slog.Error(
			"MongoDeleteManage清理失败",
			"数据库名", db.mongodbName,
			"错误信息", err.Error(),
		)
	} else {
		slog.Info(
			"MongoDeleteManage本次清理完成",
			"数据库名", db.mongodbName,
		)
	}
	// 循环执行：等待定时器触发或停止信号
	for {
		select {
		case <-ticker.C:

			if err := db.cleanNonTodayData(); err != nil {
				slog.Error(
					"MongoDeleteManage清理失败",
					"数据库名", db.mongodbName,
					"错误信息", err.Error(),
				)
			} else {
				slog.Info(
					"MongoDeleteManage本次清理完成",
					"数据库名", db.mongodbName,
				)
			}

		case <-stop:
			// 接收停止信号（如服务优雅退出）
			slog.Info(
				"MongoDeleteManage收到停止信号",
				"数据库名", db.mongodbName,
				"操作", "正在优雅退出",
			)
			return
		}
	}
}

func (db *DbStorage) saveToDb(source string, data interface{}) error {

	if err := db.saveToMongoDB(source, data); err != nil {
		return fmt.Errorf("save to mongo failed: %w", err)
	}
	if err := db.saveToRedis(source, data); err != nil {
		return fmt.Errorf("save to redis failed: %w", err)
	}
	return nil
}

// saveToMongoDB 修复后支持任意切片类型的 MongoDB 写入方法
func (db *DbStorage) saveToMongoDB(source string, data interface{}) error {
	ctx := context.Background()
	coll := db.mongoClient.Database(db.mongodbName).Collection(source)
	if err := db.ensureCrawledAtIndex(ctx, coll); err != nil {
		slog.Warn(
			"索引创建失败，不影响写入但删除操作效率可能较低",
			"集合名", source,
			"错误信息", err.Error(),
		)
	}
	// 反射判断是否为切片类型
	rv := reflect.ValueOf(data)
	switch rv.Kind() {
	case reflect.Slice:
		// 批量插入：将任意切片转为 []interface{}
		docs := make([]interface{}, rv.Len())
		for i := range docs {
			docs[i] = rv.Index(i).Interface()
		}
		_, err := coll.InsertMany(ctx, docs)
		if err != nil {
			return fmt.Errorf("batch insert (source: %s): %w", source, err)
		}
		slog.Info("saved items to MongoDB", "count", rv.Len(), "source", source, "collection", source)
	default:
		// 单条插入：处理单个对象
		_, err := coll.InsertOne(ctx, data)
		if err != nil {
			return fmt.Errorf("single insert (source: %s): %w", source, err)
		}
		slog.Info("saved 1 item to MongoDB", "source", source, "collection", source)
	}

	return nil
}

func (db *DbStorage) saveToRedis(source string, data interface{}) error {
	ctx := context.Background()
	now := time.Now()
	// 1. 生成数据键（source + 精确到秒的时间戳，避免同一秒重复键）
	dataKey := fmt.Sprintf("hot:%s:%s", source, now.Format("20060102150405"))
	// 生成ZSet键（每个source对应一个ZSet，用于管理批次）
	zsetKey := fmt.Sprintf("hot:zset:%s", source)
	// ZSet的score：毫秒级时间戳（用于排序，确保最新数据排在前面）
	timestampMs := now.UnixMilli()

	// 2. 序列化数据为JSON字符串（兼容所有Redis版本）
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data failed: %w", err)
	}

	// 3. 使用Redis事务保证原子性（写入数据 + 更新ZSet + 淘汰旧数据）
	// WATCH ZSet键，防止并发修改导致批次计数错误
	watchErr := db.redisClient.Watch(ctx, func(tx *redis.Tx) error {
		// 步骤1：写入数据键（设置24小时过期，双重保障：主动淘汰 + 过期自动删除）
		if err := tx.Set(ctx, dataKey, string(jsonBytes), 1*time.Hour).Err(); err != nil {
			return fmt.Errorf("set data key failed: %w", err)
		}

		// 步骤2：将数据键添加到ZSet（score=毫秒级时间戳）
		// ZADD 时若数据键已存在（同一秒重复写入），会更新score（不影响排序）
		if err := tx.ZAdd(ctx, zsetKey, &redis.Z{Score: float64(timestampMs), Member: dataKey}).Err(); err != nil {
			return fmt.Errorf("add to zset failed: %w", err)
		}

		// 步骤3：获取当前批次数量
		batchCount, err := tx.ZCard(ctx, zsetKey).Result()
		if err != nil {
			return fmt.Errorf("get zset count failed: %w", err)
		}

		// 步骤4：若超过最大批次（6批），淘汰最旧的批次
		if batchCount > int64(db.maxBatches) {
			// 计算需要删除的旧批次数量（超过部分）
			delCount := batchCount - int64(db.maxBatches)
			// ZRANGE + WITHSCORES：获取最旧的delCount个数据键（score最小的）
			oldMembers, err := tx.ZRangeWithScores(ctx, zsetKey, 0, delCount-1).Result()
			if err != nil {
				return fmt.Errorf("get old batches failed: %w", err)
			}

			// 提取旧数据键，准备删除
			oldDataKeys := make([]interface{}, len(oldMembers))
			for i, member := range oldMembers {
				oldDataKeys[i] = member.Member
			}

			// 批量删除旧数据键 + 从ZSet中移除
			pipe := tx.Pipeline()
			// 删除旧数据键
			// Redis Del expects []string; 将 []interface{} 转为 []string
			oldKeys := make([]string, len(oldDataKeys))
			for i, v := range oldDataKeys {
				switch t := v.(type) {
				case string:
					oldKeys[i] = t
				default:
					oldKeys[i] = fmt.Sprint(t)
				}
			}
			pipe.Del(ctx, oldKeys...)
			// 从ZSet中删除旧成员（ZRem 接受 members ...interface{}）
			pipe.ZRem(ctx, zsetKey, oldDataKeys...)
			// 执行批量操作
			if _, err := pipe.Exec(ctx); err != nil {
				return fmt.Errorf("delete old batches failed: %w", err)
			}
			slog.Info(
				"清理redis旧数据完成",
				"source", source,
				"delete_count", delCount,
				"old_batch_keys", oldDataKeys,
			)
		}

		return nil
	}, zsetKey) // 监听ZSet键，并发修改时会重试

	if watchErr != nil {
		return fmt.Errorf("transaction failed: %w", watchErr)
	}
	slog.Info(
		"数据成功保存到 Redis",
		"redis_key", dataKey,
		"source", source,
		"current_batches", db.maxBatches,
	)
	return nil
}

// ensureCrawledAtIndex 检查并创建索引
func (db *DbStorage) ensureCrawledAtIndex(ctx context.Context, coll *mongo.Collection) error {
	// 定义索引模型：
	// 1. 索引字段：hotitem.crawledat（升序，1=升序，-1=降序）
	indexModel := mongo.IndexModel{
		Keys: bson.D{{Key: "hotitem.crawledat", Value: 1}},
		Options: options.Index().
			SetSparse(true).                  // 稀疏索引：仅包含有该字段的文档（节省空间）
			SetHidden(false).                 // 显式指定索引可见（MongoDB 4.4+ 支持，默认 false，兼容新版本）
			SetName("idx_hotitem_crawledat"), // 自定义索引名（便于后续管理/删除）

	}

	_, err := coll.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		// 关键修复：通过错误码 85 判断“索引已存在”
		if mongoErr, ok := err.(mongo.CommandError); ok && mongoErr.Code == 85 {
			slog.Debug(
				"索引已存在，无需重复创建",
				"集合名", coll.Name(),
				"索引名", indexModel.Options.Name,
			)
			return nil // 忽略该错误，直接返回成功
		}
		return fmt.Errorf("create index idx_hotitem_crawledat: %w", err)
	}

	return nil
}

// cleanNonTodayData 核心清理逻辑：删除所有集合中非当天的数据
func (db *DbStorage) cleanNonTodayData() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute) // 超时时间30分钟（应对大数据量）
	defer cancel()

	// 1. 获取数据库中所有集合名称
	dbInstance := db.mongoClient.Database(db.mongodbName)
	collectionNames, err := dbInstance.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return fmt.Errorf("获取集合列表失败: %w", err)
	}

	if len(collectionNames) == 0 {
		slog.Info(
			"无需要清理的集合",
			"数据库名", db.mongodbName,
			"原因", "数据库中未找到任何集合",
		)
		return nil
	}

	now := time.Now()
	nowShanghai := now.In(config.ShanghaiLoc)

	// 手动构造上海时区的当天起始时间
	shanghaiTodayStart := time.Date(
		nowShanghai.Year(),
		nowShanghai.Month(),
		nowShanghai.Day(),
		0, 0, 0, 0, // 00:00:00.000
		config.ShanghaiLoc,
	)

	utcTodayStart := shanghaiTodayStart.UTC()
	utcTodayStartStr := utcTodayStart.Format("2006-01-02 15:04:05.999")

	slog.Info(
		"开始执行清理逻辑",
		"数据库名", db.mongodbName,
		"上海时区当天起始时间", shanghaiTodayStart.Format("2006-01-02 15:04:05.999"),
		"转换后UTC起始时间", utcTodayStartStr,
		"清理条件", fmt.Sprintf("hotitem.crawledat < %s（UTC时间，对应上海时区前一天16:00前的数据）", utcTodayStartStr),
		"待清理集合数", len(collectionNames),
	)

	// 3. 遍历所有集合，执行删除
	for _, collName := range collectionNames {
		coll := dbInstance.Collection(collName)

		// 构建查询条件：hotitem.crawledat < UTC时间（上海时区当天00:00对应的UTC时间）
		filter := bson.M{
			"hotitem.crawledat": bson.M{
				"$lt": utcTodayStart, // 匹配存储的UTC时间
			},
		}

		// 执行删除（批量删除，返回删除数量）
		// 索引提示：如果hotitem.crawledat有索引，删除速度会大幅提升（必做优化）
		deleteResult, err := coll.DeleteMany(
			ctx,
			filter,
			options.Delete().SetHint(bson.M{"hotitem.crawledat": 1}),
		)
		if err != nil {
			// 单个集合删除失败，记录错误并继续清理其他集合
			slog.Error(
				"集合清理失败",
				"数据库名", db.mongodbName,
				"集合名", collName,
				"错误信息", err.Error(),
			)
			continue
		}

		// 结构化日志：打印每个集合的清理结果
		slog.Info(
			"集合清理完成",
			"数据库名", db.mongodbName,
			"集合名", collName,
			"删除数据量", deleteResult.DeletedCount,
			"状态", func() string {
				if deleteResult.DeletedCount > 0 {
					return "清理成功"
				}
				return "无需要删除的数据"
			}(),
		)
	}

	return nil
}
