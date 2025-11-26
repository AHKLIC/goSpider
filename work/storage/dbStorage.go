package storage

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"go.mongodb.org/mongo-driver/mongo"
)

type DbStorage struct {
	mongoClient *mongo.Client // MongoDB 客户端
	mongodbName string        // MongoDB 数据库名

}

func (db *DbStorage) saveToDb(source string, data interface{}) error {

	if err := db.saveToMongoDB(source, data); err != nil {
		return fmt.Errorf("save to mongo failed: %w", err)
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
