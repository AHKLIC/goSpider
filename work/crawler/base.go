package crawler

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// NewRequest 创建HTTP请求（复用Fiddler抓到的完整请求头）
func (b *BaseCrawler) NewRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	// 1.只有初次才注入LocalCookie（从配置文件读取，已拼接好的完整Cookie）
	if b.Cfg.Cookie != "" {
		req.Header.Set("Cookie", b.Cfg.Cookie)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6")

	return req, nil
}

// GetClient 创建带 CookieJar 的 HTTP 客户端（自动管理匿名 Cookie） 复用客户端
func (b *BaseCrawler) GetClient() *http.Client {

	if b.ClientToUpdateCookie != nil {
		return b.ClientToUpdateCookie
	} else {
		// 初始化 CookieJar（允许所有域名存储 Cookie）
		jar, err := cookiejar.New(&cookiejar.Options{
			PublicSuffixList: nil, // 匿名 Cookie 无需域名校验
		})
		if err != nil {
			slog.Error("Failed to create cookiejar", "error", err)
			return &http.Client{Timeout: 15 * time.Second}
		}

		return &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar, // 自动管理 Cookie
			Transport: &http.Transport{
				DisableCompression: true,
			},
		}
	}
}

// 新增：清空指定域名的 Cookie（用于过期后重置）
func (b *BaseCrawler) ClearCookie(domain string) error {
	u, err := url.Parse(domain)
	if err != nil {
		return err
	}
	// 向 CookieJar 写入空 Cookie，覆盖旧 Cookie（实现清空）
	b.GetClient().Jar.SetCookies(u, []*http.Cookie{})
	slog.Info("Cleared expired cookie", "domain", domain)
	return nil
}

// func (b *BaseCrawler) UpdateCookie(name string, targetURL string) error {

// 	client := b.GetClient()
// 	u, err := url.Parse(targetURL)
// 	if err != nil {
// 		return fmt.Errorf("failed to parse URL: %v", err)
// 	}
// 	cookies := client.Jar.Cookies(u)
// 	if len(cookies) == 0 {
// 		return fmt.Errorf("no cookies found for %s", targetURL)
// 	}

// 	// 将Cookie格式化为字符串
// 	var cookieStrs []string
// 	for _, cookie := range cookies {
// 		cookieStrs = append(cookieStrs, cookie.Name+"="+cookie.Value)
// 	}
// 	cookieString := strings.Join(cookieStrs, "; ")
// 	return b.updateCookieInConfigFile(name, cookieString)
// }

