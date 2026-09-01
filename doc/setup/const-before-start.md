
问：


	第一次部署的准备工作，应该写到readme文档里



答：

	已在 [README.md](/E:/Workspaces/github/Ltre/MusicoletWeb/README.md:25) 增加完整的“首次部署准备”，包括：

	- Go/Git、磁盘与数据目录准备
	- 首次生成 `data/config.env`
	- 每个配置项的用途和长度要求
	- PowerShell/Linux 随机密钥生成方式
	- Windows 开发与 Linux 生产模式差异
	- TOTP、TLS、反向代理、防火墙与权限
	- 首次启动后的健康检查、登录和 V1 导入检查表

	同时确保 Windows/Linux 脚本分别强制开发/生产安全模式。格式和脚本检查通过，仍未提交、未暂存。


问：


	？？？？？？？？？？？？？？？？



答：



	已修复启动脚本的首启流程：

	- 首次创建配置后立即退出，不再下载、编译后才报错。
	- 已有配置缺项时，在构建前指出具体字段。
	- Windows 与 Linux 脚本均已同步修复。
	- 已验证当前空配置会立即提示密码缺失。
	- 未提交、未暂存。

	现在编辑 [data/config.env](</E:/Workspaces/github/Ltre/MusicoletWeb/data/config.env>)，至少填写：

	```dotenv
	MUSICOLET_ADMIN_USERNAME=admin
	MUSICOLET_ADMIN_PASSWORD=你的强密码
	MUSICOLET_SESSION_KEY=至少32字符的随机密钥
	```

	Windows 开发模式下，TOTP 和 Agent Token 暂时可以留空。填写后重新运行：

	```bat
	scripts\win-dev.start.cmd
	```

	对应修复也已记录到 [devlog-2609A-features.md](/E:/Workspaces/github/Ltre/MusicoletWeb/doc/devlog/devlog-2609A-features.md)。