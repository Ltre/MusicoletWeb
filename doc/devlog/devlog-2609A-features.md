# devlog-2609A-features

分支：`dev-2609A-GPTCHAT`

目标：依据 `doc/requestment.md`、`doc/roadmap/master/Initial Development Plans.md`、`doc/prompt/first.md`、`doc/prompt/second.md`、`doc/prompt/layout-prompt.md` 以及项目会话最终结论，完成 MusicoletWeb 初期开发计划。服务端主要采用 Go + SQLite，默认端口 `4001`。

> 记录原则：早期讨论中与 `requestment.md` 或 grill-me 最终结论冲突的方案不采纳。特别禁止重新引入 `recording_id` / `file_instance_id` / Chromaprint / 自动路径 MOVE 识别。

---

## 1. 实际起点核对

开始继续开发时重新读取 GitHub 分支树，确认 `dev-2609A-GPTCHAT` 仍然与创建分支时的 `main` 相同：仓库只有需求/roadmap/部署文档和两个空启动脚本，没有上一轮口头汇报中所称的业务代码。

因此本轮从实际仓库状态重新实现，不能把未写入仓库的内容当成已完成。

执行环境无法 DNS 解析 `github.com`，因此不能在容器内 `git clone/push`；开发源码在本地工作目录生成、检查，最后通过 GitHub Git Data API 创建 blob/tree/commit 并推进 `dev-2609A-GPTCHAT` ref。

---

## 2. 工程与运行基线（P0）

完成：

- Go module：`github.com/Ltre/MusicoletWeb`；
- 服务端入口 `cmd/server`；
- Termux Agent 入口 `cmd/agent`；
- 配置、认证、SQLite、领域模型、Musicolet parser、merge、Git adapter、Agent hub、HTTP API 分层；
- 服务端默认 `0.0.0.0:4001`；
- `data/`、`bin/` 从 Git 排除；
- 健康检查 `/api/health`；
- SIGINT/SIGTERM 优雅退出。

### 2.1 Git 实现选型偏差

Roadmap 首选 `libgit2 + git2go`。本轮在当前无法安装/验证系统 libgit2/CGO 的执行环境中，为避免把整个初期实现卡死在原生依赖上，实际先实现了独立 `internal/gitstore` adapter，底层调用本机完整 Git plumbing：

- `hash-object`
- `mktree`
- `commit-tree`
- `update-ref`（含 old SHA CAS）
- `rev-parse`

业务层完全不直接执行 Git 命令，因此以后可把 adapter 替换为 git2go，而不改 Semantic Merge Engine / Procedure 业务。该偏差不是对“Git 完整能力 SDK”最终方向的否定，而是初期可运行实现的工程折中。

---

## 3. SQLite 数据模型（P1）

完成 WAL/foreign key/busy timeout 基础配置，并明确分离：

### 3.1 不可变来源

- `snapshots`
- `musicolet_versions`
- `snapshot_songs`
- `snapshot_playlists`
- `snapshot_playlist_items`
- `snapshot_queues`
- `snapshot_queue_items`
- `snapshot_favorites`
- `snapshot_period_counts`
- `snapshot_raw_files`

Snapshot 只由导入流程写入，Working API 不修改 Snapshot。

### 3.2 Working State

- `working_songs`
- `working_playlists`
- `working_playlist_items`
- `working_queues`
- `working_queue_items`
- `working_favorites`
- `working_period_counts`
- `queue_playback_state`
- `runtime_playback_state`

Playlist/Queue 使用 `(container, path)` 主键约束同一列表内不出现重复歌曲。

### 3.3 Server Change / Procedure

- `server_changes`
- `change_targets`
- `semantic_diffs`
- `merge_conflicts`
- `import_procedures`
- `import_artifacts`
- `commit_journal`

`server_changes` 与 `change_targets` 支持“对象有服务器修改”的显式追踪；歌曲 Working 行有快速 `has_server_changes` 标记。

### 3.4 SQLite 连接问题修正

