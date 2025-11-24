package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	ConfigPath string
	ConfigMu   sync.Mutex //protect update with config.json
)

// CrawlerConfig 单个爬虫的配置结构体
type CrawlerConfig struct {
	Name     string `json:"name"`     // 站点名称（与爬虫实现对应）
	URL      string `json:"url"`      // 爬取地址
	Cookie   string `json:"cookie"`   // Cookie信息（可选）
	Interval int    `json:"interval"` // 爬取间隔（秒）
}

// GlobalConfig 全局配置结构体
type GlobalConfig struct {
	Crawlers       []CrawlerConfig `json:"crawlers"`        // 所有爬虫配置
	GlobalInterval int             `json:"global_interval"` // 全局爬取间隔（秒）
	SaveDir        string          `json:"save_dir"`        // 数据存储目录
}

var globalConfig GlobalConfig
var muGlobalConfig sync.RWMutex

// Init 初始化配置（读取config.json）
func Init(configPath string) error {
	ConfigPath = configPath
	// 处理配置文件路径（支持相对路径）
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}

	// 读取配置文件
	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解析JSON
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&globalConfig); err != nil {
		return err
	}

	// 填充默认值（若单个爬虫未配置interval则用全局值）
	for i := range globalConfig.Crawlers {
		if globalConfig.Crawlers[i].Interval == 0 {
			globalConfig.Crawlers[i].Interval = globalConfig.GlobalInterval
		}
	}

	return nil
}

// GetGlobalConfig 获取全局配置
func GetGlobalConfig() GlobalConfig {
	return globalConfig
}

// GetCrawlerConfigBy_name 根据站点名称获取单个爬虫配置
func GetCrawlerConfigByName(name string) (CrawlerConfig, bool) {
	muGlobalConfig.RLock()
	defer muGlobalConfig.RUnlock()
	for _, cfg := range globalConfig.Crawlers {
		if cfg.Name == name {
			return cfg, true
		}
	}
	return CrawlerConfig{}, false
}

func UpdateGlobaCookie(name string, cookie string) bool {
	muGlobalConfig.Lock()
	defer muGlobalConfig.Unlock()
	for i := range globalConfig.Crawlers {
		if globalConfig.Crawlers[i].Name == name {
			globalConfig.Crawlers[i].Cookie = cookie
			return true
		}

	}
	return false

}
func UpdateGlobaURL(name string, url string) bool {
	muGlobalConfig.Lock()
	defer muGlobalConfig.Unlock()
	for i := range globalConfig.Crawlers {
		if globalConfig.Crawlers[i].Name == name {
			globalConfig.Crawlers[i].URL = url
			return true
		}

	}
	return false

}
