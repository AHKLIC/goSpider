package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"

	"time"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/mongo"
)

type DbStorage struct {
	mongoClient *mongo.Client // MongoDB 客户端
	mongodbName string        // MongoDB 数据库名
	redisClient *redis.Client
	maxBatches  int // 每个source在redis里保留的最大批次（这里设为6）

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
