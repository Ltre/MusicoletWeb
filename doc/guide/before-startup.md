
问：


	目前切换到另一个从零开发的分支dev-2609A-GPTCHAT
	你看看这个版本如何启动以及启动前的准备



问：





我检查的是当前 `dev-2609A-GPTCHAT` 分支。这个版本可以通过自带脚本启动；我没有启动服务或修改文件，目前工作区是干净的。

## Windows 启动前准备

需要：

- Go 1.23 以上；当前机器的 Go 1.27 可用
- Git
- 可访问 Go 模块源的网络
- 配置文件 `data\config.env`
- 手机上的 TOTP 验证器，例如 Google Authenticator、Microsoft Authenticator

当前 `data\config.env` 尚不存在。建议在第一次执行脚本前手动创建，内容如下：

```dotenv
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=<管理员强密码>
MUSICOLET_ADMIN_TOTP_SECRET=<Base32格式的TOTP密钥>
MUSICOLET_SESSION_KEY=<独立的64位十六进制随机值>
MUSICOLET_MASTER_KEY=<独立的64位十六进制随机值>
MUSICOLET_AGENT_TOKEN=<独立的64位十六进制随机值>
MUSICOLET_PUBLIC_BASE_URL=http://localhost:4001
```

其中：

- `ADMIN_PASSWORD` 必填。
- `ADMIN_TOTP_SECRET` 必填；首次登录就需要六位动态验证码。
- `SESSION_KEY` 用于登录会话，修改后现有会话会失效。
- `MASTER_KEY` 用于加密数据库中的 Agent 令牌，必须妥善备份，部署后不要随意更换。
- `AGENT_TOKEN` 首次初始化实际上也是必填的，建议至少32字节随机值。
- 三个密钥不要共用。

PowerShell 可以生成十六进制密钥：

```powershell
function New-HexSecret([int]$Bytes = 32) {
    $buffer = New-Object byte[] $Bytes
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($buffer)
    $rng.Dispose()
    -join ($buffer | ForEach-Object { $_.ToString('x2') })
}

New-HexSecret
New-HexSecret
New-HexSecret
```

TOTP 密钥需要是 Base32 格式，并应在启动前录入手机验证器。系统时间也要同步准确。

## Windows 启动

在仓库根目录执行：

```powershell
.\scripts\win-dev.start.cmd
```

脚本会：

1. 检查 Go 和 Git。
2. 执行 `go mod tidy`。
3. 下载并校验 Go 依赖。
4. 编译为 `bin\musicoletweb.exe`。
5. 前台启动服务。
6. 默认监听 `0.0.0.0:4001`。

启动后访问：

- 管理页面：`http://localhost:4001/`
- 健康检查：`http://localhost:4001/api/health`
- 公开播放页：`http://localhost:4001/now-playing`

需要注意：脚本虽然会在配置不存在时生成空模板，但生成后不会暂停，而是继续下载、编译，最后因为空配置启动失败。因此最好提前填写配置。

脚本还硬编码了以下本地代理：

```text
HTTP_PROXY=http://127.0.0.1:58591
HTTPS_PROXY=http://127.0.0.1:58591
ALL_PROXY=socks5://127.0.0.1:51837
```

这些代理必须正在运行，否则 Go 依赖下载可能失败；不使用这些代理时，需要调整脚本中的代理配置。

## Linux 启动

先准备相同的 `data/config.env`，生产部署建议：

```dotenv
MUSICOLET_BIND_HOST=127.0.0.1
MUSICOLET_PORT=4001
MUSICOLET_PUBLIC_BASE_URL=https://musicolet.miku.us
```

然后执行：

```bash
chmod 600 data/config.env
bash scripts/linux-alyhk.start.sh
```

该脚本会编译 `bin/musicoletweb` 并以前台方式启动。仓库中的 Nginx示例位于 `deploy/nginx/musicolet-alyhk.conf`，默认反向代理到 `127.0.0.1:4001`。

## 数据和 Agent

- 当前的 `data/musicolet-backup-2026-08-30 06-46-42.zip` 不会在启动时自动导入。
- 服务启动并登录后，需要通过 Web UI 上传该备份文件，系统才会建立导入流程。
- 手机 Agent 不是服务启动的必要条件，可以后续再部署。
- ARM64 Agent 单独通过以下脚本构建：

```bash
bash scripts/build-agent-arm64.sh
```

输出为：

```text
bin/musicolet-agent-arm64
```

总体而言，这个分支的启动流程已经完整；首次部署最关键的是提前正确准备 `config.env`、录入 TOTP、保存好主密钥，以及确认 Windows 脚本中的本地代理可用。