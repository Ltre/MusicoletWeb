# devlog-2609A-features

分支：`dev-2609A-GPTCHAT`

目标：依据 `doc/requestment.md`、`doc/roadmap/master/Initial Development Plans.md`、`doc/prompt/first.md`、`doc/prompt/second.md`、`doc/prompt/layout-prompt.md` 以及项目会话最终结论，完成 MusicoletWeb 初期开发计划。服务端主要采用 Go + SQLite，默认监听 `4001`。

> 规则：后续明确结论优先于早期讨论。禁止重新引入 `recording_id` / `file_instance_id` / Chromaprint / 自动路径 MOVE 识别；Musicolet source 关系仍以实际 path/URI 为准。

---

## 1. 起点与分支

开始开发时重新核对 GitHub，确认仓库只有需求、roadmap、部署文档和空启动脚本，上一轮口头描述的实现并未真正进入远端。因此从真实仓库状态重新开发，并创建/持续使用：

```text
dev-2609A-GPTCHAT
```

`main` 未 merge。

容器无法直接解析 `github.com`，因此代码生成/测试在 `/mnt/data/MusicoletWeb-impl` 完成，提交通过 GitHub API 直接推进指定分支。

---

## 2. P0/P1：工程、SQLite 与身份模型

已实现：

- `cmd/server`、`cmd/agent`；
- 默认 `0.0.0.0:4001`；
- SIGINT/SIGTERM 优雅退出；
- SQLite WAL / foreign_keys / busy_timeout；
- Snapshot、Working State、Server Change、Procedure、Conflict、Playback State 严格分层；
- Queue/Playlist `(container,path)` 唯一约束，禁止重复歌曲；
- `file_id` 只作为 Working DB 技术编号，不承担跨版本身份；
- 路径变化只理解为旧 path DELETE + 新 path ADD。

关键表包括：

```text
snapshots / musicolet_versions
snapshot_songs / playlists / queues / favorites
snapshot_period_counts / snapshot_current_counts / snapshot_settings
working_songs / playlists / queues / favorites
working_period_counts / working_current_counts / working_settings
server_changes / change_targets
import_procedures / import_artifacts / parser_runs
semantic_diffs / merge_conflicts
conflict_resolutions / resolution_patches
commit_journal
secure_settings
```

当前 schema 使用 Python 标准库真实 SQLite 执行验证通过，共 36 张表；Queue 重复成员约束实测会触发 `IntegrityError`。

---

## 3. P2：Musicolet Backup 解密、Parser 与归档

已实现：

- Blowfish ECB，key `JSTMUSIC_2`；
- PKCS padding 去除；
- 原始 ZIP 先归档到 Procedure 目录，不删除；
- `DB_SONGS_LOG`；
- `PCs_*`；
- `.mpl` Playlist；
- `0.qstk` Queue；
- `0.favs` Favorite；
- 未知 JSON 进入 Settings；
- 未知文件继续进入 RawFiles，不静默丢弃。

### 3.1 原始结构 Diff

- JSON -> stable JSON；
- SQLite -> schema + 稳定排序 row canonical text；
- Java Serialization -> 只读 token/canonical parser，不实例化类、不执行 Java；
- 其他 binary -> stable hex；
- Procedure 可同时查看 Semantic Diff 与解密结构字符差异。

### 3.2 ZIP/manifest 安全补强

第三次复核确认早期 Parser 仍缺长期审计和异常 ZIP 防护，已补：

- `parser_runs`：RUNNING / SUCCEEDED / FAILED、parser version、report/error；
- Procedure parse 失败明确转 `FAILED`，不会永久占住 one-active-procedure 锁；
- `0.musicolet.backup` 总 hash 及可识别逐文件 MD5 校验；
- manifest 引用缺失文件或 MD5 不符直接拒绝；
- ZIP 相对目录安全保留；
- 拒绝绝对路径、`..` traversal、重复 entry；
- 文件数、单 entry、总解压量上限；
- synthetic encrypted fixture、manifest、Java、unsafe path、duplicate entry 测试；
- build-tagged SQLite parser integration fixture。

