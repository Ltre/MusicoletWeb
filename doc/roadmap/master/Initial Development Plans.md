# MusicoletWeb 初期开发计划

> 本文是项目的初期主开发路线。需求事实与最终方案以 `doc/requestment.md` 为最高优先级；`doc/prompt/second.md` 与 `doc/prompt/layout-prompt.md` 用于补充交互细节；`doc/prompt/first.md` 及项目早期语音讨论只作为设计背景，凡与后续结论冲突的内容均不得继续采用。
>
> 初期阶段的目标不是先把 Musicolet 所有按钮逐个做完，而是先把最难返工、决定整个系统正确性的主链路打通：**Musicolet 原始 ZIP → 不可变 Snapshot → 当前服务器工作状态 → Server Change/Git → 下一版 Import Procedure → Semantic Diff/Conflict Resolution → 原子提交**，并在此基础上建立可用的 Musicolet 风格核心 Web UI 与最小真实播放链路。

---

## 1. 初期阶段目标

初期开发完成后，系统至少应具备以下完整闭环：

1. Go 服务端可以稳定启动，SQLite 自动初始化/迁移，管理员通过账号密码 + TOTP 登录。
2. 可以上传真实 Musicolet Backup ZIP，并完成 Blowfish 解密、结构校验、解析和原始 ZIP 永久归档。
3. 第一次导入能够生成完整的 `Musicolet V1` 不可变 Snapshot，并从中构建服务器当前工作状态。
4. Web 可以正确浏览 All songs、Queue、Playlist、Folder、Album、Artist / Album Artist / Composer、Genre 等核心数据视图。
5. 服务器可以产生正式 M（Server Change），至少覆盖歌曲 Metadata 修改、服务器侧歌曲删除、Favorite、Playlist/Queue 成员增删移动和 Queue 顺序操作，并形成 Git 历史和对象 change 标记。
6. 第二份 Musicolet ZIP 可以进入唯一活动的 Import Procedure，生成 Candidate Snapshot、Semantic Diff、冲突列表并支持暂存。
7. 三方合并严格实现 `BASE / OURS / THEIRS` 规则；Song Core、Playlist/Queue、播放统计、服务器删除遮蔽、Playback State 等按需求文档各自语义处理。
8. Procedure 支持“保留服务器当前改动 / 采用新版 Musicolet / 手动处理”；已解决冲突若期间被新的 M 命中，必须变为 stale 并重新确认。
9. Procedure 最终提交是严格原子事务，并生成正式新 Musicolet Version、服务器新工作状态和 Git merge 历史。
10. 最小 Termux Go Agent 能以只读方式建立长期出站连接，在不轮询、不修改手机文件的前提下，为服务器按需提供当前曲库歌曲的读取/流式传输，使 Web 能完成真实播放。

初期阶段结束时，即使后续 UI 仍有大量 Musicolet 细节尚未补齐，**数据演进、导入、冲突、版本、播放基础链路必须已经可信**。

---

## 2. 初期阶段明确不做

以下内容不应阻塞初期主链路，统一放入后期计划：

- UI 像素级/细节级全面复刻；
- 所有歌曲菜单、全部多选操作、所有排序项一次性补齐；
- 完整分享中心与公开分享页面；
- 音频裁剪器；
- Web Audio 高级均衡器；
- PCM SHA-256 参照库的完整产品化；
- 本系统完整备份/还原；
- 完整语言切换、主题、Tab-bar 位置偏好；
- 完整标签解析偏好编辑器；
- 复杂缓存管理 UI；
- 自动 LRU 缓存淘汰；
- 自动识别路径 MOVE；
- Chromaprint；
- recording_id / file_instance_id；
- 服务器写回、删除、移动、重命名手机文件或修改手机 Metadata；
- 服务器永久存储整套歌曲文件。

但初期设计的数据结构/API 必须为后续功能保留合理扩展点，不能通过错误的“临时简化”破坏最终模型。

---

## 3. 不可违反的架构约束

### 3.1 数据身份

- Musicolet 某个版本内，歌曲忠实按其导出的路径/URI定位。
- 可以有服务器内部 `file_id`，但它仅是技术编号，不承担跨 Musicolet Version 的永久身份。
- 路径变化按旧路径 DELETE + 新路径 ADD 处理，不尝试恢复旧 `file_id`，不自动判断 MOVE。
- 不建立 `recording_id`、`file_instance_id` 或“一首歌曲对应多个文件实例”模型。
- 两个不同路径即使 PCM SHA-256 完全一致，也必须视为两个独立歌曲数据块。

