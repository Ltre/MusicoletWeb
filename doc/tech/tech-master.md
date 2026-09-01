# MusicoletWeb 技术总览

本文描述 `dev-2609A-step1` 初期基线的实际实现。需求解释优先级遵循文档产生链：

```text
first.md + second.md + layout-prompt.md
                    ↓
              requestment.md
                    ↓
 Initial Development Plans.md + Remaining Development Plans.md
```

其中 `second.md` 的 grill-me 回答是业务边界的原始确认依据；roadmap 是执行拆分，不反向改写已经确认的业务语义。

## 1. 运行架构

```text
Browser SPA
  ├─ authenticated JSON API ── Go HTTP server ── SQLite/WAL
  ├─ Range media request ───── Media Cache ───── Agent Hub
  └─ public now-playing ────── read-only route       │
                                                    WSS
                                                     │
                                              Termux Go Agent
                                              read-only files

Go server ── Git history adapter ── bare Git repository
```

- 服务端入口：`cmd/musicoletweb`，默认 `0.0.0.0:4001`。
- Agent 入口：`cmd/musicolet-agent`，目标为 Linux/arm64 Termux。
- SQLite 驱动：`modernc.org/sqlite`，pure Go、WAL、foreign keys、busy timeout。
- Web：无构建步骤的原生 ES module 风格 SPA，由 `embed.FS` 嵌入服务端二进制。首版冻结为 vanilla HTML/CSS/JavaScript，原因是部署简单、无 Node runtime，并仍保留组件化函数、拖拽和 Web Audio 扩展点。
- 历史：规范化 `state.json` 写入 bare Git repository；SQLite DB 本身不进入 Git。

## 2. 数据身份与不变量

### 2.1 歌曲身份

Musicolet 路径是 Snapshot 内业务键。系统不建立 `recording_id / file_instance_id` 双层身份模型，也不根据 PCM hash、Metadata、相邻版本或文件名推断 MOVE。路径改变按“旧路径 DELETE + 新路径 ADD”处理。

`songs.file_id` 只是 working table 内部技术编号，不能跨版本代替路径业务身份。PCM SHA-256 仅属于后续参照库能力，不能自动合并两条路径歌曲。

### 2.2 Song Core

标题、Artist、Album、Album Artist、Composer、Genre、Lyrics、Comment、Track/Disc/Year 及文件技术属性共同构成 Song Core。三方合并时 Core 是完整冲突单元，不做字段级自动拼接。Favorite、播放统计和运行时播放状态独立处理。

### 2.3 三层事实

1. `musicolet_versions` 与 `*_snapshot`：每次正式导入的不可变来源事实。
2. `songs/playlists/queues/...`：服务器当前 working state，可被业务 API 修改。
3. `server_changes/change_targets`：正式 Server M，记录 before/after、对象、操作、基准版本、revision 和 Git commit。

历史 Snapshot 只在导入提交时 INSERT；所有编辑 API 只操作 working tables。

### 2.4 有序列表与播放状态

- Playlist/Queue 用显式 `position`，不依赖 SQLite 自然顺序。
- `(list, song_path)` 主键和 `(list, position)` 唯一约束阻止成员重复。
- Queue 内容与 `runtime_playback_state` 分离；每个 Queue 自身另存 current/progress/stop target。
- 浏览某个 Queue 不会自动改变实际播放 Queue。
- 顶层 Queue 顺序保留 OURS；来源新增 Queue 按 THEIRS 顺序追加。无变化导入不得按 source key 重排 Queue。

## 3. SQLite 与 migration

数据库位于 `${MUSICOLET_DATA_DIR}/musicolet.db`。启动时自动运行幂等 migration：

- v1：导入/版本、Snapshot、working state、Server M、Procedure/Conflict、Git 记录、搜索历史及运行时播放状态；
- v2：给 `musicolet_versions` 增加 `current_queue_index`，保存历史来源快照的当前 Queue。

关键事务策略：

- Server M 在同一 SQLite transaction 中修改 working state、写 Server Change、change targets、revision 和待发布 Git commit 信息。
- Import Commit 在 mutation mutex 下重新校验 server revision，统一写 Version/Snapshot/working state/Procedure/Git 记录。
- Git object 先 prepare，SQLite 成功后 CAS 更新 Git ref；若进程在二者之间失败，数据库中的 `git_commits` 是恢复依据，下一次启动会校正 ref。
- active Procedure 由 partial unique index 保证最多一个。

## 4. Backup 处理

每个 Procedure 使用固定目录：

