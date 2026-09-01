# dev-2609A-step1 初期功能开发记录

- 分支：`dev-2609A-step1`
- 日期：2026-08-30
- 工作方式：未 commit、未 stage
- 目标：完成 `doc/roadmap/master/Initial Development Plans.md` 定义的初期可运行基线，并以真实 Musicolet Backup 验证导入链路

## 1. 需求读取与决策顺序

本轮按用户纠正后的文档位置读取两篇 roadmap：

```text
doc/roadmap/master/Initial Development Plans.md
doc/roadmap/master/Remaining Development Plans.md
```

需求解释严格按产生过程进行：

```text
doc/prompt/first.md
+ doc/prompt/second.md
+ doc/prompt/layout-prompt.md
          ↓
doc/requestment.md
          ↓
doc/roadmap/master/Initial Development Plans.md
+ doc/roadmap/master/Remaining Development Plans.md
```

同时读取了会话项目“Musicolet复刻”中的三个历史会话，用于恢复文档之外的讨论上下文。实现时没有把 roadmap 当作独立需求来源；出现歧义时回查 `second.md` 的 grill-me 确认。

grill-me 对实现影响最大的结论：

- 路径是每个 Musicolet Snapshot 内的歌曲业务身份，不建立 recording/file-instance 双层模型；`file_id` 仅为技术编号。
- 不推断路径 MOVE；旧路径消失、新路径出现即 DELETE + ADD。
- Song Core 是整体冲突单元，Favorite、播放统计、Playback State 分离。
- 原始 Version 永不被服务器编辑；Server M 写 working state 和正式变更记录。
- Queue/Playlist 不允许成员重复，成员顺序参与语义 merge；冲突 resolution 以完整列表为最终单位。
- 切换查看 Queue 不等于切换实际播放 Queue；每个 Queue 记忆 current/progress/stop target。
- 来源关联 Queue 依靠隐式关系表，不依靠名称；名称冲突使用 `#N`。
- Playback State 不参与导入冲突，导入提交必须保留服务器即时值。
- 取消 Procedure 保留其 ZIP、Candidate、Diff、resolution 和报告供审计；活动 Procedure 存在时拒绝新 ZIP。
- Agent 只能读取手机歌曲，不允许写、删、改名或移动手机文件，并使用长期出站连接而非服务端轮询。

## 2. P0：工程骨架与可运行基线

已完成：

- 建立 Go 1.27 module、`cmd/musicoletweb`、配置、结构化日志、signal graceful shutdown。
- 默认监听 `0.0.0.0:4001`，提供 `/healthz`。
- 使用 pure-Go `modernc.org/sqlite`，配置 WAL、foreign keys、busy timeout 和 migration runner。
- `data/*` 默认忽略，仅保留 `data/.gitkeep`；同时忽略本地测试/构建缓存。
- 冻结前端为嵌入式 vanilla SPA；不使用 Go template 承载业务交互，也不要求部署 Node runtime。
- 建立 `internal/gitstore` 窄适配层和 bare repository 初始化/commit/ref CAS。
- 建立 Termux Agent 入口和 Linux/arm64 无 CGO 交叉构建。
- 完成 Windows/Linux 启动脚本，均在每次启动前 tidy/download/verify、重编译服务端、重编译 arm64 Agent。
- Windows 脚本保留项目约定代理：HTTP/HTTPS `127.0.0.1:58591`、SOCKS5 `127.0.0.1:51837`。

Roadmap 变化：没有接入 git2go/libgit2，改用 Git CLI plumbing adapter。原因是目标构建要求 Windows、Linux 单二进制及无 CGO arm64 交叉编译，而 git2go 需要 CGO 和严格匹配的 libgit2 开发/动态库。业务层仍只依赖 adapter，后续替换不影响业务 merge。当前只有线性 `main` ref，不声称完成 libgit2 merge API/source-current 双 ref spike。

## 3. P1：SQLite 数据模型

migration v1 建立：

- Import/Version：`musicolet_versions`、`import_procedures`、`import_artifacts`、`parser_runs`、`candidate_snapshots`；
- Snapshot：songs/playlists/playlist items/queues/queue items/favorites/playcount/settings；
- Working：songs/playlists/queues/items/playback stats/settings/runtime playback；
- Server M：`server_changes`、`change_targets`、`git_commits`；
- Merge：`semantic_diffs`、`merge_conflicts`、`conflict_resolutions`；
- 来源 Queue 关联和搜索历史。