// GetDoc 发送GET请求并返回goquery文档（HTML解析通用逻辑）
func (b *BaseCrawler) GetDoc() (*goquery.Document, error) {
	req, err := b.NewRequest("GET", b.Cfg.URL)
	if err != nil {
		slog.Error("Create request failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := b.GetClient()
	b.ClientToUpdateCookie = client
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("HTTP request failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Non-200 status", "url", b.Cfg.URL, "code", resp.StatusCode)
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	// 步骤1：读取原始数据（不管什么编码，先读出来）
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Read raw body failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("read raw body: %w", err)
	}
	slog.Info("Raw body info", "url", b.Cfg.URL, "length", len(rawBody))

	// 步骤2：强制尝试 gzip 解压（微博99%是 gzip）
	var uncompressedBody []byte
	gzReader, err := gzip.NewReader(bytes.NewReader(rawBody))
	if err == nil {
		// 解压成功：说明是 gzip 格式
		defer gzReader.Close()
		uncompressedBody, err = io.ReadAll(gzReader)
		if err != nil {
			slog.Error("Gzip decompress failed", "url", b.Cfg.URL, "error", err)
			return nil, fmt.Errorf("gzip decompress: %w", err)
		}
		slog.Info("Gzip decompress success", "url", b.Cfg.URL, "uncompressed_length", len(uncompressedBody))
	} else {
		// 解压失败：认为是无压缩数据
		slog.Warn("Not gzip format, use raw body", "url", b.Cfg.URL, "gzip_err", err)
		uncompressedBody = rawBody
	}

	// 步骤3：验证解压结果（是否为正常HTML）
	htmlStr := string(uncompressedBody[:min(len(uncompressedBody), 200)])
	if !strings.Contains(htmlStr, "<html") && !strings.Contains(htmlStr, "<HTML") {
		slog.Error("Decompressed data is not HTML", "url", b.Cfg.URL, "preview", htmlStr)
		return nil, fmt.Errorf("decompressed data is not HTML")
	}

	// 步骤4：解析HTML
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(uncompressedBody))
	if err != nil {
		slog.Error("Parse HTML failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	return doc, nil
}

func (b *BaseCrawler) GetJsonBybts() ([]byte, error) {
	req, err := b.NewRequest("GET", b.Cfg.URL)
	if err != nil {
		slog.Error("Create request failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := b.GetClient()
	b.ClientToUpdateCookie = client
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("HTTP request failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Non-200 status", "url", b.Cfg.URL, "code", resp.StatusCode)
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	// 步骤1：读取原始数据（不管什么编码，先读出来）
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Read raw body failed", "url", b.Cfg.URL, "error", err)
		return nil, fmt.Errorf("read raw body: %w", err)
	}
	slog.Info("Raw body info", "url", b.Cfg.URL, "length", len(rawBody))

	var uncompressedBody []byte
	gzReader, err := gzip.NewReader(bytes.NewReader(rawBody))
	if err == nil {
		// 解压成功：说明是 gzip 格式
		defer gzReader.Close()
		uncompressedBody, err = io.ReadAll(gzReader)
		if err != nil {
			slog.Error("Gzip decompress failed", "url", b.Cfg.URL, "error", err)
			return nil, fmt.Errorf("gzip decompress: %w", err)
		}
		slog.Info("Gzip decompress success", "url", b.Cfg.URL, "uncompressed_length", len(uncompressedBody))
	} else {
		// 解压失败：认为是无压缩数据
		slog.Warn("Not gzip format, use raw body", "url", b.Cfg.URL, "gzip_err", err)
		uncompressedBody = rawBody
	}

	return uncompressedBody, nil

}

//。。。。直接处理返回的json

// func (b *BaseCrawler) updateCookieInConfigFile(name, cookie string) error {
// 	filePath, err := filepath.Abs(config.ConfigPath)
// 	if _, err := os.Stat(filePath); os.IsNotExist(err) {
// 		return fmt.Errorf("config file does not exist: %s", filePath)
// 	}
// 	if err != nil {
// 		return fmt.Errorf("failed to turn filePath in updateCookieInConfigFile: %v", err)
// 	}

// 	// 更新对应爬虫的Cookie
// 	found := config.UpdateGlobaCookie(name, cookie)
// 	if !found {
// 		return fmt.Errorf("crawler with name '%s' not found in config file", name)
// 	}
// 	newconfig := config.GetGlobalConfig()

// 	config.ConfigMu.Lock()
// 	defer config.ConfigMu.Unlock()

// 	// 创建临时文件
// 	tmpFilePath := filePath + ".tmp"
// 	tmpFile, err := os.Create(tmpFilePath)
// 	if err != nil {
// 		return fmt.Errorf("failed to create temp config file: %v", err)
// 	}

// 	// 编码到临时文件
// 	encoder := json.NewEncoder(tmpFile)
// 	encoder.SetIndent("", "  ")
// 	if err := encoder.Encode(newconfig); err != nil {
// 		tmpFile.Close()
// 		os.Remove(tmpFilePath) // 删除临时文件
// 		return fmt.Errorf("failed to encode config: %v", err)
// 	}

// 	// 确保数据写入磁盘
// 	if err := tmpFile.Sync(); err != nil {
// 		tmpFile.Close()
// 		os.Remove(tmpFilePath)
// 		return fmt.Errorf("failed to sync temp file: %v", err)
// 	}
// 	tmpFile.Close()

// 	// 重命名临时文件为正式文件（原子操作）
// 	if err := os.Rename(tmpFilePath, filePath); err != nil {
// 		os.Remove(tmpFilePath)
// 		return fmt.Errorf("failed to rename temp file: %v", err)
// 	}

// 	return nil
// }

// 辅助函数：取最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetInterval 获取爬取间隔（实现Crawler接口）
func (b *BaseCrawler) GetInterval() time.Duration {
	return time.Duration(b.Cfg.Interval) * time.Second
}
func (b *BaseCrawler) GetLastCrawleTime() time.Time {

	return b.LastCrawleTime
}
func (b *BaseCrawler) SetLastCrawleTime(t time.Time) {
	b.LastCrawleTime = t
}
