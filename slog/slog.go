package mylog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// multiHandler 基于切片的多输出 Handler 实现（内部存储多个子 Handler）
type multiHandler []slog.Handler

// NewMultiHandler 创建多输出 Handler（与标准库 slog.NewMultiHandler 同名，直接替换使用）
// 功能：将日志同时转发给所有传入的子 Handler（如控制台、文件）
func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	// 过滤空 Handler，避免无效循环
	nonNilHandlers := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			nonNilHandlers = append(nonNilHandlers, h)
		}
	}
	return multiHandler(nonNilHandlers)
}

// Enabled 实现 slog.Handler 接口：判断日志级别是否需要记录（只要有一个子 Handler 支持就返回 true）
func (h multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for i := range h {
		if h[i].Enabled(ctx, l) {
			return true
		}
	}
	return false
}

// Handle 实现 slog.Handler 接口：转发日志到所有子 Handler（仅转发给支持该级别的 Handler）
func (h multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for i := range h {
		// 再次检查级别，确保只发给需要的 Handler（避免无效调用）
		if h[i].Enabled(ctx, r.Level) {
			// 克隆 Record：防止某个 Handler 修改日志内容，影响其他 Handler
			clonedRecord := r.Clone()
			if err := h[i].Handle(ctx, clonedRecord); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// 合并所有错误（无错误则返回 nil）
	return errors.Join(errs...)
}

// WithAttrs 实现 slog.Handler 接口：给所有子 Handler 添加全局属性（如 app="crawler"）
func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h))
	for i := range h {
		// 每个子 Handler 都添加相同的属性，返回新的 Handler 实例（不修改原 Handler）
		newHandlers = append(newHandlers, h[i].WithAttrs(attrs))
	}
	return multiHandler(newHandlers)
}

// WithGroup 实现 slog.Handler 接口：给所有子 Handler 添加日志分组（如 group="weibo"）
func (h multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h))
	for i := range h {
		// 每个子 Handler 都添加相同的分组，返回新的 Handler 实例
		newHandlers = append(newHandlers, h[i].WithGroup(name))
	}
	return multiHandler(newHandlers)
}

// RotatingFileWriter 按日期轮转的文件 Writer
type RotatingFileWriter struct {
	logDir      string   // 日志目录
	prefix      string   // 日志文件名前缀（如 "crawler"）
	currentFile *os.File // 当前打开的文件
	currentDate string   // 当前日期（格式：20060102）
}

// NewRotatingFileWriter 创建轮转日志 Writer
func NewRotatingFileWriter(logDir, prefix string) (*RotatingFileWriter, error) {
	// 创建日志目录
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	r := &RotatingFileWriter{
		logDir: logDir,
		prefix: prefix,
	}

	// 初始化当前日期和文件
	if err := r.rotate(); err != nil {
		return nil, err
	}

	return r, nil
}
func InitRotatingLogger(logDir, prefix string) (*RotatingFileWriter, error) {
	// 创建轮转 Writer（日志目录：./logs，前缀：crawler）
	rotatingWriter, err := NewRotatingFileWriter(logDir, prefix)
	if err != nil {
		return nil, fmt.Errorf("create rotating writer failed: %w", err)
	}

	// 配置 slog.Handler（双输出：控制台+轮转文件）
	// 目标时间格式
	targetTimeFormat := "2006/01/02 15:04:05"

	// 1. 控制台 Handler：文本格式 + 时间格式化
	consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		// 自定义文本格式的时间输出（仅 TextHandler 支持）
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 替换 "time" 字段的格式
			if a.Key == "time" {
				// 将时间值转换为自定义格式的字符串
				t, ok := a.Value.Any().(time.Time)
				if ok {
					return slog.String("time", t.Format(targetTimeFormat))
				}
			}
			return a
		},
	})
	fileHandler := slog.NewJSONHandler(rotatingWriter, &slog.HandlerOptions{Level: slog.LevelInfo})
	multiHandler := NewMultiHandler(consoleHandler, fileHandler)

	// 5. （可选）添加全局属性（所有日志都会包含该属性）
	// multiHandler = multiHandler.WithAttrs([]slog.Attr{
	// 	slog.String("app", "hot-crawler"),
	// 	slog.String("env", "production"),
	// }).(multiHandler)

	// 设置全局 logger
	slog.SetDefault(slog.New(multiHandler))
	slog.Info("Rotating logger initialized", "log_dir", logDir, "prefix", prefix)
	return rotatingWriter, nil
}

// Write 实现 io.Writer 接口：写入前检查是否需要轮转
func (r *RotatingFileWriter) Write(p []byte) (n int, err error) {
	// 检查当前日期是否变化
	today := time.Now().Format("20060102")
	if today != r.currentDate {
		if err := r.rotate(); err != nil {
			slog.Error("Rotate log file failed", "error", err)
			return 0, err
		}
	}

	// 写入当前文件
	return r.currentFile.Write(p)
}

// rotate 轮转日志：关闭旧文件，创建新文件
func (r *RotatingFileWriter) rotate() error {
	// 1. 关闭旧文件（若存在）
	if r.currentFile != nil {
		if err := r.currentFile.Close(); err != nil {
			return err
		}
	}

	// 2. 更新当前日期
	r.currentDate = time.Now().Format("20060102")

	// 3. 生成新文件名（前缀_日期.log）
	filename := fmt.Sprintf("%s_%s.log", r.prefix, r.currentDate)
	filePath := filepath.Join(r.logDir, filename)

	// 4. 打开新文件（创建+追加模式）
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// 5. 更新当前文件
	r.currentFile = file
	slog.Info("Log file rotated", "new_file", filePath)
	return nil
}

// Close 关闭当前文件
func (r *RotatingFileWriter) Close() error {
	if r.currentFile != nil {
		return r.currentFile.Close()
	}
	return nil
}
