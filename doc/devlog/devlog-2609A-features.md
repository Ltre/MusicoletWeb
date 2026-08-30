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

开发期间每个可独立验收的小目标都直接提交并 push 到该分支；后续验收以 GitHub 远端真实 branch HEAD 与 GitHub Actions 结果为准，不以本地或工具返回但未落 ref 的临时 SHA 为准。

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

迁移可靠性在后续复核中继续补强：

- `ensureColumn()` 在 `ALTER TABLE` 前显式关闭 `PRAGMA table_info` rows，避免旧 schema 升级时读游标与 DDL 并存；
- build-tagged integration test 从缺少 `current_queue_index` 的旧 snapshots schema 启动并验证自动升级；
- Snapshot immutable / legacy schema migration 均进入真实 SQLite integration suite。

对应小目标结束于：

```text
f84eb779c02ff491096d549283fb2a8bc1fe391d
```

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

已补：

- `parser_runs`：RUNNING / SUCCEEDED / FAILED、parser version、report/error；
- Procedure parse 失败明确转 `FAILED`，不会永久占住 one-active-procedure 锁；
- `0.musicolet.backup` 总 hash 及可识别逐文件 MD5 校验；
- manifest 引用缺失文件或 MD5 不符直接拒绝；
- ZIP 相对目录安全保留；
- 拒绝绝对路径、`..` traversal、重复 entry；
- 文件数、单 entry、总解压量上限；
- synthetic encrypted fixture、manifest、Java、unsafe path、duplicate entry 测试；
- build-tagged SQLite parser integration fixture。

### 3.3 真实 Backup 基准

`internal/musicolet/real_backup_integration_test.go` 保留真实私有 ZIP 的环境变量测试入口，并锁定已完成分析的 2026-08-30 Backup 基准，包括：

```text
89 files
6653 songs
54 playlists
29282 playlist items
5527 favorites
14 queues
15780 queue items
current queue index = 13
23 historical period sets
```

第二份真实 ZIP 缺失时测试明确 skip，不用 synthetic 数据冒充 V1/V2 真实差异。

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

后续新增服务级 integration test，验证 Favorite / Metadata / Playlist / Queue 四类实际 M 均同时满足：

```text
working state changed
active server_change exists
object/song change mark exists
Git commit non-empty
refs/heads/main advances
```

对应提交：

```text
272a6e25505ff3b1018bc9b98204ba49f2f1d395
```

### 4.2 Git/SQLite 崩溃恢复

普通 M：SQLite 先记录 change，Git 成功后回填 commit；启动时 `ReconcileGit()` 补 pending Git audit。

Import：`commit_journal` 使用 PREPARED -> SOURCE_DONE -> GIT_DONE -> DONE，保存 source/main parents 和 commits；`RecoverCommitJournals()` 可恢复异常中断。

### 4.3 Git capability 与 bare repository 修复

Git adapter 已覆盖：

- merge-base；
- commit -> tree；
- 临时 `GIT_INDEX_FILE` 三树 merge；
- stage 1/2/3 `ls-files -u` conflict index；
- conflict-free `write-tree`；
- `commit-tree` + CAS ref update。

真实 GitHub CI 首次运行完整 `go test ./...` 时发现 bare audit repository 下：

```text
git read-tree -m ...
fatal: this operation must be run in a work tree
```

已改为：

```text
git read-tree -i -m ...
```

`-i` 只更新隔离 temporary index，不依赖 work tree；本地 bare repo 复现和 GitHub CI 均验证 stage 1/2/3 正常。

修复提交：

```text
6543bb5d0c19b64ab8c92fe4618f3a34c3fcd556
```

### 4.4 libgit2/git2go spike 结论

roadmap 要求独立验证 libgit2/git2go。复核确认 git2go 当前稳定 binding 线 `v34` 对应 libgit2 1.5，而当前 libgit2 1.x 已明显向后演进；为满足 roadmap 字面要求而把生产历史 backend 锁回旧 native dependency 并不合理。

因此初期正式决策为：

- 不把 git2go/v34/libgit2 1.5 引入生产依赖；
- 保留业务层与 `internal/gitstore` 的 adapter 隔离；
- 当前使用已被真实 CI 覆盖的 Git CLI plumbing backend；
- 将来只有在存在维护中的、适配当前 libgit2 版本的 Go binding 并完成目标部署环境 CGO/link 验证后才替换 backend。

技术决策已写入：

```text
doc/tech/git-backend-2609A.md
```

对应提交：

```text
9281d42d7bde0cf4f2c9c654a1389ae929a8ee2a
```

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

后续按真实曲库量级复核 `MergeOrdered()`，去掉 incoming ADD 每次重新 `positions(res)` 的重复整表扫描，改为维护 presence set；新增 30,000 ordered-item 回归和 15,780 Queue-item benchmark。当前执行环境 benchmark 约 14 ms/op，仅作基线记录，不设置脆弱硬阈值。

对应提交：

