# MusicoletWeb 后期剩余开发计划

> 本文承接 `Initial Development Plans.md`。只有初期主链路通过真实 Musicolet Backup、Server Change、第二次导入三方合并和真实播放验收后，才进入本计划。
>
> 后期阶段不再重新设计歌曲身份、Snapshot、Server Change、Procedure、Merge 基本模型。若后期功能暴露出新问题，优先在既有模型上扩展；不得因为实现某个 UI 功能而重新引入已经明确否决的 `recording_id`、`file_instance_id`、Chromaprint、路径 MOVE 自动识别等方案。

---

## 1. 后期开发总目标

后期的目标是把初期“架构正确、核心可用”的系统补齐为接近 Musicolet 使用体验的完整 Web 播放器，重点包括：

1. 完成各 Tab、详情页、浮层、菜单、多选、排序和移动端布局的高完整度复刻。
2. 完成 Now Playing、歌词、播放速度、循环/随机、睡眠倒计时、指定歌曲播完即停等播放能力。
3. 完成服务器/浏览器两级歌曲缓存与管理 UI。
4. 完成 PCM SHA-256 参照库和手机端批量只读扫描能力。
5. 完成公开分享体系和分享记录。
6. 完成本系统 Backup/Restore。
7. 完成标签多值解析偏好、主题、语言、Tab-bar 位置等设置。
8. 完成 M3U 导出、音频裁剪器等工具能力。
9. 持续强化 Import Procedure、原始结构字符 Diff、Git 审计、性能与兼容性。

---

## 2. R1：完整 UI/交互复刻与组件统一

### 2.1 目标

从“核心页面可用”推进到“主要操作位置、信息密度、浮层形态和移动端操作习惯接近 Musicolet”。

### 2.2 Queue TAB 完整化

补齐：

- Queue 下拉浮层拖拽调整 Queue 上下顺序；
- Queue 改名与删除完整交互；
- 顶部操作栏：继续播放、排序、当前播放位置/总数、保存、三点菜单；
- 保存为 Playlist、加入其他 Playlist；
- 分享 Queue、导出 M3U、进入多选模式；
- 每首歌曲三点菜单全部条目；
- 右上 Favorite 心形状态；
- 底部固定视图内搜索；
- 多选底部操作浮层；
- 高级选择器：全选、反选、连续区间选择。

Queue 排序全部补齐：

- 随机化、反向；
- 标题升/降；
- 文件名升/降；
- 文件夹升/降；
- 专辑升/降；
- 歌手升/降；
- 专辑作者升/降；
- 作曲人升/降；
- 风格升/降；
- 修改日期升/降；
- 新增日期升/降；
- 上次播放日期升/降；
- 最常播放优先；
- 最少播放优先。

所有会改变 Queue 内容/顺序的操作继续产生 Server M。

### 2.3 Now Playing 完整化

补齐：

- 大封面；
- 点击封面切换/显示歌词；
- Title / Artist / Album；
- Favorite；
- Info 浮层；
- 加入/移出 Playlist 浮层；
- 播放速度 0.25–2.0；
- Equalizer；
- 三点菜单；
- 顺序/随机共位切换；
- 独立循环控件；
- 进度条与时间；
- 上一曲/快退/播放暂停/快进/下一曲。

Info 浮层完整显示：封面、文件名、路径、Title、Artist、Album、Album Artist、Composer、Genre、歌词、Track、Disc、Year、Comment、时长、比特率、采样率、采样位数、格式、编码、声道、文件大小、入库时间、修改时间、最近播放时间、播放数。

### 2.4 Folder / Album / Artist / Genre

按 `layout-prompt.md` 完成搜索栏、排序按钮、列表行结构、详情页、歌曲统计、总时长、横向 Album 选择器、详情页操作按钮、多选和底部固定搜索。

Artist 继续支持 `Artists / Album Artists / Composers` 三类解析视图。

### 2.5 Playlist TAB

完整实现：

- 系统保留列表：全部歌曲、喜爱、最近加入、最近播放、最多播放、尚未播放；
- Personal Playlist 区域；
- `+ 新建歌单`；
- 搜索歌单；
- Playlist 歌曲详情页；
- 随机播放、排序、三点菜单；
- 多选。

系统派生 Playlist 不允许手工增删成员；Favorite 通过心形修改。

服务器仍禁止修改已有 Playlist 名称。

