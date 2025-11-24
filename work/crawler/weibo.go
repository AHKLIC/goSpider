package crawler

import (
	"fmt"
	"strings"

	"github/AHKLIC/Spider/work/config"
	"log/slog"
	"regexp"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// cookie endtime 2026 11 23
// WeiboCrawler 微博爬虫实例，嵌入BaseCrawler继承公共能力
type WeiboCrawler struct {
	BaseCrawler // 嵌入基础爬虫结构体，实现继承

}

type WeiboHotItem struct {
	HotItem
}

// Name 实现Crawler接口，返回站点名称（需与config.json中name一致）
func (w *WeiboCrawler) Name() string {
	return "weibo"
}

// Init 实现Crawler接口，初始化微博爬虫配置
func (w *WeiboCrawler) Init(cfg config.CrawlerConfig) error {
	// 将配置绑定到基础爬虫结构体
	w.BaseCrawler.Cfg = cfg
	// 可添加微博专属的初始化逻辑（如校验URL格式）
	if !strings.Contains(cfg.URL, "s.weibo.com/top/summary") {
		return fmt.Errorf("invalid weibo hot url: %s", cfg.URL)
	}
	return nil
}

// 核心爬取方法（含 Cookie 过期自动刷新）
func (w *WeiboCrawler) Crawl() ([]interface{}, error) {

	slog.Info("crawling", "source", w.Name(), "url", w.Cfg.URL)
	// 最多重试 2 次（首次 + 1 次 Cookie 刷新重试）
	const maxRetry uint32 = 2
	for retry := uint32(0); retry < maxRetry; retry++ {
		hotItems, err := w.tryCrawl()
		if err == nil && len(hotItems) > 0 {
			// if retry > 0 {
			// 	err := w.UpdateCookie(w.Name(), w.Cfg.URL)
			// 	if err != nil {
			// 		slog.Error("In UpdateCookie", "error", err)
			// 	}
			// }

			// 爬取成功，返回结果
			return hotItems, nil
		}

		// 爬取失败，判断是否需要刷新 Cookie
		slog.Warn("Crawl failed, preparing to refresh cookie",
			"retry", retry+1,
			"error", err)

		// 清空旧 Cookie（关键：让下次请求重新获取新匿名 Cookie）
		if err := w.ClearCookie(w.Cfg.URL); err != nil {
			slog.Error("Failed to clear cookie", "error", err)
			continue
		}

		// 短暂延迟（避免频繁请求触发反爬）
		time.Sleep(2 * time.Second)
	}

	// 重试 2 次后仍失败，返回最终错误
	return nil, fmt.Errorf("crawl failed after %d retries (cookie may be blocked by anti-spider)", maxRetry)
}

func (w *WeiboCrawler) tryCrawl() ([]interface{}, error) {

	doc, err := w.GetDoc() //html document execute
	if err != nil {
		return nil, fmt.Errorf("get doc: %w", err)
	}

	var hotItems []interface{}
	// 关键修改：使用最新的选择器 #pl_top_realtimehot table tbody tr
	doc.Find("#pl_top_realtimehot table tbody tr").Each(func(index int, sel *goquery.Selection) {
		// 跳过表头（第1行是表头）
		if index == 0 {
			return
		}

		judgeHot := sel.Find("td:nth-child(1)").Text()
		cleanedIndex := regexp.MustCompile(`[^0-9]`).ReplaceAllString(judgeHot, "")
		if cleanedIndex == "" {
			slog.Warn("Skip invalid item", "index", index, "reason", "no rank number")
			return
		}
		// 提取标题和链接（最新DOM结构：td:nth-child(2) a）
		titleSel := sel.Find("td:nth-child(2) a")
		title := strings.TrimSpace(titleSel.Text())
		href, exists := titleSel.Attr("href")
		if !exists || title == "" {
			slog.Warn("Skip invalid item", "index", index, "reason", "no title/url")
			return
		}

		// 补全绝对URL
		url := href
		if !strings.HasPrefix(href, "http") {
			url = "https://s.weibo.com" + href
		}

		// 提取热度值（最新DOM结构：td:nth-child(3)）
		hotStr := strings.TrimSpace(sel.Find("td:nth-child(2) span").Text())

		// 组装数据
		hotItems = append(hotItems, &WeiboHotItem{
			HotItem: HotItem{
				Title:     title,
				URL:       url,
				Source:    w.Name(),
				HotValue:  hotStr,
				CrawledAt: time.Now(),
			},
		})
	})

	// 校验结果
	if len(hotItems) == 0 {
		// 若仍无数据，打印HTML前100字节排查结构
		html, _ := doc.Html()
		slog.Error("No data crawled", "html_preview", html[:min(len(html), 100)])
		return nil, fmt.Errorf("no valid hot items found(cookie expired or page changed)")
	}

	slog.Info("Weibo crawl success", "item_count", len(hotItems))
	return hotItems, nil
}