```text
2177b2270d7ef879f2e7a6963f735ccc8fff7187
a08b010ac09dc0fbc454e11e82653622ca0480e5
```

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

### 5.6 P6/P8 服务级验收

已有 integration tests 锁定：

- active Procedure 时拒绝第二个 ZIP；
- stale resolution；
- HEAD / server-change 变化时 final commit 拒绝；
- cancel 后 Procedure 与上传 ZIP 仍可审计；
- server delete 遮蔽；
- path DELETE + ADD；
- Procedure commit / recovery 主路径。

`./scripts/test.sh` 与 GitHub CI 均已包含 `./internal/app` integration suite，不再漏跑 service-level Procedure tests。

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

后续增加服务级 Queue acceptance test，锁定：

- 同名但不同 source identity 的 Queue 隔离；
- 同一 source Queue 直接播放时复用而不重建；
- 已存在歌曲 add 到 Queue 时移动而不是重复；
- 删除当前 Queue 后切到下一 Queue 并恢复该 Queue 的记忆点；
- stop target 随歌曲 move，目标歌曲被删除后 stop target 清除。

对应提交：

```text
7621a5043f3cae8eb8b76acb38ac06b4060d74c1
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

初期操作闭环已包含：

- server-only Metadata 编辑，不写手机原 Metadata；
- 任意歌曲加入个人 Playlist；
- 个人 Playlist 移出歌曲、移动成员位置、删除；
- 系统派生 Playlist 不开放成员编辑；
- Queue rename/order/reverse/randomize；
- Procedure parser report / Resolution history；
- Agent token 管理员轮换。

### 7.1 长列表复核

真实曲库为 6653 首，旧 `songList()` 会一次生成全部 DOM，同时当前视图搜索输入会因 `render()` 重置 query 而失效。已修：

- 当前歌曲视图 search 不再自清空；
- 每批 250 首追加 DOM；
- `IntersectionObserver` 提前加载下一批；
- 不支持 observer 时保留手动继续显示 fallback；
- `.song` 使用 `content-visibility:auto` / intrinsic size containment，降低不可见行 layout/paint 成本。

对应提交：

```text
ff1a0b7fad37560e5f1d789f7e269c319bdbfde7
3cc489291af6e22a47d83f24a2bc743124536a3c
```

Folder / Album / Genre 分组与 Artist / Album Artist / Composer 三模式基础视图已存在。像素级 Musicolet 复刻、完整多选、全部排序项、艺术家页完整专辑横向轮播、均衡器等继续属于 Remaining Plan，不作为初期主链路退出条件。

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

### 8.1 Musicolet MediaStore URI

真实 2026-08-30 Backup 出现 `musicolet://media-store` source。Agent 已增加严格 mapping：

- 固定 host 必须是 `media-store`；
- 只接受白名单 query keys；
- `p_id` 必须纯数字；
- `p_mt` 只允许空或音频类型 `1`；
- volume 仅接受受限字符；
- `primary` 映射到 `external_primary`，并兼容 `external` fallback；
- 最终仍只能形成 `content://media/<volume>/audio/media/<数字ID>`；
- 不使用 `p_rp` / `p_dn` 拼命令，不提供 shell 执行入口。

实现和测试：

```text
04d12d22a1a2948fdc367bd91b6ef819c2cd85a1
733fa12a03655bd038363a3e842fa10883f9dc59
```

### 8.2 Agent token 加密

已增加：

- `MUSICOLET_MASTER_KEY`；
- `secure_settings`；
- SHA-256(master) -> AES-256-GCM；
- setting key 作为 AAD；
- `MUSICOLET_AGENT_TOKEN` 仅 bootstrap；
- 后续从加密 SQLite 获取；
- 管理员可轮换 token；
- Linux/Windows config 模板同步。

### 8.3 真机 probe 入口

为避免必须启动完整长连接才能验证 Android 文件/URI 可读性，Agent 新增：

```text
-probe <path-or-uri>
```

行为：

- 使用与正式请求完全相同的 `readRange()`；
- 仅读取首 byte；
- 输出真实 size；
- 不连接服务端；
- 不修改手机文件；
- 空读取结果显式报错，不会 index panic。

对应提交：

```text
16406af81bb7060851b4488e9bb67960e322e5e9
8a7c87e8408991067d562eb7f2a63f2f5a8fdfb3
```

Android/Termux 真机 `/system/bin/content` 权限仍必须在目标手机执行 probe 才能最终验收；服务器或 GitHub runner 无法替代 Android ContentProvider 权限环境。

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

## 10. 启动/测试脚本与真实依赖 CI

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

统一测试入口：

```bash
./scripts/test.sh
```

现包含：

```text
go test ./...
go vet ./...
go test -tags=integration ./internal/db ./internal/musicolet ./internal/app
node --check web/*.js
shell syntax checks
```

之前当前容器无法解析 Go module 网络，因此一度只能用临时 stub 检查纯 Go 类型/逻辑。为消除该边界，已新增 GitHub Actions：

