这是一个可拓展的go爬虫程序现已支持mongodb，添加新爬虫按以下步骤进行，第一步在app-config/congfig.json里添加新配置，

第二步在work/crawler里添加配置对应的go文件，实现可参考zhihu.go ,weibo.go，支持自定义结构体，

第三步在stroage中的allstroage.go里的Save函数里添加对应数据结构的分支，可参考其他分支的实现，

最后在main函数里实现  


    crawlers := []crawler.Crawler{

		&crawler.WeiboCrawler{},
    
		&crawler.BilibiliCrawler{},
    
		&crawler.ZhihuCrawler{},
    
    ....//在这添加对应的爬虫
    
	}

 现已支持接入reids哨兵集群

 现已支持docker部署，请依据ready-docker文件中的内容进行部署
 ！！！！请注意新建文件的权限与路径，本项目支持wsl和liunx，示例的挂载目录在windows中






  
  
