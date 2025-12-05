package crawler

import (
	"github/AHKLIC/Spider/work/config"
	"net/http"
	"time"
)

// HotItem 热点数据结构体基本格式
type HotItem struct {
	Title     string    // 热点标题
	URL       string    // 热点链接
	Source    string    // 来源站点（如"weibo"、"zhihu"）
	HotValue  string    // 热度值
	CrawledAt time.Time // 爬取时间
}

//extra struct

// Crawler 爬取接口（新增Init方法用于初始化配置）
type Crawler interface {
	Name() string                    // 站点名称（与配置文件name对应）
	Init(config.CrawlerConfig) error // 初始化配置（注入URL、Cookie）
	Crawl() ([]interface{}, error)   // 核心爬取逻辑
	GetInterval() time.Duration      // 获取爬取间隔
	GetLastCrawleTime() time.Time    // 获取上次爬取时间
	SetLastCrawleTime(t time.Time)   // 更新爬取时间
}

// BaseCrawler 基础爬虫结构体（封装公共逻辑）
type BaseCrawler struct {
	Cfg                  config.CrawlerConfig // 绑定的配置
	LastCrawleTime       time.Time            // 上次爬取时间戳
	ClientToUpdateCookie *http.Client         // HTTP客户端
}