Parser 子目标远端完成于：

```text
eea4cca476c1d96c6443ee4a689708d445898314
```

---

## 4. P3：Git / Server Change

运行时审计仓库：

```text
data/git/history.git
refs/heads/musicolet-source
refs/heads/main
```

业务层只依赖 `internal/gitstore`，不会散落执行 Git 命令。

### 4.1 Server M

覆盖：

- Favorite；
- server-only Metadata；
- 服务器永久删除歌曲及关联；
- Playlist create/delete/add/remove/move；
- Queue add/remove/move/delete/rename/reorder；
- Queue global order；
- Web 播放统计。

Server M 对应 change target 和 `has_server_changes`。新 Musicolet 未真正修改同一业务数据时，M 不被清空。

### 4.2 Git/SQLite 崩溃恢复

普通 M：SQLite 先记录 change，Git 成功后回填 commit；启动时 `ReconcileGit()` 补 pending Git audit。

Import：`commit_journal` 使用 PREPARED -> SOURCE_DONE -> GIT_DONE -> DONE，保存 source/main parents 和 commits；`RecoverCommitJournals()` 可恢复异常中断。

### 4.3 Git capability 补齐

初版只有 commit/ref。第三次复核补齐 roadmap 明确要求：

- merge-base；
- commit -> tree；
- 临时 `GIT_INDEX_FILE` 三树 `read-tree -m`；
- stage 1/2/3 `ls-files -u` conflict index；
- conflict-free `write-tree`；
- `commit-tree` + CAS ref update。

测试使用双分支修改 `state.json`，验证 stage 1/2/3 均可读取。

提交：

```text
771ff9156aeecb42d305a9ba56711ba82963d87c
e8ce096a0d655e0364e5886012b59606e7a506b9
```

`pkg-config --modversion libgit2` 当前仍提示未安装，因此不能声称 git2go/libgit2 CGO link spike 已通过。当前 Git CLI plumbing backend 已满足初期所需 Git primitive，替换边界见 `doc/tech/git-backend-2609A.md`。

---

## 5. P6：Semantic Merge / Import Procedure

### 5.1 基本规则

- 同时仅一个活动 Procedure；
- ZIP 与 Procedure 固定绑定；
- CANCEL/FAILED 不物理删除原 ZIP；
- Candidate Snapshot 不修改当前 Working；
- Commit 前重新校验 Server Change head；
- Procedure 可暂停/恢复。

### 5.2 Song Core

整体冲突单元，不做字段级 auto merge：

```text
THEIRS == BASE && OURS != BASE -> OURS
OURS == BASE && THEIRS != BASE -> THEIRS
OURS == THEIRS -> 自动接受
双方都改 Song Core 且结果不同 -> conflict
```

服务器删除 + 手机仅播放量变化，不 resurrect；新 path 是新 ADD。

### 5.3 Queue/Playlist ordered merge

- unrelated MOVE + ADD -> 自动合并；
- 同一成员双 MOVE 到不同位置 -> conflict；
- 双方 ADD 同一歌曲不同位置 -> conflict；
- SERVER DELETE + PHONE MOVE -> 不 conflict；
- 始终去重。

### 5.4 播放统计

严格按已确认公式：

```text
server_change     = current_server - previous_resolve
musicolet_change  = incoming - base
resolve           = previous_resolve + server_change + musicolet_change
                  = current_server + (incoming - base)
```

回归锁定：

```text
100 -> 105 -> 120 -> 146 -> 164
```

旧讨论第四轮 `150-146=14` 是算术笔误；测试使用 mathematically consistent `server=160`。

`DB_SONGS_LOG.COL_NUM_PLAYED_W/M/Y` 作为 current W/M/Y；服务器本地播放总数、Current Week/Month/Year 均 +1。系统不再伪造 `PCs_W/M/Y_*` 历史 key，历史 `PCs_*` 只按 Musicolet ZIP 中实际出现的数据保存。Last Played 截到秒级。