初版为了串行写入曾设置 `MaxOpenConns(1)`，但 Working Queue/Playlist 读取会在外层 rows 未关闭时读取成员，单连接会自锁。已改为多连接 + WAL/busy timeout，写入的业务级串行化由 Service `mutation` lock 负责。

---

## 4. Musicolet Backup 解密与 Parser（P2）

完成：

- Blowfish ECB；
- 固定 key `JSTMUSIC_2`；
- PKCS padding 去除；
- 原始 ZIP 先归档再解析；
- 解密后文件保存到 Procedure 专属 `decrypted/`；
- `0.musicolet.backup` + `hash` 存在时校验 manifest MD5（兼容 16-byte digest / 32-char hex）；
- `DB_SONGS_LOG` SQLite；
- `PCs_*` SQLite；
- `.mpl` Playlist；
- `0.qstk` Queue；
- `0.favs` Favorite；
- 不认识的文件仍保留在 RawFiles，不静默丢弃。

### 4.1 Canonical raw diff

为满足“同时查看新旧解密 ZIP 字符差异”：

- JSON → stable pretty JSON；
- SQLite → `sqlite_master` schema + 每表所有行转 JSON 后排序，形成 canonical text；
- 其他二进制 → stable hex dump；
- Raw canonical text 保存到 `snapshot_raw_files`；
- Procedure 分析生成 `target_type=raw / operation=CHAR_DIFF` 差异。

Git 的 `state.json` 不重复保存 RawFiles；原始 ZIP 与 RawFiles Snapshot 承担来源审计。

### 4.2 路径与身份

严格按需求：

- 路径/URI 是 Musicolet Snapshot 内的歌曲定位主体；
- `file_id` 仅为 Working DB 技术编号；
- 旧路径删除 + 新路径新增，不恢复旧 file_id；
- PCM 不参与导入身份判断；
- 未建立 recording/file-instance 概念。

---

## 5. Git / Server Change（P3）

运行时审计仓库：`data/git/history.git`。

Refs：

- `refs/heads/musicolet-source`：正式 Musicolet Version；
- `refs/heads/main`：服务器工作态及 M。

Server M 首批覆盖：

- Favorite；
- Metadata server-only 修改；
- 永久删除服务器歌曲数据块及 Playlist/Queue/Favorite 关系；
- Playlist 创建/删除/成员 add/remove/move；
- Queue add/remove/move/delete；
- Web 播放次数；
- Queue 来源创建。

Queue Server Change 的 target 使用 Queue **名称**而不是内部数值 ID，使其能和导入时 Queue conflict 的业务 key 对齐。

### 5.1 Server Change 的跨导入存续

修正了“成功导入后清空全部 M”的错误做法。提交新 Version 时逐条判定：

- 最终 Working Song Core 仍与 incoming 不同 → Metadata/Delete M 继续 active；
- Favorite 最终状态与 incoming 不同 → Favorite M 继续 active；
- Playlist/Queue 最终成员列表仍与 incoming 不同 → 对应 M 继续 active；
- PLAY M 在成功导入后结束，因为该增量已结算进新的 resolve 基线；
- 已完全采用 incoming 的 M 才 inactive。

随后重新计算 Working 对象 `has_server_changes`。

### 5.2 DB/Git 崩溃恢复

普通 M：

1. SQLite 先记录 `server_changes`，`git_commit=NULL`；
2. 再写 Git；
3. 成功后回填 commit；
4. 启动执行 `ReconcileGit()`，发现 pending change 时补写 Git。

Import Commit：

- 使用 `commit_journal` 记录 PREPARED → SOURCE_DONE → GIT_DONE → DONE；
- 保存 source/main parent 和 commit；
- 进程异常后 `RecoverCommitJournals()` 可继续生成缺失的 Git commit 并完成 SQLite apply；
- Git ref update 使用 old SHA CAS。

这不是跨文件系统意义上的单一 ACID 事务，但通过 intent journal + 启动恢复避免“半提交后静默丢审计”。

---