### 3.2 版本与工作状态

必须永久区分：

1. Musicolet 原始不可变版本；
2. 当前服务器工作状态；
3. 从最近正式 Musicolet 版本衍生出的 Server Changes。

服务器修改绝不能反写污染历史 Musicolet Snapshot。

### 3.3 Merge

- Song Core 是歌曲级整体冲突单元，不做 Metadata 字段级自动 merge。
- Playlist / Queue 使用成员级有序语义 merge。
- Playback Count/周/月/年统计使用需求文档确定的 delta 公式，不用普通 ours/theirs 覆盖。
- Last Played 取最晚值。
- Playback State 不参与导入冲突，始终以服务器状态为准。
- 新版 Musicolet 某数据项若与 BASE 相同，则无权覆盖服务器既有 M。
- 服务器已删除歌曲而手机只增加播放次数，不构成歌曲本体冲突。

### 3.4 手机 Agent

Agent 必须严格只读：

- 不写文件；
- 不删除；
- 不移动；
- 不重命名；
- 不新增歌曲文件；
- 不改 Metadata；
- 不修改 Musicolet 数据。

网络上不做服务器轮询；采用长期出站连接、低频 heartbeat、断线退避、请求复用。

---

## 4. 建议的初期代码结构

实际目录可以随实现调整，但职责边界建议从第一天明确：

```text
cmd/
  server/                 # Web 服务端入口
  agent/                  # Termux/Android arm64 Go Agent 入口

internal/
  config/                 # 环境变量、运行配置
  db/                     # SQLite、migration、transaction
  auth/                   # Admin + session + TOTP

  musicolet/
    decrypt/              # Backup 解密
    archive/              # 原始 ZIP / procedure 文件归档
    parser/               # SQLite/JSON/mpl/qstk/... 解析
    canonical/            # 稳定规范化表示
    snapshot/             # Musicolet Snapshot
    diff/                 # Semantic Diff
    merge/                # 业务三方 merge
    procedure/            # Import Procedure 状态机

  library/                # Song/Album/Artist/Folder/Genre 读模型
  playlist/               # Playlist 业务
  queue/                  # Queue + queue playback state
  playback/               # 当前播放、播放统计、sleep/stop target 基础
  changes/                # Server M、change marks
  gitstore/               # libgit2/git2go 适配层

  agenthub/               # 服务端与手机 Agent 的长连接/请求路由
  media/                  # 按需流式传输接口，后期再扩缓存

  httpapi/                # API handlers / middleware
  web/                    # 前端静态资源/构建产物入口（视前端选型）

migrations/
web/                      # 前端工程（若采用独立构建）
scripts/
doc/
data/                     # 运行数据，不提交用户数据到 Git
```

必须用接口隔离 Git 实现与业务语义 merge。业务层不得散落直接调用 git2go。

---

## 5. 阶段 P0：项目骨架、工程约束与可运行基线

### 5.1 任务

- 初始化 Go module、服务端入口、配置模块、日志、优雅退出。
- SQLite 连接、WAL/事务策略、migration runner。
- 建立 `data/` 运行目录规则和 `.gitignore`，确保真实用户 ZIP、数据库、缓存不会误提交。
- 建立基本测试框架：Go unit/integration tests；前端测试方案在选定前端技术后补齐。
- 建立开发脚本：启动、测试、migration、agent arm64 构建。
- 明确并冻结首版前端技术栈；如果需求文档没有指定框架，不应为了赶进度把前端业务逻辑硬塞进 Go template。应优先保证移动端 SPA/类 App 交互能力、拖拽、浮层、长列表性能和 Web Audio 可扩展性。
- libgit2/git2go 做独立 spike：验证目标开发/部署环境的 CGO、libgit2 版本绑定、静态/动态链接方式、merge API、repository 初始化与基本 commit。

### 5.2 输出

- 服务端可启动并返回 health endpoint。
- 空 SQLite 可自动完成 migration。
- Git repository 可初始化并由 Go 创建最小 commit。
- 前端空壳可显示顶层 Tab 导航框架。
- Agent 可交叉编译/直接构建为 Android/Termux 可运行的 arm64 binary（此阶段只需启动和版本输出）。

### 5.3 退出条件