### 5.5 Settings / Resolution Audit

已增加：

- Settings Snapshot / Working / Semantic Diff；
- total / Last Played / historical PCs_* / current W/M/Y Diff；
- `conflict_resolutions` 永久历史；
- `resolution_patches`；
- ordered Queue/Playlist 的 ADD/REMOVE/MOVE patch；
- stale Resolution 保留旧历史，重新确认后写新 resolution。

Procedure API/UI 现在可查看 parser runs、Semantic Diff、Conflict 和 Resolution history。

相关 API 完成于：

```text
2495bdcbcb850e9bb3c917639a8ec3c250437cab
```

---

## 6. P5：Queue / Playlist / Playback

核心行为：

- Queue/Playlist 禁止重复歌曲；
- 来源对象与 Queue 隐式关联，不只按名称匹配；
- 名称碰撞使用 `AAA #编号`；
- 普通直接播放复用来源 Queue 并跳到目标歌曲；
- 已存在歌曲加入 Queue -> 移动到队尾；
- 插队待播 -> 当前歌曲之后；
- viewed Queue 与实际 playback Queue 分离；
- 每 Queue 保存 current path / progress / stop target；
- 删除当前播放 Queue -> 切换下一 Queue 记忆点；
- stop target 绑定歌曲路径；
- Queue rename、global order、atomic reorder/reverse/randomize；
- Playback State 不参加 Musicolet import conflict。

第三次复核发现 HTTP action switch 和 Web UI 仍停在旧版，已补并立即推送：

```text
0c4d98e4  HTTP queue rename/order/reverse/randomize
4c810ba0  viewed Queue/playback Queue split + Queue ordering UI
```

---

## 7. P4：Core Web UI

移动优先 SPA，顶层：

- Queue
- Now Playing
- Folder
- Album
- Artist / Album Artist / Composer
- Genre
- Playlist
- Search
- Menu

第三次复核补齐初期操作闭环：

- server-only Metadata 编辑，不写手机原 Metadata；
- 任意歌曲加入个人 Playlist；
- 个人 Playlist 移出歌曲、移动成员位置、删除；
- 系统派生 Playlist 不开放成员编辑；
- Queue rename/order/reverse/randomize；
- Procedure parser report / Resolution history；
- Agent token 管理员轮换。

相关提交：

```text
0987148e004ff3d2ef267fe93a8d3e2b4f5f4fce
217de9e786486f5a0524906109731cb0e83ac4ce
4c810ba07e9ef77a12066e846b0dd3f964cf3cb1
d6f57bb65efede99f92c7bfd0dd42c89c3f115f0
```

像素级 Musicolet 复刻、完整多选、全部排序项、均衡器等按 Remaining Plan 继续，不作为本轮初期主链路阻塞项。

---

## 8. P7：Termux arm64 Agent

严格只读：

- Agent 主动 SSE 长连接，不轮询；
- 45 秒 heartbeat；
- 指数退避；
- generation 防旧连接 disconnect 覆盖新连接；
- 单次读取最多 4 MiB、最多 4 并发；
- 服务端无 Range 时连续拉取完整文件；
- 单 Range 标准 `206 + Content-Range`；
- 普通路径 `EvalSymlinks` 后校验 allow root；
- 只读 regular file；
- 受限 `content://media/.../audio/media/<数字ID>`；
- 仅固定 `/system/bin/content query/read`，没有 shell RPC；
- 没有 create/write/delete/move/rename/Metadata write；
- 默认 HTTPS，HTTP 仅显式 local-dev；
- `-version`；
- arm64 build ldflags 注入版本。

Agent/媒体小目标按完成即推策略提交：

```text
6642d758  Hub replacement + range metadata
97cea243  full media streaming + HTTP Range
b7af3d53  Range regression
bdabd246  content media / versioning
fd24a58e  Agent safety tests
e4c250fe  arm64 build version
```

### 8.1 Agent token 加密