## 6. Semantic Merge / Procedure（P6）

### 6.1 Procedure 基本规则

- 同时仅一个活动 Procedure；
- 状态包含 `PARSING / REVIEWING / RESOLVING / READY_TO_COMMIT / COMMITTING / COMMITTED / CANCELLED / FAILED`；
- 活动 Procedure 未提交或取消之前拒绝新 ZIP；
- ZIP 与 Procedure 固定绑定；
- CANCEL 只改状态，不删除 ZIP / Candidate / Diff / Resolution。

### 6.2 Song Core

Song Core 整体判断，不做 Metadata 字段级自动 merge。

实现：

- `THEIRS == BASE && OURS != BASE` → OURS；
- `OURS == BASE && THEIRS != BASE` → THEIRS；
- `OURS == THEIRS` → 自动接受；
- 双方都改 Song Core 且结果不同 → conflict；
- 服务器删除 + 手机 Song Core 未变化（即使播放数增长）→ 保持服务器删除；
- 手机新路径 → 新 ADD，不继承旧路径 server delete。

### 6.3 Ordered Queue/Playlist

实现成员级 merge：

- server move + phone unrelated add → 自动合并；
- 双方将同一歌曲 move 到不同位置 → conflict；
- 双方新增同一歌曲但位置不同 → conflict；
- server delete + phone move → 不 conflict，server delete 胜出；
- 列表成员始终去重。

### 6.4 播放次数

实现等价公式：

```text
resolve = previous_resolve
        + (current_server - previous_resolve)
        + (new_import - previous_import)

        = current_server + (new_import - previous_import)
```

单测锁定：

- 100 / server 102 / import 103 → 105
- base 103 / server 115 / import 108 → 120
- base 108 / server 138 / import 116 → 146

此前讨论示例第四轮 `previous_resolve=146, server=150, server_change=+14` 是算术笔误；按已确认公式 `150-146=4`。实现遵循公式，不迎合该笔误。

Last Played：取较晚时间，服务器新增记录统一截到秒级，不保留毫秒精度。

年/月服务器播放统计：当前年份 `PCs_Y_YYYY` 与零基月份 `PCs_M_YYYY.M` 同步 +1。

`PCs_W_*` 的周编号格式尚未从当前仓库 fixture 得到可靠确认，因此本轮**没有凭空发明周 key**；已有 Musicolet 周统计仍会完整导入、diff、merge。待拿真实周 key fixture 后补服务器新增播放对应周计数。

### 6.5 Resolution 与 stale

重要修正：重新 Analyze 不再删除用户已作出的 resolution。

重算时以 `target_type + target_key` 关联旧 conflict：

- 未命中新 M → 保留 RESOLVED 决定；
- `resolved_server_head` 之后出现命中同一业务对象的 Server Change → 该 resolution 重新插入为 STALE；
- STALE 被视为 unresolved，禁止 Commit；
- 用户重新选择 OURS / THEIRS / MANUAL 后更新到当前 head。

手动 Resolution 接受完整 Song JSON 或完整路径数组，不限制在单个字段修改。

---

## 7. Core Web UI（P4/P5）

实现无前端构建依赖的移动优先 SPA，避免当前阶段再引入 Node toolchain 作为服务端启动前置条件。

顶层 Tabs 已建立：

- 播放队列
- 正在播放
- 文件夹
- 专辑列表
- 艺术家列表（Artists / Album Artists / Composers）
- 风格
- 歌曲清单
- 搜索
- 汉堡菜单

完成初期可用视图：

- Queue 下拉和歌曲列表；
- All songs / 系统 Playlist / Personal Playlist；
- Folder / Album / Artist / Album Artist / Composer / Genre 聚合；
- 视图内搜索；
- 全局输入即搜索及分组数量；
- Favorite；
- 服务器永久删除；
- 基础播放；
- Import Procedure / Diff / Conflict resolve UI。

完整 Musicolet 菜单、像素级细节、完整多选、所有排序项等仍按 `Remaining Development Plans.md` 继续。

