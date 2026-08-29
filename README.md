# MusicoletWeb

MusicoletWeb 是一个以 Go + SQLite 为主的 Musicolet Web 镜像系统。当前分支实现初期开发计划的核心闭环：Musicolet Backup 全量导入、不可变 Snapshot、服务器 Working State / Server Change、可暂存 Import Procedure、三方语义合并、核心 Musicolet 风格浏览/播放 UI，以及 Termux 只读 Go Agent。

## 服务端启动

默认监听：`0.0.0.0:4001`。

推荐直接使用：

```bash
./scripts/linux-alyhk.start.sh
```

Windows 开发环境：

```bat
scripts\win-dev.start.cmd
```

两个脚本都会在每次启动前重新执行 Go 依赖整理/下载/校验并重新编译服务端。Windows 脚本保留了项目提供的 example 中的本地代理配置。

首次启动会在 `data/config.env` 创建模板。至少填写：

```text
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=<strong-password>
MUSICOLET_ADMIN_TOTP_SECRET=<base32-secret>
MUSICOLET_SESSION_KEY=<long-random-secret>
MUSICOLET_AGENT_TOKEN=<long-random-secret>
```

`data/` 是运行数据目录，不应提交到 Git。

## Termux / Android arm64 Agent

编译：

```bash
./scripts/build-agent-arm64.sh
```

得到：

```text
bin/musicolet-agent-arm64
```

示例：

```bash
MUSICOLET_AGENT_SERVER=https://musicolet.example.com \
MUSICOLET_AGENT_TOKEN='same-as-server' \
MUSICOLET_AGENT_ROOTS='/storage/emulated/0' \
./bin/musicolet-agent-arm64
```

Agent 仅支持只读打开允许根目录内的普通文件、按 Range 读取并回传；协议不存在 shell/命令执行、写文件、删除、移动、改名或 Metadata 写入命令。默认要求 HTTPS。仅可信本地开发环境才使用 `-allow-http`。

## Musicolet Backup 导入

管理端汉堡菜单中上传 ZIP 会创建唯一活动的 Import Procedure：

- 原始 ZIP 永久保存到 `data/imports/procedure-XXXXXX/original.zip`；
- 解密结果保存于对应 `decrypted/`；
- Candidate Snapshot 与正式 Musicolet Version 分离；
- 第二次及后续导入采用 BASE / OURS / THEIRS 三方语义分析；
- 未完成/未取消 Procedure 时拒绝新的 Backup ZIP；
- Conflict 可分批 resolve，服务器期间产生命中的新 M 后旧 resolution 会变为 stale；
- 最终提交前再次校验 Server Change head；
- 导入通过 commit journal 在 Git 与 SQLite 之间提供崩溃恢复。

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

Git 只保存稳定规范化的业务状态和版本拓扑，业务冲突语义仍由 Go Merge Engine 处理。

## 测试

联网并能下载 Go 依赖的环境：

```bash
./scripts/test.sh
```

包括普通 Go tests、SQLite integration test、前端 JavaScript 语法和 shell 语法验证。

## 设计文档

- `doc/requestment.md`
- `doc/roadmap/master/Initial Development Plans.md`
- `doc/roadmap/master/Remaining Development Plans.md`
- `doc/devlog/devlog-2609A-features.md`