复核发现长期明文 env token 不符合安全要求，已增加：

- `MUSICOLET_MASTER_KEY`；
- `secure_settings`；
- SHA-256(master) -> AES-256-GCM；
- setting key 作为 AAD；
- `MUSICOLET_AGENT_TOKEN` 仅 bootstrap；
- 后续从加密 SQLite 获取；
- 管理员可轮换 token；
- Linux/Windows config 模板同步。

该小目标远端结束于：

```text
f37dc2921e77be055c86186ca6834fdc16f28c08
```

---

## 9. Auth 与公开边界

实现：

- 环境变量 Admin username/password；
- TOTP / Google Authenticator；
- HMAC session；
- CSRF；
- SameSite Strict；
- CSP / X-Frame-Options / nosniff。

默认所有管理 API 需管理员身份。

公开只有当前初期明确开放的 Now Playing：

```text
/now-playing
/api/public/now-playing
/api/public/now-playing/media
```

公开 API 不返回手机完整路径和 raw Metadata。

---

## 10. 启动/测试脚本

Linux / Windows 均参考 `scripts/example`：

- 默认 4001；
- 自动 config template；
- `go mod tidy`；
- `go mod download all`；
- `go mod verify`；
- 每次重新 `go build`；
- Windows 保留 `58591` HTTP/HTTPS 与 `51837` SOCKS5 代理。

Agent：

```text
scripts/build-agent-arm64.sh
```

测试脚本已升级并推送：

```text
9ba924bde7c1f37f5bc9c4d72a9af809f85cd27b
```

在正常联网环境执行：

```bash
./scripts/test.sh
```

包含：

```text
go test ./...
go vet ./...
go test -tags=integration ./internal/db ./internal/musicolet
node --check web/*.js
shell syntax checks
```

---

## 11. 当前执行环境的实际验证

第三次复核后的当前对应源码重新执行：

```text
go test -modfile=<stub-only-modfile> ./...    PASS
go vet  -modfile=<stub-only-modfile> ./...    PASS
node --check web/*.js                        PASS
bash -n scripts/*.sh                         PASS
Python sqlite3 executes Go schema             PASS (36 tables)
Queue duplicate (queue_id,path) constraint    PASS
```

stub 仅存在于 `/mnt/data/musicolet-stubs`，不提交仓库，只用于当前 DNS 受限容器的类型编译/纯逻辑测试，不冒充真实 dependency integration。

真实运行过的逻辑测试包括：

- Song Core conflict；
- ordered MOVE conflict；
- SERVER DELETE + PHONE MOVE；
- 多版本 play-count 105/120/146/164；
- Resolution ordered patch；
- Agent symlink escape / file Range / media URI allowlist；
- stale Agent disconnect replacement；
- HTTP Range parser；
- Java serialization canonical parser；
- unsafe/duplicate ZIP；
- Git commit/ref/merge-base/stage1/2/3 conflict index；
- TOTP/session。

---

## 12. 当前仍必须在外部真实环境完成的验收边界

以下不是继续写普通业务代码即可在当前容器可信完成：

1. 容器 DNS 仍无法解析 `proxy.golang.org`，无法下载真实 `modernc.org/sqlite` / `golang.org/x/crypto`；需要正常联网环境运行 `./scripts/test.sh` 并生成/提交真实 `go.sum`；
2. 当前没有可可靠访问的原始**加密**私人 Musicolet V1/V2 ZIP，不能伪造 50+ Playlist、Queue/Favorite/统计的真实 E2E 对照；
3. `content://media/...` 需要目标 Android/Termux 真机验证 `/system/bin/content` 的实际只读权限；
4. 当前系统未安装 libgit2，git2go/libgit2 CGO link spike 不能伪报通过。

除上述真实依赖、私有 ZIP、Android 真机和 libgit2 环境边界外，第三次逐项复核发现的已知初期代码闭环缺口均已实际实现，并遵循“小目标完成后立即推送”的方式进入 `dev-2609A-GPTCHAT`。