---

## 8. Queue / Playback 行为（P5）

完成核心规则：

- Queue 内不允许重复歌曲；
- Playlist 内不允许重复歌曲；
- 来源（Album/Playlist/Genre/Artist/Folder/Search）与 Queue 有隐式 `source_type/source_key` 关联；
- 同名但来源不同会生成 `AAA #编号`；
- 来源已有对应 Queue 时复用并跳到点击歌曲，不重建；
- 加入已存在歌曲 → 移动到队尾；
- 插队待播 → 移到当前歌曲之后；
- stop target 绑定歌曲路径，删除该成员时清掉 stop target；
- 每 Queue 保存 current song / progress / stop target；
- 删除当前播放 Queue 时切换到下一个 Queue 的记忆点；
- Queue 内容与 runtime Playback State 分离；
- Playback State 不进入 Musicolet Import conflict；
- 顺序/随机、loop mode、speed 有独立状态。

Web Audio 使用 `/api/media` 从 Agent 按 Range 只读取得音频。

---

## 9. 管理员认证与公开边界

实现：

- `MUSICOLET_ADMIN_USERNAME`
- `MUSICOLET_ADMIN_PASSWORD`
- `MUSICOLET_ADMIN_TOTP_SECRET`
- 标准 30 秒 TOTP（Google Authenticator 可用）
- HMAC session cookie
- CSRF cookie + `X-CSRF-Token`
- SameSite Strict
- 基础 CSP / X-Frame-Options / nosniff

默认所有业务 API 需要管理员身份。

公开：

- `/now-playing`
- `/api/public/now-playing`
- `/api/public/now-playing/media`

公开 Now Playing 返回裁剪后的歌曲信息，不返回手机文件路径和 Raw Metadata。

---

## 10. Termux Go Agent（P7）

交付：

- Go 源码 `cmd/agent`；
- `scripts/build-agent-arm64.sh`；
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`。

协议：

- Agent 主动建立 SSE 长连接；
- 服务端在现有连接中下发只读 Range 请求；
- Agent POST 对应 request ID 的字节结果；
- 无轮询；
- 45 秒低频 heartbeat；
- 断线指数退避，最大 1 分钟；
- 服务端请求有 timeout/cancel。

权限边界：

- 默认要求 HTTPS；
- HTTP 只有显式 `-allow-http` / `MUSICOLET_AGENT_ALLOW_HTTP=1` 才启用；
- Bearer Agent Token；
- 仅允许 `MUSICOLET_AGENT_ROOTS` 内路径；
- `EvalSymlinks` 后再次检查 root，阻止 symlink 越界；
- 只接受普通文件；
- 没有 shell/exec RPC；
- 没有 create/write/remove/rename/move API；
- 支持 `file://`；
- 支持常见 `com.android.externalstorage.documents` primary URI 转 `/storage/emulated/0/...`；
- 纯 Termux Go 进程无法使用 Android ContentResolver，因此一般 `content://media/...` 暂明确返回 unsupported，不做错误路径猜测。

这项限制需要后续结合用户实际 Musicolet All songs 路径形态验证；若大批路径只有 MediaStore content URI，需要设计不引入 APK 的可靠只读解析桥接。

---

## 11. 启动/测试脚本

按 `scripts/example` 复刻运行流程并改为 MusicoletWeb：

### Linux

`scripts/linux-alyhk.start.sh`

- 默认 `0.0.0.0:4001`；
- 自动创建 `data/config.env` 私密模板；
- `go mod tidy`；
- `go mod download all`；
- `go mod verify`；
- 每次重新 `go build`；
- 启动 `bin/musicoletweb`。

### Windows

`scripts/win-dev.start.cmd`

保留 example 的开发代理：

```text
HTTP/HTTPS 127.0.0.1:58591
SOCKS5      127.0.0.1:51837
```

同样每次 tidy/download/verify/build 后启动 `4001`。

### Agent

`scripts/build-agent-arm64.sh`

### Tests