不得在 libgit2/git2go 构建链尚未跑通的情况下大规模写依赖其历史模型的业务代码。

---

## 6. 阶段 P1：SQLite 核心数据模型与不可变版本基座

这是初期最重要的 schema 阶段。先建立能够正确表达最终模型的数据结构，再做 UI。

### 6.1 至少需要的领域

#### Musicolet 导入/版本

```text
musicolet_versions
import_procedures
import_artifacts
parser_runs
candidate_snapshots
```

记录：原始 ZIP SHA-256、文件路径、Procedure 状态、`parser_version`、正式 version number、BASE version、Candidate 与正式版本关系、创建/提交/取消时间。

#### Snapshot 数据

使用独立的 version/snapshot 关联表达至少：

```text
songs_snapshot
playlists_snapshot
playlist_items_snapshot
queues_snapshot
queue_items_snapshot
favorites_snapshot
playcount_snapshot
settings_snapshot
```

历史 Snapshot 行不可被服务器编辑接口 UPDATE。

#### 当前工作状态

为服务器实际可编辑状态建立明确 working tables/read model，避免运行时每次都把 Snapshot + 所有 M 从零重放：

```text
songs
playlists
playlist_items
queues
queue_items
favorites
playback_stats
queue_playback_state
runtime_playback_state
```

#### Server Change

至少有：

```text
server_changes
change_targets
```

每个 M 要能记录业务对象、操作类型、变更前/后必要数据、Git commit、相对哪个正式 Musicolet Version 产生、是否仍为有效 overlay、对象 `has_server_changes` 快速标记。

#### Procedure/Conflict

预留：

```text
semantic_diffs
merge_conflicts
conflict_resolutions
resolution_patches
```

必须能记录 BASE/OURS/THEIRS、resolution、解析时 server head、stale 状态。

### 6.2 原则

- Snapshot 与 current working state 不得混在同一组可随意 UPDATE 的行里。
- 所有 ordered list 必须有稳定排序表达，不依赖 SQLite 自然行顺序。
- Playlist/Queue 成员唯一约束必须阻止同一路径歌曲在同一列表内重复。
- 播放统计数据与 Song Core 分离。
- Playback State 与 Queue Content 分离。

### 6.3 测试

通过数据库测试证明 V1 Snapshot 不受工作态修改污染、M 修改 working state 后 Snapshot 保持不变、同一 Playlist/Queue 无重复歌曲、transaction 回滚不会留下半更新 ordered list。

---

## 7. 阶段 P2：Musicolet Backup 解密、解析与第一次全量导入

### 7.1 Backup Archive

上传 ZIP 后先归档再解析。建议：

```text
data/imports/<procedure-id>/
  original.zip
  decrypted/
  canonical/
  parser/
```

原始 ZIP 永不被修改。

### 7.2 解密

实现当前已验证的 Musicolet Backup 解密：Blowfish ECB、固定密钥、PKCS#5/兼容 padding，并使用 backup manifest/hash 尽可能做明文校验。必须返回逐文件状态与整体校验结果。

### 7.3 Parser

按实际 Backup 类型实现：

- `DB_SONGS_LOG`；
- `PCs_Y_* / PCs_M_* / PCs_W_*`；
- `.mpl` Playlist；
- `0.qstk` Queue；
- `0.favs`；
- 其他当前需求直接使用的设置/状态文件；
- Java Serialization 等格式需要独立 parser，未知内容不得静默丢弃。

每种 parser 输出稳定领域 DTO，业务代码不直接依赖 Musicolet 缩写字段。

### 7.4 第一次导入

无正式 Musicolet Version 时：创建 Procedure → 解密解析 → Candidate Snapshot → 展示摘要 → 提交生成 `Musicolet V1` → 构建 working state → 初始化 Git source/current 基线。

### 7.5 回归样本

真实私人 Backup 不提交公共仓库。制作脱敏/合成 fixture、synthetic SQLite/JSON/mpl/qstk、已知明文/hash 的小型加密 fixture。

### 7.6 验收

对真实 Backup 人工核对 All songs、Playlist 数量/顺序、Queue 数量/顺序/播放状态、Favorite、总/年/月/周播放数、Last Played、Album/Artist/Genre/Folder 归类。

---

## 8. 阶段 P3：Git 历史与 Server Change 基础

### 8.1 Git 模型

建立 Musicolet source history 和 server/main history。Git tree 存稳定规范化业务表示，不把 SQLite DB 文件直接作为 merge 对象。