### 2.6 Search TAB

完成输入即搜、搜索历史、清除搜索历史，以及顶部结果分组统计：Album、Artist、Album Artist、Folder 等有结果才显示。点击统计在浮层显示该类结果。

歌曲结果 Header 显示“找到 N 首歌曲”，右侧提供随机播放和三点菜单。

---

## 3. R2：播放引擎完整化

### 3.1 播放模式

完善顺序/随机播放模式和列表循环、单曲循环。

明确区分：

- Queue “随机化” = 永久修改 Queue 顺序；
- 播放随机模式 = runtime playback state。

### 3.2 指定歌曲播完即停

完善 Stop Target：

- 可在当前歌曲之后任意歌曲 S 设置；
- 播放到 S 末尾才停止；
- S 移动后目标跟随；
- S 被移出 Queue 后自动取消；
- 切换 Queue 时目标保留在原 Queue；
- 回到原 Queue 继续有效。

### 3.3 睡眠倒计时

实现三种标准模式：

1. 在 `hh:mm` 之后停止；
2. 在 N 首歌之后停止；
3. 在指定歌曲播放完即停。

第三项从汉堡菜单进入时提示用户前往 Queue 的歌曲菜单设置。已经存在 Stop Target 时，睡眠面板显示“将在播放这首歌后停止：<歌曲标题>”。

### 3.4 预览即听

实现旁路预览：不改变 Queue 内容、实际播放 Queue 和 Queue current song；预览结束恢复原播放上下文。

### 3.5 播放统计持续验证

完善服务器本地播放计数、周期统计和 Last Played，与后续 Musicolet Import 的 delta merge 持续做多版本回归测试。

---

## 4. R3：歌曲缓存与媒体传输完整化

### 4.1 服务器临时缓存

按需从 Agent 拉取歌曲并缓存。缓存不是事实数据，不进入系统 Backup，不建立永久 `server_storage_path`。

支持：

- 单首清除；
- 批量清除；
- 一键清空全部服务器歌曲缓存；
- 缓存占用统计；
- 文件校验/失败状态；
- Range 复用与并发控制。

第一阶段仍以手工清理为主；若以后增加自动淘汰，必须作为可配置缓存策略，不能影响事实数据。

### 4.2 浏览器缓存

根据浏览器能力选择 IndexedDB/Cache Storage 等实现，支持：

- 单首清除；
- 批量清除；
- 一键清空；
- 容量统计；
- 服务端缓存/手机在线状态提示。

### 4.3 媒体取用优先级

保持：浏览器缓存 → 服务器缓存 → 在线 Agent。三者都不可用时明确不可播放。

### 4.4 Agent 稳定性

强化：

- 长连接认证；
- TLS；
- 心跳；
- 断线重连；
- 网络切换恢复；
- 服务端 request cancellation；
- 多并发读取保护；
- 手机休眠/Termux 场景下的行为说明；
- 日志与诊断。

仍然不增加任何手机文件写权限。

---

## 5. R4：PCM SHA-256 音轨参照库

### 5.1 扫描范围

由 Musicolet `All songs` 中的歌曲路径形成扫描清单，由 Termux Agent 只读访问并计算 PCM SHA-256。

### 5.2 数据定位

扫描结果关联到“具体路径数据主体”，不是业务 song ID。允许同一路径在不同 scanner/FFmpeg 版本下记录多个 PCM 结果。

建议记录：

```text
path
scanner_name
scanner_version
ffmpeg_version
pcm_format_rule
pcm_sha256
scan_time
status
```

### 5.3 参照用途

提供查询：某 PCM 特征历史上对应过哪些路径/Title/Artist/Album 等歌曲信息。

PCM 相同的多个不同路径仍然是不同歌曲，不合并 Queue/Playlist/播放数据。

### 5.4 明确禁止

- 不作为 `file_id`；
- 不参与 Import Merge；
- 不自动识别路径 MOVE；
- 不使用 Chromaprint；
- 不因 PCM 相同而合并歌曲。

---

## 6. R5：分享与公开页面

### 6.1 公开范围

只有：

- 分享页面；
- 正在播放公开页；

允许匿名访问。其余默认管理员权限。

### 6.2 单曲分享

分享菜单提供：

1. 歌曲文件链接；
2. 该单曲播放界面。