`scripts/test.sh`

---

## 12. 本轮验证

当前执行容器不能解析 GitHub/外部 Go module 域名，所以无法真正下载：

- `modernc.org/sqlite`
- `golang.org/x/crypto`

因此验证明确分成两层：

### 已实际执行并通过

- `go test`：merge/auth/gitstore/agent/parser 等纯逻辑；
- 全项目使用**仅存在于 `/tmp`、不提交仓库**的 sqlite/blowfish type stub 进行编译和单元测试，确认所有 Go package 类型可编译；
- `node --check web/app.js`；
- `node --check web/now-playing.js`；
- `bash -n scripts/linux-alyhk.start.sh`；
- `bash -n scripts/build-agent-arm64.sh`；
- Git adapter 实际使用本机 Git 创建 bare repo、连续 commit 和 ref 更新测试通过；
- Agent path root / symlink escape / externalstorage URI 测试通过；
- Ordered Merge / Song Core / play-count 规则测试通过。

### 需要在有网络依赖的用户开发环境执行

```bash
./scripts/test.sh
```

其中包含真实 SQLite `-tags=integration` schema test。

由于不能真实获取 module，本分支没有伪造 `go.sum`。Windows/Linux 启动脚本会先联网 `go mod tidy/download/verify`；在正常开发机首次运行后应把稳定生成的 `go.sum` 纳入后续提交。

---

## 13. 初期验收状态与仍需真实数据验证的边界

代码侧已完成 Initial Development Plans 的架构主链路和核心 UI/Agent 实现，但以下项目天然依赖真实运行环境/真实备份，不能在当前无外网容器中伪称已完成实机验收：

1. **真实 modernc SQLite 运行**：需用户环境下载 module 后执行 integration test；
2. **真实 Musicolet ZIP V1/V2**：仓库没有私人 Backup fixture，需使用实际 ZIP 对照 All songs/54 左右 Playlist/Queues/统计；
3. **PCs_W 周 key**：需真实 fixture 再实现服务器本地播放对当前周计数 +1；
4. **手机实际 URI**：若 All songs 使用 `content://media/...`，Termux-only Agent 无 Android ContentResolver，需追加只读解析方案；
5. **libgit2/git2go**：当前 adapter 为完整 Git CLI plumbing，后续可按最终技术偏好替换。

这些均已明确记录，避免把未验证边界伪装成已验证功能。

---

## 14. 开发中发现并修正的问题

- 重新 Analyze 会丢 Resolution → 改为按业务 key 保留，命中新 M 才 stale；
- Queue M target 用内部 ID，无法命中导入 conflict → 改成 Queue name；
- SQLite 单连接 + nested read 会自锁 → 调整连接池；
- Snapshot song INSERT placeholder 数量多 1 → 修正；
- CanonicalSnapshot 曾错误排序 Playlist Paths，会破坏播放列表顺序 → 只排序容器，不排序 ordered members；
- SQLite raw diff 曾退化为 MD5 → 改为 canonical schema/row text；
- 导入成功曾考虑清空所有 M → 改成逐项判断是否仍与 incoming 不同；
- Agent root 检查如果只看输入路径会被 symlink 绕过 → `EvalSymlinks` 后检查；
- Public Now Playing 不能暴露手机路径 → 独立裁剪 DTO；
- Server Last Played → 秒级精度；
- Queue/Playlist 防重复通过数据库约束 + 业务去重双层保证。

---

## 15. 后续衔接

后续进入 `Remaining Development Plans.md` 时，不应重新推翻：

- 路径为 Musicolet 数据定位主体；
- file_id 非跨版本身份；
- Song Core 整体 conflict；
- Ordered member semantic merge；
- Server Change 覆盖规则；
- Procedure stale resolution；
- Playback delta merge；
- Agent 只读权限边界。

后期主要补齐完整 Musicolet UI、全部排序/多选/菜单、缓存管理、分享、PCM 参照库、系统 Backup/Restore、音频裁剪器和更完整的播放功能。