migration v2 新增 `musicolet_versions.current_queue_index`。这是使用真实 Backup 首次提交时发现的缺口：Queue 自己有 current item，但来源快照还必须保存“哪个 Queue 当前活动”。migration 兼容已创建的 v1 数据库，并通过 `PRAGMA table_info` 避免重复 ALTER。

关键约束：Snapshot/working tables 分离；所有 ordered list 显式 position；列表成员路径和 position 双唯一；Playback State 与 Queue Content 分离；SQLite transaction + mutation mutex 串行化 Server M/Import Commit。

相关自动测试覆盖 Snapshot 不可变、Queue/Playlist 去重、Queue 播放位置初始化与跨导入保持、来源 Queue 关联跨导入重绑、随机 source key 碰撞修复等。

## 4. P2：Backup 解密、校验、Parser 与 V1

实现的固定流程：

1. 上传先归档为 `data/imports/<id>/original.zip` 并计算 SHA-256；
2. Blowfish ECB + 固定密钥 + compatible unpadding 解密；
3. 校验 entry 路径，阻止 zip-slip；
4. 用 manifest 的逐文件明文 MD5 和 manifest hash 做整体校验；
5. 写 `parser/files.json` 与 `parser/validation.json`；
6. 解析 DB、播放统计、Playlist、Queue、Favorite、设置；未知格式保留而非静默忽略；
7. 写 Candidate，管理员确认后生成 V1、working state 和 Git commit。

合成测试样本覆盖加密/明文 fixture、manifest 校验、SQLite/JSON/mpl/qstk 解析。真实私人 Backup 未复制进受版本控制区域。

### 真实 Backup 证据

输入文件：`data/musicolet-backup-2026-08-30 06-46-42.zip`（被 `.gitignore` 保护）

- 文件大小：12,958,671 bytes
- SHA-256：`f67c049a7c70188da85624734791b664161d9a1b02b9f979d149b444b6f959b7`
- ZIP entries：89
- manifest entries：87，明文 MD5 matched 87/87
- manifest hash expected/actual：`49103b0a093e00d414e18bb1e87c9e3b`
- validation：`VERIFIED`，hash verified
- kind：62 JSON、23 SQLite、1 text、1 Java Serialization、2 empty
- parse state：78 PARSED、6 PRESERVED_JSON、5 PRESERVED

真实 V1 结果：

- Songs：6684
- Playlists：54，items：29282
- Queues：14，items：15780
- Playback stats：6653
- Settings：6
- current Queue index：13
- current Queue：`至喜²｜H↑`
- current item：86
- progress：152034 ms
- 当前歌曲：`思いっきりサンバ`

Java Serialization 等未用于初期核心领域的文件保留在 artifacts/canonical 报告中，不以“解析成功”伪装。

## 5. P3：Git 与 Server M

Git adapter 使用 stable canonical `state.json`，通过 `hash-object/mktree/commit-tree/update-ref` 创建 commit；业务 merge 不使用 Git 文本 merge。SQLite 先记录 prepared commit，事务成功后 CAS 更新 ref；启动时可依据 SQLite 记录恢复 ref。

已实现 Server M：

- Favorite；
- Song Metadata；
- 服务器永久删除；
- Playlist 创建、成员替换/移动、删除；
- Queue 创建、改名、删除、上下重排、成员替换、插队、移到队尾、反向、随机化；
- 服务器播放完成统计。

每个 M 写 revision、base version、target/related song targets、before/after、Git commit，并更新 `has_server_changes`。活动 Procedure 在 M 后退回 REVIEWING，提交 CAS 拒绝过期 server revision。

## 6. P4/P5：核心 UI、Queue 与 Playback

SPA 已提供顶部 Tab：播放队列、正在播放、文件夹、专辑、艺术家（Artists/Album Artists/Composers）、风格、歌曲清单、搜索、更多。共用 Song List 显示标题/艺术家/专辑/时长、当前项、M 标记、三点菜单和视图内搜索。

主要行为：

- Queue 下拉查看、多 Queue、管理/改名/删除/上下顺序、反向和随机化；
- 查看 Queue 与实际播放 Queue 分离；
- 来源直接播放与来源关联 Queue 复用；
- 已有关联 Playlist 的乱序播放不再次打乱；
- 插队待播、排到队尾时移动已存在成员而不复制；
- current song、progress、play/pause、prev/next、seek、speed、shuffle/repeat state、stop target；
- 每 15 秒保存播放进度，ended 时记录播放次数；
- Folder/Album/Artist/Genre/Playlist 详情和分组搜索；
- 公开只读 `/now-playing` 页面；
- 桌面与窄屏布局已做浏览器人工核对。