### 8.2 Git 适配层

实现 repository init/open、commit、tree/blob、refs、merge-base、commit/tree merge、conflict index、原子更新 ref 封装。业务 merge 仍由 Go 自己实现。

### 8.3 首批 Server M

先打通：Favorite、Song Metadata server-only 修改、服务器永久删除歌曲、Playlist 加入/移出/移动、Queue 加入/移出/移动、Queue 手动移动/反向/随机化、服务器播放统计增量。

每次业务操作必须让 SQLite working state、server_change、change mark、Git commit 保持一致，设计失败补偿/原子策略。

---

## 9. 阶段 P4：核心 Musicolet 风格浏览 UI

### 9.1 顶层 Tab

实现完整导航骨架：播放队列、正在播放、文件夹、专辑列表、艺术家列表、风格、歌曲清单、搜索、汉堡菜单；Artist 子 Tab 为 Artists / Album Artists / Composers。

### 9.2 公共歌曲列表组件

尽早统一 Song List：标题/艺术家/专辑/时长、可选封面、当前项状态、底部视图内搜索、三点菜单扩展槽、多选扩展槽、长列表能力预留。

### 9.3 初期视图

达到可用：Queue 下拉与多 Queue、All songs、Playlist、Folder、Album、Artist 三类基础详情、Genre、基础全局搜索、当前歌曲基础信息页。

### 9.4 UI 对照

以 `doc/prompt/layout-prompt.md` 为核对清单。初期优先页面层级、关键控件区域、移动端交互、数据语义正确；图标/间距/全部菜单细节后期精修。

---

## 10. 阶段 P5：Queue、Playlist 与播放状态核心行为

### 10.1 Queue

实现多 Queue、上下顺序、改名/删除、禁止重复歌曲、查看 Queue 与实际播放 Queue 分离、每 Queue 独立 current song + progress、删除当前 Queue 后切下一 Queue 记忆点继续、插队待播、排到队尾、加入已有歌曲时移动到队尾。

### 10.2 来源 → Queue 隐式关联

建立专辑/歌单/风格/作者/文件夹与 Queue 的隐式关联，不能只按名称复用。名称碰撞使用 `AAA #编号`。

### 10.3 直接播放与乱序播放

来源视图直接点击：复用该来源关联 Queue 并跳到歌曲；不存在则创建。Playlist 乱序：已有关联 Queue 则复用不重新打乱，不存在则首次创建随机化 Queue。Queue “随机化”是永久 M；播放“随机/顺序”只是运行状态。

### 10.4 Playback State

实现当前 Queue、当前歌曲、播放/暂停、progress、上一/下一、快退/快进基础、每 Queue 独立恢复、顺序/随机状态、stop target 基础。Playback State 不进入导入冲突。

---

## 11. 阶段 P6：第二次导入、Semantic Diff 与 Import Procedure

这是初期最关键的验收阶段。

### 11.1 Procedure 状态机

至少包含 `PARSING / REVIEWING / RESOLVING / READY_TO_COMMIT / COMMITTED / CANCELLED / FAILED`。同一时间仅一个未结束 Procedure；活动 Procedure 存在时拒绝新 ZIP；ZIP 不可替换；取消后所有 artifacts/diff/resolution 保留。

### 11.2 Candidate Snapshot

第二次 ZIP 全量解析成 Candidate，不修改正式版本/工作数据库。

### 11.3 Semantic Diff

覆盖 Song ADD/DELETE/Song Core MODIFY；Playlist/Queue create/delete/add/remove/move；Favorite；总/年/月/周播放统计和 Last Played；Settings 可识别变化。

### 11.4 三方规则

自动化测试锁死：

```text
THEIRS == BASE && OURS != BASE => 保留 OURS
OURS == BASE && THEIRS != BASE => 采用 THEIRS
OURS == THEIRS => 自动接受
OURS != BASE && THEIRS != BASE && OURS != THEIRS => 进入业务冲突判定
```

Song Core 不字段级 merge。Playlist/Queue 做成员级语义 merge。SERVER DELETE B + PHONE MOVE B 不冲突，服务器删除继续有效。Server Delete 遮蔽不因播放次数增长失效；新路径是新 ADD。

播放次数固定：

```text
server_change = current_server - previous_resolve
musicolet_change = import - base
resolve = previous_resolve + server_change + musicolet_change
```

