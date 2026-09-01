# MusicoletWeb

MusicoletWeb 是一个以 Musicolet Backup 为来源、可在浏览器中浏览和播放曲库的自托管镜像系统。服务端使用 Go + SQLite，默认监听 `0.0.0.0:4001`；手机侧 Termux Agent 只提供按路径读取音频的能力，不修改手机文件。

## 当前初期能力

- 归档、解密、校验并解析 Musicolet Backup ZIP；
- 保存不可变 Musicolet Version、可编辑 working state、Server M 和 Git 审计历史；
- 第二次及后续导入的 BASE / OURS / THEIRS 语义合并、冲突处理和 stale resolution；
- Musicolet 风格的 Queue、正在播放、文件夹、专辑、艺术家、风格、歌单和搜索视图；
- Queue/Playlist 去重、来源关联 Queue、播放位置记忆、Favorite、Metadata 和播放统计 Server M；
- 密码 + TOTP + Session + CSRF 管理员认证；
- Termux arm64 只读 Agent、服务端媒体缓存和浏览器音频播放；
- 只读公开“正在播放”页面：`/now-playing`。

## 运行要求

- Go 1.27；
- Git CLI（服务端通过隔离适配层写入 bare repository）；
- Windows 10/11 或常见 Linux；
- Termux Agent 需要能访问配置的 Android 媒体目录。

SQLite 使用 pure-Go 驱动，不要求本机安装 SQLite CLI 或 CGO。

## 首次部署准备

### 1. 准备运行环境

1. 把仓库放到一个服务账号可以读写、普通用户不能随意访问的位置。
2. 安装 Go 1.27 和 Git，并确认 `go version`、`git version` 可执行。
3. 确保机器能访问 Go module 源。Windows 开发脚本已经按本项目约定配置本机代理；Linux 部署机需要自行配置直连或代理。
4. 为 `data/` 预留足够空间。原始 ZIP、解密/规范化 artifact、SQLite、Git 历史和歌曲缓存都保存在这里；已取消 Procedure 的 artifact 也会按审计要求保留。
5. 不要把现有 `data/` 从另一套实例直接覆盖过来。迁移已有实例前应整体备份该目录，并确保没有两个服务端同时使用同一个数据库。

### 2. 生成私有配置模板

第一次运行对应平台的脚本：

```bat
scripts\win-dev.start.cmd
```

或：

```bash
bash scripts/linux-alyhk.start.sh
```

首次运行只创建 `data/config.env` 并退出，不会下载依赖、编译或启动服务。该文件包含密码和密钥，已被 `.gitignore` 忽略；不要把它发到聊天、Issue、日志或版本库。

### 3. 填写管理员和服务密钥

`data/config.env` 的完整示例：

```dotenv
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=replace-with-a-long-password
MUSICOLET_ADMIN_TOTP_SECRET=BASE32_SECRET
MUSICOLET_SESSION_KEY=replace-with-at-least-32-random-characters
MUSICOLET_AGENT_TOKEN=replace-with-at-least-24-random-characters
MUSICOLET_PUBLIC_BASE_URL=https://musicolet.example.com
```

各字段用途：

- `MUSICOLET_ADMIN_USERNAME`：管理员登录名。
- `MUSICOLET_ADMIN_PASSWORD`：公网/生产模式至少 12 个字符；不要与其他站点共用。
- `MUSICOLET_ADMIN_TOTP_SECRET`：Google Authenticator 兼容的 Base32 密钥。把它作为“基于时间”的密钥手动加入验证器，并保证服务器时间同步正常。
- `MUSICOLET_SESSION_KEY`：至少 32 个随机字符，用于签名 Session；不能使用管理员密码代替。
- `MUSICOLET_AGENT_TOKEN`：至少 24 个随机字符，服务端和 Termux Agent 必须完全相同；不能使用 Session Key 代替。
- `MUSICOLET_PUBLIC_BASE_URL`：浏览器实际访问的 HTTPS 外部地址，不要填写内部监听地址。

可以用 PowerShell 生成独立随机值；连续执行两次，分别用于 Session Key 和 Agent Token：

```powershell
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
[Convert]::ToBase64String($bytes)
$rng.Dispose()
```

Linux 也可以分别执行两次：

```bash
openssl rand -base64 32
```

每个密钥都必须独立生成。配置使用一行一个 `KEY=value`，不要在等号两侧加空格。

Windows 开发脚本固定启用 `MUSICOLET_DEV_AUTH_ENABLED=1`，因此本机首次调试可以暂时留空 TOTP 和 Agent Token，但用户名、密码、32 字符 Session Key 仍是必填项。Linux 部署脚本固定使用生产模式，忽略配置文件中残留的开发模式值；密码、TOTP、Session Key 和 Agent Token 全部必填，并在任何下载或构建前检查长度。