```text
data/imports/<procedure-id>/
  original.zip
  decrypted/
  canonical/
  parser/files.json
  parser/validation.json
```

处理顺序：

1. multipart 流先写入 `original.zip`，同步计算 SHA-256；
2. 创建 Procedure 后后台解析，ZIP 不允许替换；
3. 校验 ZIP entry 名，拒绝绝对路径和 zip-slip；
4. 对 payload 尝试已验证的 Blowfish ECB 固定密钥 + PKCS#5/7 compatible unpadding；
5. 以 manifest 的明文 MD5 和整体 manifest hash 校验解密结果；
6. 保存逐文件 kind/encrypted/size/SHA-256/MD5/parseState；
7. 解析为稳定领域 DTO 和 Candidate Snapshot。

当前 parser 覆盖：

- `DB_SONGS_LOG` SQLite；
- `PCs_Y_* / PCs_M_* / PCs_W_*` 和总播放统计；
- `.mpl` Playlist；
- `0.qstk` Queue、当前 Queue、当前项与 progress；
- `0.favs`；
- JSON 设置和状态；
- 未知 JSON、SQLite 和 Java Serialization 均保留并在 parser 报告标记，不静默丢弃。

原始 ZIP 和已解密 artifact 是 Procedure 审计事实。取消 Procedure 不删除这些文件；但在 Procedure 尚未创建成功时发生 409/写入失败，会清理该次随机临时目录，避免遗留额外私有 ZIP。

## 5. Procedure 与 Merge

状态机：

```text
PARSING → REVIEWING → RESOLVING → READY_TO_COMMIT → COMMITTED
    └────────────── active states ───────────────┘
                    ├─ CANCELLED
                    └─ FAILED
```

取消是终态。Candidate 持久化、分析保存和失败回写都带状态条件，迟到的后台 parser 不能复活已取消 Procedure。

第二次导入使用：

```text
BASE   = 上一正式 Musicolet Version
OURS   = 当前服务器 working state
THEIRS = 本次固定 ZIP 的 Candidate Snapshot
```

通用规则：THEIRS 未变保留 OURS；OURS 未变采用 THEIRS；两边结果相同自动接受；两边不同进入业务冲突判断。

主要特例：

- Song Core 双改产生完整 Core 冲突；
- Favorite 独立合并；
- Server delete 可遮蔽手机仅播放统计增长；
- 新路径永远是新 ADD；
- Queue/Playlist 成员按 ADD/DELETE/MOVE 语义合并，最终 resolution 单位仍是完整列表；
- `SERVER DELETE B + PHONE MOVE B` 保持删除；同一成员双方 MOVE 到不同位置、或双方 ADD 到不同位置产生冲突；
- Settings 参与三方差异和冲突；
- Playback State 完全保留服务器即时值，不进入 Import Conflict。

播放次数每个 bucket 分别使用：

```text
resolve = current_server + (current_import - base_import)
```

这等价于确认文档里的：

```text
previous_resolve
+ (current_server - previous_resolve)
+ (current_import - base_import)
```

Last Played 取两端较晚时间。正式提交后，新 BASE 是本次来源 Candidate，新 `previous_resolve` 是本次综合结果。

Conflict 保存 BASE/OURS/THEIRS，resolution 支持 OURS、THEIRS、MANUAL；手动结果与 patch 都持久化。如果 Procedure 分析后产生新的 Server M，状态退回 REVIEWING；重新分析发现冲突 OURS 改变时，将旧决定标 stale 并强制重确认。Commit 使用 server revision 做 CAS。

## 6. Git 历史实现与边界

业务层只依赖窄 `History` 接口，业务 merge 从不调用 Git 文本 merge。当前适配器使用系统 Git 的 plumbing 命令：`hash-object`、`mktree`、`commit-tree`、`update-ref`，而不是 git2go/libgit2。

这是一次有意记录的 roadmap 变更：目标环境同时包含 Windows、无 CGO 的 Linux 服务端构建和 Termux arm64 交叉编译；git2go 需要 CGO、匹配版本的 libgit2 头文件/动态库，并破坏当前单二进制构建。初期选择可部署的 Git CLI adapter，把替换边界限制在 `internal/gitstore`。当前 repository 使用线性 `refs/heads/main`，没有把 source/current 拆为两条 Git ref，也没有使用 Git conflict index；SQLite 的 Version/Server M/Conflict 表才是业务审计真相。若后续引入 libgit2，应先补齐目标主机的独立构建 spike，再替换 adapter，不改变 merge 规则。

## 7. Server M 与 Queue 行为