真实数据 V2 首次验证时发现 no-op merge 按 `source_key` 重排顶层 Queue，导致当前 Queue 工作索引 13→5。修复为“保留 OURS 顶层顺序，按 THEIRS 顺序追加来源新增列表，再补 BASE-only key”，并增加 `TestNoOpImportPreservesTopLevelQueueOrder`。全新隔离数据库重跑后 V1/V2 均保持索引 13、current item 86、progress 152034 ms。

## 7. P6：V2、Diff、Conflict 与 Procedure

实现完整状态集合：`PARSING / REVIEWING / RESOLVING / READY_TO_COMMIT / COMMITTED / CANCELLED / FAILED`。partial unique index 与 API 共同保证一个 active Procedure。

Semantic Diff/Merge 覆盖 Song、Favorite、Stats、Settings、Playlist、Queue 和 raw canonical character diff。规则测试覆盖：

- THEIRS unchanged / OURS changed；
- OURS unchanged / THEIRS changed；
- 两边相同；
- Song Core 双改；
- delete masking；
- 新路径 ADD；
- ordered MOVE + ADD、DELETE + MOVE、ADD/ADD different positions；
- 完整列表 resolution；
- settings three-way；
- stale resolution；
- commit 前 server revision CAS；
- Playback State 不被导入回滚。

播放量使用文档明确公式：

```text
resolve = previous_resolve
        + (current_server - previous_resolve)
        + (current_import - base_import)
```

即 `current_server + current_import - base_import`，周/月/年分别计算，Last Played 取较晚值。

`second.md/requestment.md` 的 V(n+3) 示例文字给出 base=116、server=150、import=120，同时声称 server_change=4/result=164；该算术与公式以及前一步 previous_resolve=146 不一致。实现遵循明确公式，结果为 `150 + (120-116) = 154`，测试序列固定为 105/120/146/154。此处没有暗中修改公式来迎合错误示例。

### 真实 V2 与取消测试

在全新隔离数据库对同一真实 ZIP 做 V1→V2：

- V2 状态 READY_TO_COMMIT；Semantic/raw Diff 0；Conflict 0；
- 提交后 version=2；songs/playlists/queues 数量不变；
- current Queue index 13、current item 86、progress 152034 ms 全部保持；
- bare Git `main` 正好 2 commits。

同一 ZIP + 隔离环境 Server Metadata M 另做过一次验证：V2 Diff/Conflict 0 时，服务器 `[E2E]` 标题仍被保留，Playback Queue/song/progress 仍保持。

活动 Procedure 验证：

- 第一个上传返回 PARSING；第二个上传返回 HTTP 409 `an unfinished import procedure already exists`；
- rejected 上传前后 import artifact 目录数量不变，未遗留第二份私有 ZIP；
- 取消后等待后台 parser 越过原完成时点，状态仍为 CANCELLED，Candidate/Diff 未迟到写入；
- `PutCandidate`、`SaveAnalysis`、`FailProcedure` 均有终态 guard，并新增取消不可复活回归测试。

## 8. P7：Termux Agent 与媒体

Agent v1 已实现：

- 长期出站 WebSocket、Bearer token、hello/version；
- 断线指数退避；
- offset/length/size/EOF 分段读取，每段最大 1 MiB；
- Android primary `content://` URI 到 `/storage/emulated/0/...` 映射；
- configured roots + symlink-resolved containment；
- 只允许 regular file read；没有任何 write/delete/rename/move 命令；
- 默认拒绝明文 HTTP，只有显式 `MUSICOLET_AGENT_ALLOW_HTTP=1` 才允许本地调试。

服务端 Hub 支持单 active Agent、请求路由、pending request、context cancel、断线清理；Cache 使用 partial file + sync + atomic rename，缓存命中由 `http.ServeContent` 支持 Range/seek。离线 cache miss 返回明确错误。

arm64 binary 可交叉构建；本机没有实际连接用户手机，因此“真实设备 WSS + 真文件播放”仍是部署环境验收项，不能用单元测试替代。

## 9. P8：整合结果与剩余验收边界