### 4. 准备公网部署

仅在本机开发时可以直接访问 `http://localhost:4001/`。公网部署还需要：

- 为域名配置 DNS；
- 使用 Nginx/Caddy 等反向代理提供 HTTPS；
- 转发普通 HTTP、WebSocket `/agent/connect` 和媒体 Range 请求；
- 防火墙只开放实际需要的端口，优先只公开 80/443，不直接向公网暴露 4001；
- 校准系统时间，否则 TOTP 登录会失败；
- 确保 `data/config.env`、SQLite、Backup 和解密 artifact 只有服务账号可读。

反向代理和虚拟主机参考 `doc/tech/deploy-vhost.md`。生产 Agent 默认只接受 HTTPS/WSS；不要用 `MUSICOLET_AGENT_ALLOW_HTTP=1` 绕过公网 TLS。

### 5. 完成首次启动

填好配置后再次运行启动脚本。正常顺序应为依赖检查、重新编译服务端、交叉编译 Agent、启动监听。然后依次检查：

1. `http://localhost:4001/healthz` 返回健康状态；
2. 管理员密码和 TOTP 可以登录；
3. 未登录时管理 API 返回 401；
4. 上传 Backup 前确认它是从 Musicolet 新导出的完整 ZIP；
5. 首次导入先查看 manifest 校验和数量摘要，再提交生成 V1；
6. 将生成的 arm64 Agent 复制到 Termux，并按下文配置只读目录。

## 日常启动

Windows 开发环境：

```bat
scripts\win-dev.start.cmd
```

Linux/阿里云香港环境：

```bash
bash scripts/linux-alyhk.start.sh
```

两个脚本都会在每次启动前执行 module tidy/download/verify，重新编译服务端，并交叉编译 Linux arm64 Agent。首次运行只会在被 Git 忽略的 `data/config.env` 创建私有配置模板，然后立即退出，不会先下载依赖或编译；填写配置后再次启动。已有配置缺少必填项时也会在构建前给出具体错误。Windows 脚本按项目约定设置本机 HTTP/HTTPS/SOCKS 代理，Linux 脚本不预设代理。

直接运行二进制或编写自定义启动脚本时可使用的变量：

```dotenv
MUSICOLET_BIND_HOST=0.0.0.0
MUSICOLET_PORT=4001
MUSICOLET_DATA_DIR=data
MUSICOLET_SESSION_TTL=12h
MUSICOLET_DEV_AUTH_ENABLED=0
```

`MUSICOLET_DEV_AUTH_ENABLED=1` 仅用于受控开发环境：它允许省略 TOTP，并放宽密码/Agent token 的生产长度检查。不要在公网部署中启用。仓库自带的 Windows/Linux 脚本会分别强制开发/生产模式，避免私有配置中的旧值意外改变部署安全等级。

## Termux Agent

把脚本生成的 `data/bin/musicolet-agent-linux-arm64` 复制到手机，在 Termux 中配置并运行：

```bash
export MUSICOLET_SERVER_URL='https://musicolet.example.com'
export MUSICOLET_AGENT_TOKEN='与服务端相同的随机 token'
export MUSICOLET_AGENT_ROOTS='/storage/emulated/0/Music,/storage/emulated/0/存档'
./musicolet-agent-linux-arm64
```

生产连接必须使用 HTTPS/WSS。仅在可信本地调试时可显式设置 `MUSICOLET_AGENT_ALLOW_HTTP=1`。Backup 中的 Android `content://...primary%3A...` 路径会被映射为 `/storage/emulated/0/...`，但目标仍必须位于 `MUSICOLET_AGENT_ROOTS` 内；符号链接越界会被拒绝。

## 导入流程

1. 登录管理界面，打开“更多”。
2. 上传 Musicolet Backup ZIP；服务端先固定保存原始 ZIP，再异步解密、校验和解析。
3. 查看 manifest 校验、Semantic Diff、原始结构 Diff 和冲突。
4. 对冲突选择 OURS、THEIRS 或提交完整对象的手动 JSON。
5. Procedure 达到 `READY_TO_COMMIT` 后提交，生成新的不可变 Musicolet Version。

同一时间只允许一个未结束 Procedure。取消会保留其 ZIP、parser 报告、Diff 和 resolution 供审计。真实备份、数据库、缓存、私有配置和本地二进制都在 `.gitignore` 保护范围内。

## 本地验证

```bash
go test ./...
go build ./cmd/musicoletweb
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/musicolet-agent
```

健康检查：

```text
GET http://localhost:4001/healthz
```

更完整的架构、数据规则和运行说明见 `doc/tech/tech-master.md`；本轮实现与验证记录见 `doc/devlog/devlog-2609A-features.md`。
