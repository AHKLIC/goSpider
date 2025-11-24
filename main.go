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

	// 初始化轮转日志（日志目录：./logs，前缀：crawler）
	rotatingWriter, logInitErr := mylog.InitRotatingLogger("./logs", "crawler") //控制台+文件输出
	if logInitErr != nil {
		panic(fmt.Sprintf("init logger failed: %v", logInitErr))
	}
	defer rotatingWriter.Close() // 程序退出时关闭文件

	err := config.Init("config.json")
	if err != nil {
		panic(err)
	}

	crawlers := []crawler.Crawler{
		&crawler.WeiboCrawler{},
		&crawler.BilibiliCrawler{},
	}
	globalConfig := config.GetGlobalConfig()
	second := globalConfig.GlobalInterval
	scheduler, err := scheduler.NewScheduler(time.Duration(second)*time.Second, globalConfig.SaveDir, crawlers)
	if err != nil {
		panic(err)
	}
	scheduler.Start()

}
