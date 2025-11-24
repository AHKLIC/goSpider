package crawler

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github/AHKLIC/Spider/work/config"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type BilibiliCrawler struct {
	BaseCrawler
}

type BilibiliHotItem struct {
	HotItem
}

// 顶层 JSON 结构体（仅保留 data 字段，其他字段按需添加）
type BiliHotResponse struct {
	Code    int         `json:"code"`    // 响应状态码（0 成功）
	Message string      `json:"message"` // 响应信息
	Data    BiliHotData `json:"data"`    // 核心数据
}

// Data 结构体（仅保留 list 字段）
type BiliHotData struct {
	List []BiliHotItem `json:"list"` // 热搜视频列表
}

// 单个视频项结构体（仅保留需要的字段，嵌套 stat 子结构体）
type BiliHotItem struct {
	Title       string      `json:"title"`         // 视频标题
	ShortLinkV2 string      `json:"short_link_v2"` // 视频短链接
	Stat        BiliHotStat `json:"stat"`          // 统计数据（view/reply 在此处）
}

// 统计数据结构体（仅保留 view 和 reply 字段）
type BiliHotStat struct {
	View  int `json:"view"`  // 播放量
	Reply int `json:"reply"` // 评论数
}

func (bili *BilibiliCrawler) Name() string {
	return "bilibili"
}

func (bili *BilibiliCrawler) Init(cfg config.CrawlerConfig) error {
	bili.BaseCrawler.Cfg = cfg
	return nil
}

func (bili *BilibiliCrawler) Crawl() ([]interface{}, error) {

	slog.Info("crawling", "source", bili.Name(), "url", bili.Cfg.URL)

	const maxRetry uint32 = 2
	for retry := uint32(0); retry < maxRetry; retry++ {
		hotItems, err := bili.tryCrawl()
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
		// 爬取失败，判断是否需要刷新 url
		//刷新url
		bili.Cfg.URL = bili.GetSignUrl()
		// 短暂延迟（避免频繁请求触发反爬）
		time.Sleep(5 * time.Second)
	}

	// 重试 2 次后仍失败，返回最终错误
	return nil, fmt.Errorf("%s crawl failed after %d retries url: %s  (cookie may be blocked by anti-spider)", bili.Name(), maxRetry, bili.Cfg.URL)

}
func (bili *BilibiliCrawler) tryCrawl() ([]interface{}, error) {
	jsonBytes, err := bili.GetJsonBybts()
	if err != nil {
		slog.Error("Failed to get Bili JSON data", "error", err)
		return nil, fmt.Errorf("get json data: %w", err)
	}

	//  解析 JSON 字节流到结构体
	var response BiliHotResponse
	err = json.Unmarshal(jsonBytes, &response)
	if err != nil {
		slog.Error("Failed to unmarshal Bili JSON", "error", err, "json_length", len(jsonBytes))
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}

	//提取目标字段，封装为自定义结构体（如 HotItem）
	var result []interface{}
	for _, item := range response.Data.List {
		curhotvalue := item.Stat.View + item.Stat.Reply*10
		// 封装为你的热点数据结构体（根据项目现有结构调整）
		hotItem := &BilibiliHotItem{
			HotItem: HotItem{
				Title:     item.Title,
				URL:       item.ShortLinkV2,
				Source:    "bilibili",
				HotValue:  fmt.Sprintf("%d", curhotvalue),
				CrawledAt: time.Now(),
			},
		}
		result = append(result, hotItem)
	}

	slog.Info("Bili crawl success", "item_count", len(result))
	return result, nil
}

// 函数实现参考 https://github.com/SocialSisterYi/bilibili-API-collect/blob/master/docs/video_ranking/ranking.md
func (bili *BilibiliCrawler) GetSignUrl() string {
	u, err := url.Parse("https://api.bilibili.com/x/web-interface/ranking/v2")
	if err != nil {
		panic(err)
	}
	time.Sleep(2 * time.Second)
	err = Sign(u)
	if err != nil {
		slog.Error(" bili GetSignUrl in Sign error ", "error", err)
	}
	err = bili.updateUrlInConfigFile(bili.Name(), u.String())
	if err != nil {
		slog.Error("updateUrlInConfigFile", "error", err)
	}
	return u.String()
	// 获取 wbi 时未修改 header
	// 但实际使用签名后的 url 时发现风控较为严重
}