```text
.github/workflows/initial-ci.yml
```

过程发现并解决：

1. `go mod download` 只生成 module `/go.mod` hash，不能满足 package checksum；
2. 改用 `go mod tidy` 生成完整 dependency graph；
3. 按 Go 1.23.12 实际 tidy 结果固化 `go.mod` indirect dependencies；
4. 完整 `go.sum` 从 CI artifact 原始字节生成 Git blob，并校验 blob SHA 后提交；
5. 随后真实 unit test 暴露 bare Git `read-tree` 错误并修复。

关键 commits：

```text
91a34f26eab768b9d94b2016aaabc8daf730a9d7  add real-dependency CI
435aa5024b2c14ff7fcf4d5ea264f7648c4fcdeb  tidy go.mod graph
ca79e82ef6dab3e2e065b064710e168102890986  exact CI artifact go.sum
6543bb5d0c19b64ab8c92fe4618f3a34c3fcd556  bare Git merge fix
```

截至 commit：

```text
8a7c87e8408991067d562eb7f2a63f2f5a8fdfb3
```

GitHub Actions `Initial Development CI` run #11 已真实完成并 `success`：

```text
go mod tidy                              PASS
go.mod stable check                      PASS
go test ./...                            PASS
go vet ./...                             PASS
go test -tags=integration \
  ./internal/db ./internal/musicolet \
  ./internal/app                         PASS
node --check web/*.js                    PASS
```

因此真实 `modernc.org/sqlite`、`golang.org/x/crypto` 编译/测试已经完成，不再属于环境未验证项。

---

## 11. P8 规模与主链路验收状态

已经完成并自动化的部分：

- 6653 首真实曲库 Parser 结构基准；
- 54 Playlist / 29282 Playlist item / 14 Queue / 15780 Queue item 基准；
- 30,000 ordered-item merge regression；
- 15,780 Queue-item merge benchmark；
- Snapshot immutability / legacy migration；
- Server M audit；
- Queue source identity / playback memory / stop target；
- Procedure stale / second-upload rejection / head revalidation / cancel audit；
- Git bare repository merge-base / conflict-index；
- ZIP safety / manifest / Java canonicalization；
- Agent range / source allowlist / long-connection replacement；
- 真实 SQLite / Blowfish dependency CI；
- 前端 JS syntax；
- 大曲库 DOM 分批渲染。

---

## 12. 当前仍需要外部真实输入才能完成的验收边界

重新对照 `Initial Development Plans.md` 后，目前仍不能在仓库/CI 内伪造完成的主要边界只剩：

1. **真实 V1 -> Server M -> 真实 V2 完整 E2E**：当前可访问文件中只有一份真实 2026-08-30 Backup；测试入口已存在，但必须取得第二份实际 Musicolet ZIP 才能验收两版本数量、顺序、统计 delta、冲突和最终 commit。
2. **Android/Termux 真机读取链路**：`musicolet://media-store` 与普通路径的 allowlist/readRange 已有单测和 CI，但 `/system/bin/content query/read` 对目标 Android 的实际权限只能通过 `musicolet-agent -probe` 在真机完成。

不再列为未完成的旧边界：

- Go dependency / `go.sum`：已由 GitHub CI 实际解决并全绿；
- SQLite integration：已真实运行；
- Blowfish package：已真实运行；
- bare Git merge：已真实 CI 发现问题并修复；
- git2go/libgit2：已完成技术 spike 决策，当前明确不采用停留在 libgit2 1.5 binding 线的 git2go/v34，生产使用隔离 Git plumbing adapter。

---

## 13. 最近一轮逐项补缺提交

```text
04d12d22  feat: support Musicolet MediaStore URI mapping
733fa12a  test: cover Musicolet MediaStore URI mapping
de673043  fix: close schema cursor before ALTER TABLE
f84eb779  test: cover snapshot immutability and legacy schema migration
ff1a0b7f  fix: preserve current-view search and batch song DOM rendering
3cc48929  perf: contain off-screen song rows
7621a504  test: lock queue source identity and playback semantics
272a6e25  test: verify server changes are marked and audited
2177b227  perf: avoid repeated ordered-list presence scans
a08b010a  test: add real-scale ordered merge regression
91a34f26  ci: verify real Go dependencies and integration tests
7c65a382  test: include service integration suite in test runner
d324d778  ci: generate complete go.sum before real tests
435aa502  build: normalize Go module graph
ca79e82e  fix: replace go.sum with exact CI artifact
d76ce176  ci: rerun verification with committed module checksums
6543bb5d  fix: merge trees against bare Git audit repository
9281d42d  docs: record current Git backend spike decision
16406af8  feat: add read-only Termux source probe
8a7c87e8  fix: validate Termux probe read result
```

后续继续以 `doc/roadmap/master/Initial Development Plans.md` 的退出条件为验收标准；已经进入 Remaining Plan 的像素级 UI、完整排序、多选、均衡器等不倒灌回初期阻塞项。
