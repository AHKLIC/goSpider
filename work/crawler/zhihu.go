package crawler

import (
	"encoding/json"
	"fmt"
	"github/AHKLIC/Spider/work/config"
	"log/slog"
	"strings"
	"time"
)

type ZhihuCrawler struct {
	BaseCrawler
}
type ZhihuHotItem struct {
	HotItem
}

// 顶层响应结构体：匹配 JSON 顶层 {"data": [{}]}
type ZhihuHotResponse struct {
	Data []ZhihuHotData `json:"data"` // 首字母大写（导出），json标签对应顶层键 "data"
}

// 单个热点数据结构体：对应 data 数组中的每个元素
type ZhihuHotData struct {
	Target     ZhihuInfoItem `json:"target"`      // 首字母大写，对应 JSON 的 "target" 键
	DetailText string        `json:"detail_text"` // 首字母大写，对应 JSON 的 "detail_text" 键（热度值）
}

// 热点详情结构体：对应 target 子对象
type ZhihuInfoItem struct {
	Title string `json:"title"` // 首字母大写，对应 JSON 的 "title" 键（标题）
	URL   string `json:"url"`   // 首字母大写，对应 JSON 的 "url" 键（链接）
}

func (zhihu *ZhihuCrawler) Name() string {
	return "zhihu"
}

func (zhihu *ZhihuCrawler) Init(cfg config.CrawlerConfig) error {
	zhihu.BaseCrawler.Cfg = cfg
	return nil
}

func (zhihu *ZhihuCrawler) Crawl() ([]interface{}, error) {

	slog.Info("crawling", "source", zhihu.Name(), "url", zhihu.Cfg.URL)

	const maxRetry uint32 = 1
	for retry := uint32(0); retry < maxRetry; retry++ {
		hotItems, err := zhihu.tryCrawl()
		if err == nil && len(hotItems) > 0 {
			return hotItems, nil
		}

		// 短暂延迟（避免频繁请求触发反爬）
		time.Sleep(5 * time.Second)
	}

	// 重试 2 次后仍失败，返回最终错误
	return nil, fmt.Errorf("%s crawl failed after %d retries url: %s  (cookie may be blocked by anti-spider)", zhihu.Name(), maxRetry, zhihu.Cfg.URL)

}

//换api接口

func (zhihu *ZhihuCrawler) tryCrawl() ([]interface{}, error) {
	jsonBytes, err := zhihu.GetJsonBybts()
	if err != nil {
		slog.Error("Failed to get zhihu JSON data", "error", err)
		return nil, fmt.Errorf("get json data: %w", err)
	}

	//  解析 JSON 字节流到结构体
	var response ZhihuHotResponse
	err = json.Unmarshal(jsonBytes, &response)
	if err != nil {
		slog.Error("Failed to unmarshal zhihu JSON", "error", err, "json_length", len(jsonBytes))
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}

	//提取目标字段，封装为自定义结构体
	var result []interface{}
	for _, item := range response.Data {
		apiURL := item.Target.URL
		wwwURL := strings.Replace(apiURL, "api.zhihu.com/questions", "www.zhihu.com/question", 1)
		// 封装为你的热点数据结构体（根据项目现有结构调整）
		hotItem := &ZhihuHotItem{
			HotItem: HotItem{
				Title:     item.Target.Title,
				URL:       wwwURL,
				Source:    "zhihu",         // 来源标记为知乎
				HotValue:  item.DetailText, // 热度值（如 "1849 万热度"）
				CrawledAt: time.Now(),      // 爬取时间
			},
		}
		result = append(result, hotItem)
	}

	slog.Info("zhihu crawl success", "item_count", len(result))
	return result, nil

}

//doc处理
// func (zhihu *ZhihuCrawler) tryCrawl() ([]interface{}, error) {

// 	doc, err := zhihu.GetDoc() //html document execute
// 	if err != nil {
// 		return nil, fmt.Errorf("get doc: %w", err)
// 	}

// 	var hotItems []interface{}
// 	doc.Find("ListShortcut HotList-list section").Each(func(index int, sel *goquery.Selection) {
// 		// 跳过表头（第1行是表头）
// 		if index == 0 {
// 			return
// 		}

// 		// 提取标题和链接（最新DOM结构：td:nth-child(2) a）
// 		titleSel := sel.Find("HotItem-content a")
// 		title, ok := titleSel.Attr("title")
// 		href, exists := titleSel.Attr("href")
// 		if !exists || !ok {
// 			slog.Warn("Skip invalid item", "index", index, "reason", "no title/url")
// 			return
// 		}

// 		// 补全绝对URL
// 		url := href
// 		if !strings.HasPrefix(href, "http") {
// 			url = "https://www.zhihu.com" + href
// 		}

// 		hotStr := strings.TrimSpace(sel.Find("HotItem-content #text").Text())

// 		// 组装数据
// 		hotItems = append(hotItems, &ZhihuHotItem{
// 			HotItem: HotItem{
// 				Title:     title,
// 				URL:       url,
// 				Source:    zhihu.Name(),
// 				HotValue:  hotStr,
// 				CrawledAt: time.Now(),
// 			},
// 		})
// 	})

// 	// 校验结果
// 	if len(hotItems) == 0 {
// 		// 若仍无数据，打印HTML前100字节排查结构
// 		html, _ := doc.Html()
// 		slog.Error("No data crawled", "html_preview", html[:min(len(html), 100)])
// 		return nil, fmt.Errorf("no valid hot items found(cookie expired or page changed)")
// 	}

// 	slog.Info(" zhihu crawl success", "item_count", len(hotItems))
// 	return hotItems, nil
// }
