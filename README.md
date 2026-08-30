# MusicoletWeb

MusicoletWeb 是一个以 Go + SQLite 为主的 Musicolet Web 镜像系统。`dev-2609A-GPTCHAT` 实现初期开发计划的核心闭环：Musicolet Backup 全量导入、不可变 Snapshot、服务器 Working State / Server Change、可暂存 Import Procedure、三方语义合并、核心 Musicolet 风格浏览/播放 UI，以及 Termux 只读 Go Agent。

## 服务端启动

默认监听 `0.0.0.0:4001`。

Linux：

```bash
./scripts/linux-alyhk.start.sh
```

Windows 开发环境：

```bat
scripts\win-dev.start.cmd
```

两个脚本都会在每次启动前执行 `go mod tidy`、`go mod download all`、`go mod verify`，随后重新编译服务端。Windows 脚本保留 example 中的 HTTP/HTTPS/SOCKS 本地代理配置。

首次运行会创建 `data/config.env`。至少配置：

```text
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=<strong-password>
MUSICOLET_ADMIN_TOTP_SECRET=<base32-secret>
MUSICOLET_SESSION_KEY=<long-random-secret>
MUSICOLET_MASTER_KEY=<long-random-master-key>
MUSICOLET_AGENT_TOKEN=<bootstrap-token>
```

`MUSICOLET_AGENT_TOKEN` 只用于首次初始化或主动轮换 bootstrap。服务端成功启动后会使用 `MUSICOLET_MASTER_KEY` 派生的 AES-GCM 密钥将 Agent token 加密保存到 SQLite；之后可清空 `config.env` 中的 bootstrap token。管理端也提供轮换 Agent token 的入口。`data/` 不应提交 Git。

## Termux / Android arm64 Agent

编译：

```bash
./scripts/build-agent-arm64.sh
```

输出：

```text
bin/musicolet-agent-arm64
```

查看构建版本：

```bash
./bin/musicolet-agent-arm64 -version
```

示例：

```bash
MUSICOLET_AGENT_SERVER=https://musicolet.example.com \
MUSICOLET_AGENT_TOKEN='same-as-server' \
MUSICOLET_AGENT_ROOTS='/storage/emulated/0' \
./bin/musicolet-agent-arm64
```

Agent 主动建立低成本长期出站连接，不轮询服务端。协议仅允许读取歌曲内容和文件范围，不存在 shell/任意命令执行、文件写入、删除、移动、改名或 Metadata 写入命令。普通路径会在解析 symlink 后再次验证允许根目录；媒体 URI 只接受受限的 `content://media/.../audio/media/<数字ID>`，并通过固定 `/system/bin/content query/read` 调用读取，不把服务端数据拼成 shell 命令。默认要求 HTTPS，只有可信本地开发时才显式使用 `-allow-http`。

服务端向浏览器支持标准单 Range：Range 请求返回 `206 + Content-Range`；无 Range 时服务端会按块连续向 Agent 拉取直到完整文件结束，而不是只返回首个 4 MiB，因此大 MP3/FLAC 不会被截断。

## Musicolet Backup 导入

管理端上传 ZIP 后创建唯一活动的 Import Procedure：

- 原始 ZIP 永久保存到 `data/imports/procedure-XXXXXX/original.zip`；
- 解密内容保存在同一 Procedure 的 `decrypted/`，安全保留 ZIP 内相对目录结构；
- 解密前检查条目数、单项/总解压大小、重复项和不安全路径；
- Blowfish/manifest 校验写入 `parser_runs`，可查看逐文件 MD5 校验结果；
- Java Serialization 使用只读 token/canonical parser，不实例化 Java 类；
- Candidate Snapshot 与正式 Musicolet Version 分离；
- Songs、Playlist、Queue、Favorites、历史 `PCs_*`、当前 W/M/Y 统计、Settings 均进入 Snapshot；
- 第二次及后续导入使用 BASE / OURS / THEIRS 三方语义分析；
- Song Core 按整首歌曲判断冲突，Metadata 不做字段级自动 merge；
- Queue/Playlist 按 ordered-member 语义 merge；
- Playback State 始终以服务器为准，不参加导入冲突；
- Conflict 可分批 resolve，并永久记录 BASE / OURS / THEIRS / RESULT 与 resolution patch；
- 新 Server M 命中已处理冲突时，旧 Resolution 变为 stale；
- 最终提交前再次校验 Server Change head；
- 导入通过 commit journal 在 SQLite/Git 边界提供崩溃恢复。

服务器本地产生的播放会增加总播放数以及当前周/月/年计数。系统不会自行伪造 `PCs_W_*` / `PCs_M_*` / `PCs_Y_*` 历史 key；这些历史周期记录只按 Musicolet ZIP 中真实出现的名称导入。

## Git 历史

运行时 Git 审计仓库：

```text
data/git/history.git
```

逻辑 refs：

```text
refs/heads/musicolet-source
refs/heads/main
```

当前 `internal/gitstore` 使用隔离的 Git CLI plumbing backend，提供 blob/tree/commit/ref、merge-base、三树 merge和 stage 1/2/3 conflict-index 能力。业务冲突语义仍由 Go Merge Engine 处理。实现取舍见 `doc/tech/git-backend-2609A.md`。

## 初期 Web 操作

当前 Web 已把初期主链路所需操作接通：

- 多 Queue 查看与实际 Playback Queue 分离；
- Queue 改名、全局排序、删除、反向和永久随机化；
- 歌曲服务器侧 Metadata 编辑，不写回手机文件；
- Favorite、插队、排队尾、停止目标；
- 个人 Playlist 创建/删除、加歌、移歌和成员位置调整；
- Import Procedure parser report、Semantic Diff、Conflict、Resolution history；
- Agent token 管理员轮换。

## 测试

正常联网、能够下载 Go module 的环境执行：

```bash
./scripts/test.sh
```

脚本包括普通 Go unit tests、`go vet`、使用真实 `modernc.org/sqlite` 的数据库 integration tests、synthetic Musicolet SQLite Backup parser integration test、前端 JavaScript 语法和 shell 脚本语法检查。

仓库不包含用户私人 Musicolet Backup。真实 V1/V2 ZIP 的最终数量/顺序/统计对照必须在持有实际备份的环境执行，不能由 synthetic fixture 替代。

## 设计与开发记录

- `doc/requestment.md`
- `doc/roadmap/master/Initial Development Plans.md`
- `doc/roadmap/master/Remaining Development Plans.md`
- `doc/devlog/devlog-2609A-features.md`
- `doc/tech/git-backend-2609A.md`