文件链接页显示基本文件信息、Metadata、专业音频信息，并可下载。如果当前无缓存，需要按需从手机拉取；手机不可达且无缓存时显示不可提供文件。

### 6.3 列表对象分享

Queue、Playlist、Album、Artist、Folder 生成“当前特定歌曲列表”的公开分享内容页链接。

分享创建时保存 Share Record，保证未来即使当前工作数据继续变化，也能清楚定义该分享是静态快照还是动态引用；建议默认保存分享时歌曲列表快照，避免旧链接内容无声漂移。

### 6.4 分享管理

管理员查看、复制、禁用、删除分享记录；支持过期策略可作为后续扩展。

---

## 7. R6：歌曲工具与导出

### 7.1 M3U

完成 Queue/Playlist 等列表的 M3U 导出，路径表现需考虑 Web 与手机原路径语义，不能伪造服务器永久文件路径。

### 7.2 音频裁剪器

从手机或缓存取得音频，临时裁剪并生成独立输出：用于下载/分享。

必须保证：

- 不替换原歌曲；
- 不修改手机文件；
- 不自动加入曲库；
- 临时产物有清理策略。

### 7.3 Metadata 编辑器完善

服务器 Metadata 编辑继续只影响 working state 并形成 M，不写手机文件。完善字段校验、歌词编辑、批量编辑规则（仅在明确安全的字段/操作上开放）。

---

## 8. R7：标签解析偏好与资料索引

### 8.1 多值解析

默认分隔符包含：`,`、`;`、`|`、`&`、`ft.`、`feat.`。

分别允许配置：

- Artist；
- Album Artist；
- Composer；
- Genre。

解析只生成索引/视图，不修改原 Metadata。

### 8.2 重建索引

修改标签解析规则后，提供可控的后台重建 derived index 能力，并显示进度/结果，不应修改 Musicolet Snapshot 或歌曲 raw metadata。

### 8.3 搜索增强

结合多值索引完善 Artist/Album Artist/Composer/Genre 搜索与分组统计。

---

## 9. R8：设置、主题与界面偏好

完善汉堡菜单：

- 睡眠倒计时；
- 语言；
- 主题；
- Tab-bar 位置；
- 标签解析偏好；
- 本系统 Backup/Restore；
- 从 Musicolet Backup 导入；
- 缓存管理；
- 分享记录；
- 版本/诊断信息。

主题和 Tab-bar 位置属于服务器/用户 UI 设置，不应影响 Musicolet source 数据。

---

## 10. R9：本系统 Backup / Restore

### 10.1 Backup 内容

必须覆盖：

- 业务 SQLite 数据；
- Git repository 和完整 commit history；
- 全部正式 Musicolet Snapshots；
- 全部原始 Musicolet Backup ZIP；
- Import Procedures（包括未完成/已取消）；
- Diff / Conflict / Resolution / Manual Patch；
- Server M / Change marks；
- PCM SHA-256 Reference DB；
- 系统设置、标签解析偏好、UI 偏好；
- Share Records。

不包含服务器/浏览器歌曲缓存。

### 10.2 Restore

Restore 必须先在临时目录/数据库做完整性检查，确认 manifest、SQLite、Git refs/objects、artifact 文件齐全后再切换正式数据。

不得在发现恢复包不完整后把当前系统覆盖成半恢复状态。

### 10.3 可重建性演练

定期增加自动化/人工演练：在只拥有 Backup 的情况下恢复整个系统，并验证历史 Version、current state、Server Change、Procedure、Git log 都一致。

---

## 11. R10：Import Procedure 高级体验与审计

### 11.1 原始结构字符 Diff

完善 canonical text：

- SQLite schema/table/row 固定顺序导出；
- JSON stable key order；
- `.mpl`；
- Queue/Favorite；
- Java Serialization 等结构化转储。

UI 同时提供 Semantic Diff 与 raw/canonical textual diff。

### 11.2 Conflict UI

为 Song Core、Playlist、Queue 等提供不同的业务冲突编辑器，不能所有冲突都用一个 JSON textarea。

### 11.3 Resolution 审计

每次 resolution 显示 BASE / 当时 OURS / THEIRS / RESOLVED；手动 patch 可读；stale 前后差异清楚。

### 11.4 Git 查看器

提供管理端 Git/Change History 视图：按时间、对象、Musicolet Version、Procedure 查看历史；能从对象快速定位对应 commit 和 M。

---

## 12. R11：性能、稳定性与数据库优化

