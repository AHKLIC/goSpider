package scheduler

import (
	"github/AHKLIC/Spider/work/crawler"
	"github/AHKLIC/Spider/work/storage"
	"log/slog"
	"sync"
	"time"

	"github/AHKLIC/Spider/work/config"
)

// Scheduler 定时调度器
type Scheduler struct {
	crawlers       []crawler.Crawler   // 所有爬取器
	storage        *storage.AllStorage // 存储实例
	interval       time.Duration       // 爬取间隔（如1小时）
	wg             sync.WaitGroup      // 并发控制
	stopChan       chan struct{}       // 停止信号
	deleteInterval time.Duration       // 删除间隔

}

// NewScheduler 创建调度器  interval单位为秒且应该为min deleteInterval为数据库删除间隔
func NewScheduler(interval time.Duration, deleteInterval time.Duration, crawlers []crawler.Crawler) (*Scheduler, error) {
	allStorage, err := storage.NewAllStorage(true, false)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		crawlers:       crawlers,
		storage:        allStorage,
		interval:       interval,
		stopChan:       make(chan struct{}),
		deleteInterval: deleteInterval,
	}, nil
}

// Start 启动调度器（阻塞）
func (s *Scheduler) Start() {
	slog.Info("scheduler started", "interval", s.interval.Seconds())

	// 首次爬取（立即执行）
	s.runCrawlers()
	s.wg.Add(1)
	go s.storage.MongoDeleteManage(s.deleteInterval, &s.wg, s.stopChan) // 启动数据库清理协程
	// 定时执行
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runCrawlers()
		case <-s.stopChan:
			slog.Info("scheduler stopped")
			return
		}
	}
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	s.storage.Close()
}

// runCrawlers 执行所有爬取器（并发）
func (s *Scheduler) runCrawlers() {
	slog.Info("starting crawl task")
	s.wg.Add(len(s.crawlers))
	for _, c := range s.crawlers {
		go func(crawler crawler.Crawler) {
			defer s.wg.Done()
			lastTime := crawler.GetLastCrawleTime()
			name := crawler.Name()
			if lastTime.IsZero() {

				curConfig, ok := config.GetCrawlerConfigByName(name)

				if !ok {
					slog.Error("no config found for crawler", "name", name)
					return
				}
				crawler.Init(curConfig)
			} else {
				timeNow := time.Now()
				interval := crawler.GetInterval()
				if timeNow.Sub(lastTime)*time.Second < interval {
					slog.Info("skipping crawl due to interval not reached", "source", name)
					return
				}
			}

			items, err := crawler.Crawl()
			if err != nil {
				slog.Error("crawl failed please update cookies or url", "source", name, "error", err)
				return
			}
			crawler.SetLastCrawleTime(time.Now())
			slog.Info("crawl success", "source", name, "count", len(items))
			if err := s.storage.Save(items); err != nil {
				slog.Error("save failed", "source", name, "error", err)
			}
		}(c) // 注意：循环变量捕获，需传参
	}

	s.wg.Wait()
	slog.Info("crawl task finished")
}
