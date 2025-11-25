这是一个可拓展的go爬虫程序，添加新爬虫按以下步骤进行，第一步在congfig.json里添加新配置，

第二步在work/crawler里添加配置对应的go文件，实现可参考zhihu.go ,weibo.go，支持自定义结构体，

第三步在stroage中的stroage.go里的save函数里添加对应数据结构的分支，可参考其他分支的实现，

最后在main函数里实现  


    crawlers := []crawler.Crawler{

		&crawler.WeiboCrawler{},
    
		&crawler.BilibiliCrawler{},
    
		&crawler.ZhihuCrawler{},
    
    ....//在这添加对应的爬虫
    
	}

  
  