// Sign 为链接签名
func Sign(u *url.URL) error {
	return wbiKeys.Sign(u)
}

// Update 无视过期时间更新
func Update() error {
	return wbiKeys.Update()
}

func Get() (wk WbiKeys, err error) {
	if err = wk.update(false); err != nil {
		return WbiKeys{}, err
	}
	return wbiKeys, nil
}

var wbiKeys WbiKeys

type WbiKeys struct {
	Img            string
	Sub            string
	Mixin          string
	lastUpdateTime time.Time
}

// Sign 为链接签名
func (wk *WbiKeys) Sign(u *url.URL) (err error) {
	if err = wk.update(false); err != nil {
		return err
	}

	values := u.Query()

	values = removeUnwantedChars(values, '!', '\'', '(', ')', '*') // 必要性存疑?

	values.Set("wts", strconv.FormatInt(time.Now().Unix(), 10))

	// [url.Values.Encode] 内会对参数排序,
	// 且遍历 map 时本身就是无序的
	hash := md5.Sum([]byte(values.Encode() + wk.Mixin)) // Calculate w_rid
	values.Set("w_rid", hex.EncodeToString(hash[:]))
	u.RawQuery = values.Encode()
	return nil
}

// Update 无视过期时间更新
func (wk *WbiKeys) Update() (err error) {
	return wk.update(true)
}

// update 按需更新
func (wk *WbiKeys) update(purge bool) error {
	if !purge && time.Since(wk.lastUpdateTime) < time.Hour {
		return nil
	}

	// 测试下来不用修改 header 也能过
	resp, err := http.Get("https://api.bilibili.com/x/web-interface/nav")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	nav := Nav{}
	err = json.Unmarshal(body, &nav)
	if err != nil {
		return err
	}

	if nav.Code != 0 && nav.Code != -101 { // -101 未登录时也会返回两个 key
		return fmt.Errorf("unexpected code: %d, message: %s", nav.Code, nav.Message)
	}
	img := nav.Data.WbiImg.ImgUrl
	sub := nav.Data.WbiImg.SubUrl
	if img == "" || sub == "" {
		return fmt.Errorf("empty image or sub url: %s", body)
	}

	// https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png
	imgParts := strings.Split(img, "/")
	subParts := strings.Split(sub, "/")

	// 7cd084941338484aae1ad9425b84077c.png
	imgPng := imgParts[len(imgParts)-1]
	subPng := subParts[len(subParts)-1]

	// 7cd084941338484aae1ad9425b84077c
	wbiKeys.Img = strings.TrimSuffix(imgPng, ".png")
	wbiKeys.Sub = strings.TrimSuffix(subPng, ".png")

	wbiKeys.mixin()
	wbiKeys.lastUpdateTime = time.Now()
	return nil
}

func (wk *WbiKeys) mixin() {
	var mixin [32]byte
	wbi := wk.Img + wk.Sub
	for i := range mixin { // for i := 0; i < len(mixin); i++ {
		mixin[i] = wbi[mixinKeyEncTab[i]]
	}
	wk.Mixin = string(mixin[:])
}

var mixinKeyEncTab = [...]int{
	46, 47, 18, 2, 53, 8, 23, 32,
	15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19,
	29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61,
	26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63,
	57, 62, 11, 36, 20, 34, 44, 52,
}

func removeUnwantedChars(v url.Values, chars ...byte) url.Values {
	b := []byte(v.Encode())
	for _, c := range chars {
		b = bytes.ReplaceAll(b, []byte{c}, nil)
	}
	s, err := url.ParseQuery(string(b))
	if err != nil {
		panic(err)
	}
	return s
}

type Nav struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Ttl     int    `json:"ttl"`
	Data    struct {
		WbiImg struct {
			ImgUrl string `json:"img_url"`
			SubUrl string `json:"sub_url"`
		} `json:"wbi_img"`

		// ......
	} `json:"data"`
}
