package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// FileStorage 本地文件存储
type FileStorage struct {
	saveDir string // 存储根目录
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
	slog.Info("successfully saved %T data to %s \n", data, savePath)
	return nil
}