提交后 `next base = current import`、`next previous_resolve = current resolve`。周/月/年分别计算。Last Played 取较晚值。Playback State 保留服务器值。

### 11.5 Conflict Resolution

支持“保留服务器当前改动 / 采用新版 Musicolet / 手动处理”。手动处理进入完整对象编辑器，保存最终状态和详细 patch。

### 11.6 Stale Resolution

Procedure 保存 `last_analyzed_server_head`。暂存期间若产生新 M，重新分析；命中已处理 conflict 的 resolution 标记 stale，展示旧决定、当时 OURS、当前 OURS、THEIRS，并要求重新确认。

### 11.7 原子提交

Commit 前 CAS/校验 server head。变化则拒绝并刷新。成功一次完成 Candidate → V(n+1)、Snapshot、source history、Resolution、working state、Git merge commit、Server Change overlay 标记、Procedure COMMITTED。任何一步失败不得出现半导入版本。

---

## 12. 阶段 P7：最小 Termux Go Agent 与真实播放链路

### 12.1 Agent v1

启动、长期出站连接、Agent 认证、heartbeat、断线指数退避、按路径只读歌曲、Range/分段流式读取、返回 stat/错误、明确拒绝写命令。服务端不轮询 Agent。

### 12.2 Agent Hub

在线连接注册、请求路由、超时/取消、并发流控制、离线状态、流数据桥接浏览器。

### 12.3 Web Audio 最小播放

播放/暂停、seek、上一/下一、Queue progress 记忆；Agent 离线且没有可用数据源时明确提示无法播放。

### 12.4 安全验收

审计 Agent，不得存在针对歌曲文件的 create/write/remove/rename/move API 路径。

---

## 13. 阶段 P8：初期整合、回归测试与冻结基线

### 13.1 端到端场景

至少覆盖：首次真实 ZIP → V1；V1 数据对照；服务器 Metadata 与手机未变；双方 Song Core 冲突；服务器删除 + 手机 play-count；手机改路径；Queue 自动 merge/冲突；连续多版本播放量公式；Procedure 暂存后 stale resolution；活动 Procedure 拒绝新 ZIP；提交前 HEAD 变化；取消 Procedure 可审计；Agent 在线播放/离线错误；Queue/Playlist 无重复成员。

### 13.2 性能基线

按真实量级测试数千歌曲、50+ Playlist、多 Queue、数万 Playlist item、长列表、搜索、全量 Candidate 解析、Semantic Diff；不能只用几十首 demo。

### 13.3 冻结条件

两次真实 ZIP 完整 import/merge 已跑通；Server M 跨导入正确；Git/SQLite 可审计；Queue/Playlist 语义可靠；最小真实播放可用；管理员认证已保护非公开功能；关键规则有自动化测试。满足后才进入后期计划。

---

## 14. 初期管理员认证

初期即实现：管理员账号/密码从环境变量读取；Google Authenticator/TOTP；服务端 Session；CSRF/session/cookie 基础安全；除明确 public route 外默认拒绝匿名访问。

---

## 15. 开发顺序与依赖

```text
P0 工程与 Git spike
   ↓
P1 数据模型
   ↓
P2 Parser + V1
   ↓
P3 Server M + Git
   ↓
P4 核心浏览 UI
   ↓
P5 Queue/Playback 业务
   ↓
P6 V2 Procedure + Merge
   ↓
P7 Agent + 真实播放
   ↓
P8 整合验收
```

允许 P4 前端组件与 P3 后端并行，但 P6 不应在 P1/P2/P3 尚不稳定时仓促实现。

---

## 16. 每阶段提交与开发记录

每阶段保持可回滚小提交，并在 `doc/devlog/` 记录完成项、关联需求、migration、关键技术决定、实测、已知问题、roadmap 变化。涉及数据模型和 merge 规则的提交必须附带测试。

---

## 17. 初期最终交付物

```text
Go server
SQLite migrations
Musicolet decrypt/parser
Snapshot/version engine
Git/libgit2 adapter
Server Change engine
Import Procedure engine
Semantic Diff/Merge engine
Core Web UI
Queue/Playlist core behavior
Playback state engine
Termux Go Agent source
arm64 build script
Core integration tests
Deployment/dev scripts
Updated devlog/technical docs
```

初期结束时，项目应已从“静态曲库展示”进入“具有可信版本演进与真实播放能力的 Musicolet 镜像系统”阶段。