随着全量 Version 增长，重点优化：

- Snapshot 查询索引；
- current working read model；
- Playlist/Queue ordered operations；
- 搜索索引；
- Semantic Diff 批处理；
- canonical dump 增量生成/缓存；
- Procedure 重新分析；
- SQLite WAL/checkpoint/backup；
- 大量历史 Version 的存储占用。

优化不得通过删除原始 ZIP、历史 Snapshot、Server Changes 来换空间。

对前端数千/上万歌曲长列表启用虚拟化，避免一次渲染全部 DOM。

---

## 13. R12：安全强化

后期完善：

- 登录速率限制；
- TOTP 恢复/重置流程（保持管理员可控）；
- Session 轮换与失效；
- CSRF/CSP/安全响应头；
- 公共分享 token 强随机性；
- 分享下载限速/并发限制；
- Agent 双向身份确认；
- TLS 配置检查；
- 导入 ZIP 大小、解压炸弹、路径穿越防护；
- 音频/Metadata 内容安全转义；
- 数据目录权限建议。

管理员账号/密码仍由服务启动环境变量提供，不写入源码。

---

## 14. R13：部署、维护与诊断

补齐：

- Linux 服务部署说明；
- systemd 示例；
- 反向代理示例；
- SQLite 数据目录权限；
- libgit2/git2go 运行依赖；
- Termux Agent 安装/更新/构建脚本；
- Agent 诊断命令；
- 数据库 integrity check；
- Git fsck/一致性检查；
- Import artifacts 完整性检查；
- 系统版本与 parser/scanner 版本查看。

---

## 15. R14：Musicolet Backup 兼容性维护

Musicolet 未来可能修改 Backup 格式，因此 parser 必须持续版本化。

后期建立：

- Backup format fingerprint；
- parser_version 与适配矩阵；
- 未知文件/字段报告；
- 新 Musicolet 版本导入前兼容性预检查；
- 旧原始 ZIP 重跑新 parser 的工具；
- parser regression fixture。

任何 parser 升级都不能修改原始 ZIP，也不能无审计覆盖旧正式 Snapshot；若需要重解析，应建立明确的重建/修正流程。

---

## 16. R15：Musicolet 功能对照清单与逐项补齐

以 `doc/requestment.md` 和 `doc/prompt/layout-prompt.md` 逐项维护 Feature Matrix：

```text
功能
需求来源
后端状态
前端状态
移动端状态
自动化测试
实机对照
备注
```

每完成一批功能，用真实 Musicolet 对照：控件位置、菜单入口、数据含义、Queue 行为、播放行为。

对于早期语音讨论中与最终需求不一致的内容，只记录为废弃背景，绝不重新引入。

---

## 17. 后期建议实施顺序

建议按用户可感知价值和基础依赖分四波：

```text
Wave 1
UI/交互补齐
播放引擎完整化
缓存管理

Wave 2
分享
M3U/裁剪器
标签解析/搜索增强
PCM 参照库

Wave 3
系统 Backup/Restore
Procedure 高级审计/Git 查看器
安全强化

Wave 4
性能/存储优化
部署维护
Musicolet 新版本兼容
长期 Feature Matrix 补齐
```

Wave 之间不是版本号承诺，可按实际使用中的痛点穿插，但不能牺牲初期已经锁定的数据一致性原则。

---

## 18. 后期完成判定

后期主开发完成应达到：

1. `layout-prompt.md` 中各 Tab 主要布局和交互基本完整；
2. `requestment.md` 中核心功能均有明确实现状态，无“按钮存在但语义未定义”的占位功能；
3. Web 播放、Queue、Playlist、Favorite、搜索、睡眠、分享、缓存形成完整用户体验；
4. PCM 参照库可用但从不污染业务身份；
5. 本系统 Backup/Restore 经完整恢复演练；
6. Import Procedure 多版本长期使用稳定，旧 M 不被无变化的手机数据覆盖；
7. Agent 持续保持严格只读权限边界；
8. 公开页面与管理员页面权限边界经过安全测试；
9. 大曲库下性能可接受；
10. 新 Musicolet Backup 格式变化时有明确兼容/报警机制。

完成这些后，项目从“镜像与复刻”进入长期维护阶段，后续工作主要是 Musicolet 新版本行为跟进、体验精修、兼容性和个人曲库数据维护，而不再是核心架构开发。