已自动化或真实隔离覆盖：首次真实 ZIP、同 ZIP V2、Server M 保持、Song Core conflict、播放量多版本公式、Queue merge、stale resolution、active 409、cancel audit/terminal guard、commit CAS、Snapshot immutable、列表去重、Agent 明文 HTTP/root escape 拒绝、认证/TOTP、公开页面和桌面/移动布局。

真实量级已加载 6684 Songs、54 Playlists、29282 Playlist items、14 Queues、15780 Queue items；Candidate/merge 可在普通开发机完成，没有只用几十首 demo 作为性能结论。

尚需外部条件才能关闭的 roadmap 退出项：

1. 当前只有一份真实 ZIP。不同真实 V1/V2 的手机端变化无法实测；本轮用同一真实 ZIP验证幂等性和 Server M 保持，用 synthetic tests 覆盖变化/conflict。
2. 当前没有已连接 Termux 手机。真实手机音频与 WSS 链路需部署后验收。
3. Git 使用 CLI adapter/单 main ref，不是 roadmap 原写法的 git2go/libgit2 + source/current refs；详细取舍见技术总览。

因此，代码层的初期功能基线已形成，但上述三项不能标记为“真实环境已验收”。它们不是偷偷挪入后期计划，而是明确保留的部署/样本/技术偏差记录。

## 10. 安全与隐私

- 真实 ZIP 始终位于 ignored `data/`；未复制到 fixture 或文档。
- 文档只记录计数、hash 和必要业务核对值，不记录完整私人路径列表。
- rejected upload 会清理尚未成为 Procedure 的随机目录；已取消 Procedure 的 artifacts 按需求保留。
- 测试用 SQLite、解密 artifact、凭据、日志和二进制只存在 `.codex-e2e/.codex-build` 隔离目录，并在结束前清理。
- 管理 API 默认拒绝匿名；写请求校验 CSRF；生产要求 TOTP 和足够长度的 secret/token。

## 11. 本轮缺陷修复摘要

- 适配真实 manifest JSON 结构并完成 87/87 MD5 与 manifest hash 校验。
- 修复 CurrentQueueIndex 未进入历史 Version schema/加载流程。
- 修复 subsequent import 时 Queue ID 重建导致 runtime playback 失联。
- 修复导入 commit 使用分析时旧 Queue progress 覆盖最新服务器状态。
- 修复 source→Queue 关联在 working tables 全量替换后丢失。
- 修复设置只存在 Snapshot、没有 working/materialized/merge 路径。
- 修复 raw diff 对大 SQLite canonical 文本的内存风险。
- 修复 Windows 高速创建 Queue/Playlist 时伪随机 source key 碰撞，改用 crypto/rand。
- 修复 no-op V2 顶层 Queue 被 source key 排序。
- 修复取消/后台 parser 竞态导致终态可能被复活。
- 修复 active Procedure 409 后遗留孤立 ZIP 目录。
- 修复 public `/now-playing` 被认证 SPA 路由覆盖。
- 修复 Last Played 源毫秒时间戳未统一成秒。
- 修复启动脚本首建空配置后仍下载、编译，最终才失败的问题；Windows/Linux 现在都会在构建前校验各自模式的必填配置，首次生成模板后立即退出。

## 12. 最终验证命令

在交付前执行：

```text
go test -count=1 ./...
go vet ./...
go build ./cmd/musicoletweb
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/musicolet-agent
node --check internal/webui/static/app.js
bash -n scripts/linux-alyhk.start.sh
git diff --check
git status --short --branch
```

Windows启动脚本同时做静态审阅：代理常量、默认 4001、private config、每次 rebuild、arm64 环境恢复和 errorlevel 分支均与 example 的启动策略一致。

最终结果：

- `go test -count=1 ./...`：PASS；
- `go vet ./...`：PASS；
- Windows server build：PASS；
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` Agent build：PASS；
- `node --check internal/webui/static/app.js`：PASS；
- Git for Windows Bash `-n scripts/linux-alyhk.start.sh`：PASS；
- 本轮文件 trailing-whitespace 检查：PASS；
- 真实 Backup `git check-ignore`：由 `.gitignore` 的 `data/*` 命中；
- Git index：clean，没有 stage。

全仓 `git diff --check` 只报告既有用户改动 `doc/prompt/prompt-2608C.md` 的 trailing whitespace。该文件不是本轮实现文件，按“保留用户改动”原则未修改；排除该文件后的本轮 tracked 文件检查通过。
