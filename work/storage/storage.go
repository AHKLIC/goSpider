package storage

import (
	"encoding/json"
	"fmt"
	"github/AHKLIC/Spider/work/crawler"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStorage 本地文件存储
type FileStorage struct {
	saveDir string     // 存储根目录
	mu      sync.Mutex // 并发安全锁
}

func NewFileStorage(saveDir string) (*FileStorage, error) {
	// 创建存储目录（不存在则创建）
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return nil, err
	}
	return &FileStorage{saveDir: saveDir}, nil
}

// Save 主存储方法：按类型分发，按 source+日期 分类存储
func (f *FileStorage) Save(items []interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(items) == 0 {
		return nil
	}

	// 按第一个item的类型分发（假设同批次items类型一致）
	switch items[0].(type) {
	case *crawler.HotItem:
		return f.saveHotItems(items)
	case *crawler.WeiboHotItem:
		return f.saveWeiboHotItems(items)
	case *crawler.BilibiliHotItem:
		return f.saveBiliHotItems(items)
	default:
		return fmt.Errorf("unsupported item type: %T", items[0])
	}
}

// saveHotItems 存储基础 HotItem 类型：按 source 分目录
func (f *FileStorage) saveHotItems(items []interface{}) error {
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
		if err := f.saveToFile(source, hotItems); err != nil {
			return fmt.Errorf("save HotItem (source=%s) failed: %w", source, err)
		}
	}
	return nil
}

// saveBiliHotItems 存储扩展 BilibiliHotItem 类型：按 source 分目录
func (f *FileStorage) saveBiliHotItems(items []interface{}) error {
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
		if err := f.saveToFile(source, biliItems); err != nil {
			return fmt.Errorf("save BilibiliHotItem (source=%s) failed: %w", source, err)
		}
	}
	return nil
}

func (f *FileStorage) saveWeiboHotItems(items []interface{}) error {
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
		if err := f.saveToFile(source, weiboItems); err != nil {
			return fmt.Errorf("save WeiboHotItem (source=%s) failed: %w", source, err)
		}
	}
	return nil
}

// saveToFile 通用存储工具：创建目录 + 写入 JSON 文件
func (f *FileStorage) saveToFile(source string, data interface{}) error {
	// 1. 构建完整存储路径：saveDir/source/20060102_hot.json
	dateStr := time.Now().Format("20060102150405")
	filename := fmt.Sprintf("%s_hot.json", dateStr)
	// 子目录路径（根目录 + source）
	sourceDir := filepath.Join(f.saveDir, source)
	// 完整文件路径
	savePath := filepath.Join(sourceDir, filename)

	// 2. 创建子目录（不存在则创建，支持多级目录）
	if err := os.MkdirAll(sourceDir, 0755); err != nil { // 0755：读/写/执行权限
		return fmt.Errorf("create source dir %s failed: %w", sourceDir, err)
	}

	// 3. 创建/覆盖文件
	file, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file %s failed: %w", savePath, err)
	}
	defer file.Close()

	// 4. JSON 格式化写入（保留缩进，与旧函数一致）
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encode JSON failed: %w", err)
	}

	fmt.Printf("successfully saved %T data to %s \n", data, savePath)
	return nil
}