已接通的 Server M：

- Song Metadata、Favorite、永久删除；
- Playlist 创建、成员替换/移动、删除；
- Queue 创建、改名、删除、上下重排、成员替换、插队、移到队尾、反向和永久随机化；
- 播放完成后的总次数、周期 bucket 和 Last Played 更新。

来源与 Queue 的关联存入 `source_queue_links`，键为 `(source_type, source_key)`，不能按名称猜测。名称冲突自动生成 `name #N`。关联在导入替换 working tables 时按 Queue source key 重绑。对已关联来源执行乱序播放只复用现有 Queue，不再次随机化。

## 8. Web、API 与认证

非公开 `/api/*` 默认要求登录。写请求还要求 Session 内绑定的 CSRF token。Cookie 为 HttpOnly、SameSite=Strict；HTTPS 请求使用 Secure。认证配置来自环境变量，生产模式要求 TOTP、至少 12 字符密码、至少 32 字符 session key 和至少 24 字符 Agent token。

公开路由只有：

- `GET /healthz`；
- `GET /api/public/now-playing`；
- `/now-playing` SPA 只读视图；
- `GET /agent/connect` 仍需 Bearer Agent token。

主要管理 API：

```text
POST /api/auth/login          POST /api/auth/logout
GET  /api/library             GET/PATCH /api/playback
POST /api/playback/complete   GET /api/agent/status
GET  /api/media               DELETE /api/media/cache
GET/POST /api/imports         GET /api/imports/<id>
POST /api/imports/<id>/resolve|commit|cancel
PATCH/DELETE /api/songs/<hex-path>
PUT /api/songs/<hex-path>/favorite
POST/PUT/DELETE /api/playlists/...
POST/PATCH/DELETE /api/queues/...
PUT /api/queues/<id>/items|stop
```

SPA 使用顶部横向 Tab、固定 Mini Player、页面内底部搜索和 dialog 浮层。桌面和窄屏共用同一信息结构；长列表当前为原生 DOM 列表，已保留组件入口但尚未虚拟化。

## 9. Agent 与媒体链路

Agent 只建立长期出站 WebSocket，Bearer token 认证，发送 hello/version，并在断线后指数退避。协议只定义 `read` 与 `read_result`；每次最多读取 1 MiB，支持 offset/EOF/stat size。没有 create/write/remove/rename/move 消息或执行分支。

安全边界：

- 生产默认拒绝明文 HTTP/WS；
- `content://` 只支持 Android primary volume 映射；
- 目标必须位于配置的只读 roots；
- `EvalSymlinks` 后再次做 root containment，阻止符号链接逃逸；
- 普通文件以外的目标拒绝读取。

服务端按 1 MiB 分块从 Agent 拉取到 `.partial` 文件，sync + atomic rename 后进入按路径 SHA-256 命名的缓存。缓存命中时由 `http.ServeContent` 提供 Range/seek。Agent 离线且缓存 miss 时返回明确错误；缓存可按歌曲或整体手动清理，不进入系统 Backup。

## 10. 部署与数据保护

`data/` 默认全部忽略，仅保留 `.gitkeep`。以下内容不得提交：真实 Musicolet ZIP、解密文件、SQLite/WAL、Git runtime repository、媒体缓存、`config.env`、生成的二进制。

Windows 与 Linux 启动脚本位于 `scripts/`，都在启动前重建。反向代理/vhost 示例见 `doc/tech/deploy-vhost.md`。公网部署必须使用 TLS；Agent URL 应为同一 HTTPS origin 或受信任的 WSS endpoint。

## 11. 已知初期边界

- 只有一份 2026-08-30 真实 Backup 可用，因此真实环境完成了“同一真实 ZIP 的 V1/V2 幂等合并”和“Server M 跨导入保持”，尚不能证明两份不同真实 ZIP 的手机侧变化；不同 THEIRS 的规则由 synthetic/unit tests 覆盖。
- 本机没有连接真实 Termux 手机，Agent 的编译、协议、路径映射、安全边界和离线行为已验证，但真实设备在线播放仍需部署后验收。
- Git 当前是 CLI adapter + 单 main ref，未完成 libgit2、source/current 双 ref 和 Git conflict index；业务 Conflict/Version 审计由 SQLite 完整承担。
- 长列表未虚拟化；数千歌曲数据加载与浏览器视图已验证，进一步滚动性能优化属于后续工作。
- 分享中心、音频裁剪、Web Audio 均衡器、PCM 参照库和系统 Backup/Restore 属于后期 roadmap。
