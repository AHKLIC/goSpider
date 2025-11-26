package storage

import (
	"context"
	"fmt"
	"github/AHKLIC/Spider/work/config"
	"github/AHKLIC/Spider/work/crawler"
	"os"
	"sync"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AllStorage struct {
	FileStorage
	DbStorage
	isOpenDb   bool
	isOpenFile bool
	mu         sync.Mutex // 并发安全锁
}

// 参数1为是否开启数据库存储，参数2为是否开启文件存储，默认开启文件存储关闭数据库存储
func NewAllStorage(dbOpen bool, fileOpen bool) (*AllStorage, error) {

	globalConfig := config.GetGlobalConfig() //获取存储配置
	saveDir := globalConfig.SaveDir
	mongodbName := globalConfig.MongoDBName
	mongoUrl := globalConfig.MongoURL

	var mongoClient *mongo.Client
	var redisClient *redis.Client
	if fileOpen {
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			return nil, err
		}
	}
	if dbOpen {
		mongoCli, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoUrl))
		if err != nil {
			return nil, fmt.Errorf("init mongo client failed: %w", err)
		}
		if err := mongoCli.Ping(context.Background(), nil); err != nil {
			return nil, fmt.Errorf("ping mongo failed: %w", err)
		}
		redisCli := redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "redis123456", // 你的Redis密码
			DB:       0,
		})
		if redisCli == nil {

			return nil, fmt.Errorf("connect to redis failed")

		}
		redisClient = redisCli

		mongoClient = mongoCli

	}

	return &AllStorage{
		FileStorage: FileStorage{saveDir: saveDir},
		isOpenDb:    dbOpen,
		isOpenFile:  fileOpen,
		DbStorage: DbStorage{
			mongoClient: mongoClient,
			mongodbName: mongodbName,
			redisClient: redisClient,
			maxBatches:  6, //每一数据源只保留6批最新的缓存
		},
	}, nil

}

// Save 主存储方法：按类型分发，按 source+日期 分类存储
func (all *AllStorage) Save(items []interface{}) error {
	all.mu.Lock()
	defer all.mu.Unlock()

	if len(items) == 0 {
		return nil
	}

	// 按第一个item的类型分发
	switch items[0].(type) {
	case *crawler.HotItem:
		return all.saveHotItems(items)
	case *crawler.WeiboHotItem:
		return all.saveWeiboHotItems(items)
	case *crawler.BilibiliHotItem:
		return all.saveBiliHotItems(items)
	case *crawler.ZhihuHotItem:
		return all.saveZhihuHotItems(items)
	default:
		return fmt.Errorf("unsupported item type: %T", items[0])
	}
}

// saveHotItems 存储基础 HotItem 类型：按 source 分目录
func (all *AllStorage) saveHotItems(items []interface{}) error {
	// 1. 转换类型并分组（按 source 分组，确保同目录下是同一来源数据）
	sourceItemsMap := make(map[string][]*crawler.HotItem)
	for _, item := range items {
		hotItem, ok := item.(*crawler.HotItem)
		if !ok {
			fmt.Printf("warning: skip invalid HotItem type: %T\n", item)
			continue
		}
		// 按 source 分组（source 为空时存入 "unknown" 目录）
		source := hotItem.Source
		if source == "" {
			source = "unknown"
		}
		sourceItemsMap[source] = append(sourceItemsMap[source], hotItem)
	}

	// 2. 遍历分组，逐个存储
	for source, hotItems := range sourceItemsMap {
		if all.isOpenFile {
			if err := all.saveToFile(source, hotItems); err != nil {
				return fmt.Errorf("save HotItem to file (source=%s) failed: %w", source, err)
			}
		}

		if all.isOpenDb {
			if err := all.saveToDb(source, hotItems); err != nil {
				return fmt.Errorf("save HotItem to DB(source=%s) failed: %w", source, err)
			}
		}

	}
	return nil
}

// saveBiliHotItems 存储扩展 BilibiliHotItem 类型：按 source 分目录
func (all *AllStorage) saveBiliHotItems(items []interface{}) error {
	// 1. 转换类型并分组（WeiboHotItem 继承 HotItem，直接取 Source）
	sourceItemsMap := make(map[string][]*crawler.BilibiliHotItem)
	for _, item := range items {
		biliItem, ok := item.(*crawler.BilibiliHotItem)
		if !ok {
			fmt.Printf("warning: skip invalid BilibiliHotItem type: %T\n", item)
			continue
		}
		// 按 source 分组（复用 HotItem 的 Source 字段）
		source := biliItem.Source
		if source == "" {
			source = "unknown"
		}
		sourceItemsMap[source] = append(sourceItemsMap[source], biliItem)
	}

	// 2. 遍历分组，逐个存储
	for source, biliItems := range sourceItemsMap {
		if all.isOpenFile {
			if err := all.saveToFile(source, biliItems); err != nil {
				return fmt.Errorf("save BilibiliHotItem to file(source=%s) failed: %w", source, err)
			}
		}
		if all.isOpenDb {
			if err := all.saveToDb(source, biliItems); err != nil {
				return fmt.Errorf("save BilibiliHotItem to DB (source=%s) failed: %w", source, err)
			}
		}
	}
	return nil
}

func (all *AllStorage) saveWeiboHotItems(items []interface{}) error {
	sourceItemsMap := make(map[string][]*crawler.WeiboHotItem)
	for _, item := range items {
		weiboItem, ok := item.(*crawler.WeiboHotItem)
		if !ok {
			fmt.Printf("warning: skip invalid WeiboHotItem type: %T\n", item)
			continue
		}
		// 按 source 分组（复用 HotItem 的 Source 字段）
		source := weiboItem.Source
		if source == "" {
			source = "unknown"
		}
		sourceItemsMap[source] = append(sourceItemsMap[source], weiboItem)
	}

	// 2. 遍历分组，逐个存储
	for source, weiboItems := range sourceItemsMap {
		if all.isOpenFile {
			if err := all.saveToFile(source, weiboItems); err != nil {
				return fmt.Errorf("save WeiboHotItem to file(source=%s) failed: %w", source, err)
			}
		}
		if all.isOpenDb {
			if err := all.saveToDb(source, weiboItems); err != nil {
				return fmt.Errorf("save WeiboHotItem to DB(source=%s) failed: %w", source, err)
			}
		}
	}
	return nil
}

func (all *AllStorage) saveZhihuHotItems(items []interface{}) error {
	sourceItemsMap := make(map[string][]*crawler.ZhihuHotItem)
	for _, item := range items {
		zhihuItem, ok := item.(*crawler.ZhihuHotItem)
		if !ok {
			fmt.Printf("warning: skip invalid ZhihuHotItem type: %T\n", item)
			continue
		}
		// 按 source 分组（复用 HotItem 的 Source 字段）
		source := zhihuItem.Source
		if source == "" {
			source = "unknown"
		}
		sourceItemsMap[source] = append(sourceItemsMap[source], zhihuItem)
	}

	// 2. 遍历分组，逐个存储
	for source, zhihuItems := range sourceItemsMap {
		if all.isOpenFile {
			if err := all.saveToFile(source, zhihuItems); err != nil {
				return fmt.Errorf("save ZhihuHotItem to file(source=%s) failed: %w", source, err)
			}
		}
		if all.isOpenDb {
			if err := all.saveToDb(source, zhihuItems); err != nil {
				return fmt.Errorf("save ZhihuHotItem to DB(source=%s) failed: %w", source, err)
			}
		}
	}
	return nil
}
