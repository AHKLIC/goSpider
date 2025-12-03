package main

import (
	"fmt"
	mylog "github/AHKLIC/Spider/slog"
	"github/AHKLIC/Spider/work/config"
	"github/AHKLIC/Spider/work/crawler"
	"github/AHKLIC/Spider/work/scheduler"
	"time"
)

type Control struct {
}

func main() {

	config.InitTimeZone() // 初始化时区
	// 初始化轮转日志（日志目录：./logs，前缀：crawler）
	rotatingWriter, logInitErr := mylog.InitRotatingLogger("./logs", "crawler") //控制台+文件输出
	if logInitErr != nil {
		panic(fmt.Sprintf("init logger failed: %v", logInitErr))
	}
	defer rotatingWriter.Close() // 程序退出时关闭文件

	err := config.Init("./app-config/config.json")
	if err != nil {
		panic(err)
	}

	crawlers := []crawler.Crawler{
		&crawler.WeiboCrawler{},
		&crawler.ZhihuCrawler{},
		&crawler.BilibiliCrawler{},
	}

	minSecond := config.Mininterval //取配置中的最小间隔
	dHour := 24
	scheduler, err := scheduler.NewScheduler(time.Duration(minSecond)*time.Second, time.Duration(dHour)*time.Hour, crawlers)
	defer scheduler.Stop()
	if err != nil {
		panic(err)
	}

	scheduler.Start()

}
