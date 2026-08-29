问：

	现在要开发一个镜像于Musicolet app的web系统，服务端主要采用golang+sqlite。
	经历了以下讨论，并记录到文档，如下；

		初步探讨系统基本形态						doc/prompt/first.md
		开始提具体的需求，并反复确认是否理解 		doc/prompt/second.md
		UI布局需求									doc/prompt/layout-prompt.md
		Musicolet 镜像系统技术需求文档				doc/requestment.md
	
	请你结合会话项目"Musicolet复刻"内的所有会话记录，并参照以上文档，出一篇的初期开发计划文档，保存到 doc/roadmap/master/Initial Development Plans.md ，之后再出一份后期剩余开发计划文档，保存到 doc/roadmap/master/Remaining Development Plans.md。
	
	项目地址在 https://github.com/Ltre/MusicoletWeb 你改完直接push就行了。
	
	
	
问：


	现在要开发一个镜像于Musicolet app的web系统，服务端主要采用golang+sqlite，默认监听端口4001。
	经历了以下讨论，并记录到文档，如下；

		初步探讨系统基本形态						doc/prompt/first.md
		开始提具体的需求，并以grill-me反复确认 		doc/prompt/second.md
		UI布局需求									doc/prompt/layout-prompt.md
		Musicolet 镜像系统技术需求文档				doc/requestment.md
		MusicoletWeb 初期开发计划					Initial Development Plans.md
		MusicoletWeb 后期剩余开发计划				Remaining Development Plans.md
	
	请你结合会话项目"Musicolet复刻"内的所有会话记录，并参照以上文档，完成初期开发计划的任务。
	
	启动脚本要参考 scripts/example 目录中的脚本（这两个example脚本时从FmlySys拷贝过来的，有go每次启动前重新编译的过程，还设置了一些按需的常量，在windows版中还有网络代理配置）编写，代码最终保存到 scripts 目录中我已创建好的两个空脚本文件。