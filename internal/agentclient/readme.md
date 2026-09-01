问：

	手机侧的musicolet agent是具体如何工作的，从网络、文件等技术层面详细解释下
	
	


当前的 Musicolet Agent 本质上是一个“手机侧只读文件网关”：它不控制 Musicolet、不播放音乐、不修改文件，也不监听公网端口。它主动连接服务器，在服务器需要某首歌时，按路径读取手机文件并分块传给服务器缓存；最终解码和播放发生在浏览器。

## 一、整体链路

```text
浏览器 <audio>
    │
    │ GET /api/media?path=<Musicolet路径>
    ▼
MusicoletWeb 服务端
    │
    ├─ 缓存命中 ────────────────► 直接用 HTTP Range 返回
    │
    └─ 缓存未命中
          │
          │ WebSocket read 请求，每次最多 1 MiB
          ▼
    手机上的 Termux Agent
          │
          ├─ 把 content:// 路径映射为 Android 文件路径
          ├─ 检查目标是否位于允许的只读目录
          ├─ os.Open + ReadAt
          └─ 返回文件分块
          │
          ▼
    服务端写入临时文件
          │
          ├─ 完整下载成功
          ├─ fsync
          └─ 原子重命名为缓存文件
          │
          ▼
浏览器开始读取缓存并播放
```

关键点是：当前不是浏览器直接从手机流式播放，而是“手机 → 服务端完整缓存 → 浏览器播放”。

## 二、Agent 如何启动

Agent 是脚本交叉编译出的 Linux/arm64 单文件程序：

```text
data/bin/musicolet-agent-linux-arm64
```

入口在 [cmd/musicolet-agent/main.go](/E:/Workspaces/github/Ltre/MusicoletWeb/cmd/musicolet-agent/main.go:16)。

手机侧一般需要：

```bash
termux-setup-storage
chmod +x musicolet-agent-linux-arm64

export MUSICOLET_SERVER_URL='https://musicolet.example.com'
export MUSICOLET_AGENT_TOKEN='与服务端完全相同的随机Token'
export MUSICOLET_AGENT_ROOTS='/storage/emulated/0/Music,/storage/emulated/0/存档'

./musicolet-agent-linux-arm64
```

环境变量含义：

- `MUSICOLET_SERVER_URL`：服务器根地址。Agent 自动在后面加 `/agent/connect`。
- `MUSICOLET_AGENT_TOKEN`：手机与服务端共享的长期认证 Token。
- `MUSICOLET_AGENT_ROOTS`：允许读取的 Android 目录，可用逗号分隔。
- `MUSICOLET_AGENT_ALLOW_HTTP=1`：仅允许本地调试使用明文 HTTP/WS。

如果没有配置 roots，默认只有：

```text
/storage/emulated/0/Music
```

你真实 Backup 中不少文件位于“存档”等其他目录，所以必须把这些目录加入 roots。

Agent 只在启动时读取配置。环境变量变化后需要重启 Agent。

## 三、网络连接方式

具体连接代码在 [internal/agentclient/client.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agentclient/client.go:55)。

### 1. 手机主动连接服务器

Agent 不在手机上开启 HTTP 端口，也不要求手机有公网 IP。连接方向始终是：

```text
手机 → 服务器
```

所以可以穿过家庭 NAT、运营商移动网络和大多数防火墙。

URL 转换规则：

```text
https://example.com → wss://example.com/agent/connect
http://example.com  → ws://example.com/agent/connect
```

明文 HTTP 默认拒绝。只有显式设置：

```bash
MUSICOLET_AGENT_ALLOW_HTTP=1
```

才允许 `ws://`。

生产环境应使用手机系统信任的 HTTPS 证书。反向代理必须正确转发 WebSocket Upgrade，并允许连接长时间保持。

### 2. Bearer Token 认证

Agent 在 WebSocket 握手里发送：

```http
Authorization: Bearer <MUSICOLET_AGENT_TOKEN>
```

服务端在 [internal/agenthub/hub.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agenthub/hub.go:46) 中使用定长比较验证 Token。

这个认证与浏览器管理员 Session 无关：

- 浏览器使用密码、TOTP、Session、CSRF；
- Agent 使用独立的 Bearer Token。

Token 为空时，服务端不会接受 Agent，即使 Windows 正在使用开发认证模式。

### 3. 单 Agent 模型

服务端当前只保留一个 active Agent。

如果第二个 Agent 使用同一 Token 连入，服务端会：

1. 把新连接设为 active；
2. 主动关闭旧连接；
3. 旧连接上的未完成读取全部失败。

所以当前不支持多手机同时在线、按设备选择歌曲来源。

### 4. hello 和在线状态

WebSocket 建立后，Agent 首先发送：

```json
{
  "type": "hello",
  "version": "musicolet-agent-v1"
}
```

服务端保存：

- 是否在线；
- 连接时间；
- Agent 版本。

管理 API 可以读取 Agent 在线状态。

### 5. 保活与重连

Agent 每 4 分钟发送一次 WebSocket Ping，单次等待时间最多 20 秒，代码在 [client.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agentclient/client.go:84)。

连接断开后采用指数退避：

```text
1s → 2s → 4s → 8s → 16s → 32s → 60s
```

之后最多每 60 秒重试一次。

当前有两个细节：

- 退避时间在重新连上后不会重置为 1 秒；多次断线后会逐渐达到 60 秒。
- Ping 错误目前被忽略，没有单独记录 last-pong；通常会由后续 WebSocket 读写发现断线。

如果 Android 把 Termux 进程彻底杀死，重连逻辑也就不存在了。因此长期运行通常还需要 Termux wakelock、Termux:Boot 或进程管理工具；这些目前不属于 Agent 自身实现。

## 四、WebSocket 应用协议

协议结构在 [internal/agentproto/protocol.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agentproto/protocol.go:3)。

服务端请求示例：

```json
{
  "type": "read",
  "id": "随机请求ID",
  "path": "content://com.android.externalstorage.documents/...",
  "offset": 1048576,
  "length": 1048576
}
```

Agent 响应：

```json
{
  "type": "read_result",
  "id": "相同请求ID",
  "size": 10485760,
  "data": "<Base64编码的字节>",
  "eof": false
}
```

错误响应：

```json
{
  "type": "read_result",
  "id": "相同请求ID",
  "error": "path is outside configured read-only roots"
}
```

### 请求关联

服务端为每次读取生成 12 字节随机 ID，编码成 24 位十六进制字符串。

服务端维护：

```text
request ID → 等待响应的 channel
```

响应回来后按 ID 唤醒对应请求。WebSocket 断开时，所有 pending channel 都会关闭，调用方收到 `agent disconnected`。

服务端支持多个并发 pending 请求，但 Agent 的主读取循环是串行处理的，因此手机文件读取最终仍基本按请求顺序执行。

### 分块大小

单次读取最大 1 MiB：

```text
1 << 20 = 1,048,576 bytes
```

手机和服务端两侧都会重新限制长度，不能由恶意请求要求一次读取任意大内存。

WebSocket message read limit 是 2 MiB。因为协议使用 JSON，Go 会把 `[]byte` 自动编码成 Base64，所以 1 MiB 原始文件块在网络上大约会膨胀到 1.33 MiB，再加少量 JSON 字段，仍位于 2 MiB 限制内。

WebSocket 压缩当前关闭，因此不会进一步消耗手机 CPU 做消息压缩。

## 五、手机文件路径处理

文件解析在 [client.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agentclient/client.go:176)。

Agent 接受两类来源路径。

### 1. 普通绝对路径

例如：

```text
/storage/emulated/0/Music/A.mp3
```

它会：

1. 转为绝对路径；
2. 解析符号链接；
3. 检查最终实际路径是否位于允许的 roots 内。

### 2. Android `content://` 路径

Musicolet Backup 中记录的通常不是普通路径，而是：

```text
content://com.android.externalstorage.documents/...
```

当前代码会：

1. 使用 URL parser 解析；
2. 对 `%3A`、`%2F` 等进行解码；
3. 查找 `primary:`；
4. 映射为：

```text
/storage/emulated/0/<primary: 后面的内容>
```

例如理想输入：

```text
content://.../document/primary%3AMusic%2FA.mp3
```

会映射为：

```text
/storage/emulated/0/Music/A.mp3
```

当前只支持 Android `primary` volume。SD 卡 UUID 之类的路径会返回：

```text
unsupported Android content URI volume
```

## 六、这里有一个当前实现中的实际路径缺陷

你这份真实 Backup 的 URI 大量采用 SAF tree/document 组合形式：

```text
.../tree/primary%3A某目录/document/primary%3A某目录%2F歌曲.mp3
```

解码后里面会出现两个 `primary:`：

```text
/tree/primary:某目录/document/primary:某目录/歌曲.mp3
```

但当前实现使用的是第一次出现的位置：

```go
strings.Index(decoded, "primary:")
```

因此可能错误映射为：

```text
/storage/emulated/0/某目录/document/primary:某目录/歌曲.mp3
```

正确做法应优先取 `/document/` 后面的 volume/path，或者至少使用最后一个 `primary:`。

这意味着：当前 Agent 的网络协议和文件读取链路已经存在，但面对你真实 Backup 的 tree/document URI，实际播放很可能因为映射到不存在的路径而失败，最终浏览器收到 503。

这是当前实现需要修复的真实缺陷，不只是理论限制。

## 七、文件安全边界

Agent 的文件读取函数在 [client.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/agentclient/client.go:115)。

每个请求都会执行：

1. 路径转换；
2. `filepath.Abs`；
3. `filepath.EvalSymlinks`；
4. 与允许 roots 做 `filepath.Rel`；
5. 拒绝 `..` 越界；
6. `os.Open`；
7. `Stat`；
8. 确认是普通文件；
9. 检查 offset；
10. 使用 `ReadAt` 读取。

符号链接检查很重要。例如：

```text
/storage/emulated/0/Music/link.mp3
    → /data/data/其他目录/secret
```

即使链接本身位于 Music，解析后的目标不在 root 内，也会被拒绝。

当前 Agent 协议没有以下命令：

- create
- write
- truncate
- remove
- rename
- move
- chmod
- mkdir

代码只使用 `os.Open`，没有 `os.OpenFile` 写模式。因此服务端即使被入侵，也不能通过现有 Agent 协议直接删除或修改手机文件。

不过服务端可以请求 roots 内的任何普通文件，不仅限于 Backup 中已经出现的歌曲。真正的安全边界是 `MUSICOLET_AGENT_ROOTS`，所以不要把它配置成：

```text
/storage/emulated/0
```

除非确实愿意让服务端读取整个共享存储。

## 八、实际读文件方式

每个 1 MiB 分块都会：

```text
os.Open
→ Stat
→ ReadAt(offset)
→ Close
```

也就是说，一个 20 MiB 文件大约需要 20 次请求，并可能重复打开文件约 20 次。

Agent 不长期持有文件句柄，也没有文件 ID/session。这让断线恢复简单，但增加了一点手机文件系统调用开销。

响应包含读取时的文件总大小和 EOF：

```go
EOF = offset + 实际读取长度 >= 文件大小
```

如果请求 offset 为负数或超过文件大小，会直接拒绝。

## 九、服务端如何获取和缓存歌曲

缓存代码在 [internal/media/cache.go](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/media/cache.go:35)。

浏览器请求：

```http
GET /api/media?path=<歌曲路径>
```

该接口属于管理 API，需要有效管理员 Session。

服务端先用“来源路径的 SHA-256”生成缓存文件名：

```text
SHA256(path).media
```

例如两个不同路径，即使内容完全相同，也会生成两个缓存文件。这符合系统的“路径是歌曲业务身份”原则。

### 缓存命中

如果缓存文件存在：

- 不联系手机；
- Agent 可以离线；
- 服务端直接打开缓存；
- 使用 `http.ServeContent` 响应浏览器；
- 支持 `Range`、seek、Content-Length、If-Modified-Since 等标准行为。

### 缓存未命中

服务端：

1. 为该歌曲取得 per-path mutex，避免多个请求重复下载；
2. 创建 `download-*.partial`；
3. 从 offset=0 开始，每次向 Agent 请求 1 MiB；
4. 顺序写入临时文件；
5. 直到 Agent 返回 EOF；
6. `Sync`；
7. 关闭文件；
8. 原子 rename 为正式缓存文件。

如果中途失败：

- `.partial` 文件会删除；
- 不会留下一个被误认为完整歌曲的缓存；
- 浏览器收到 HTTP 503：

```text
当前歌曲未缓存，且无法从手机在线取得歌曲文件。
```

一次下载的服务端超时是 20 分钟。

## 十、当前并不是真正的边下边播

这是当前媒体实现最重要的技术特征。

虽然手机与服务器之间按 1 MiB 分块读取，但服务端的 `Cache.Ensure` 必须完整下载并原子重命名后，才会进入：

```go
http.ServeContent(...)
```

所以第一次播放一首未缓存歌曲时：

- 浏览器不会在第一个 1 MiB 到达后立即播放；
- 必须等待整首歌从手机传到服务器；
- 浏览器发出的 Range 请求不会直接转发给手机；
- 首次 seek 同样要等待全文件缓存完成。

缓存完成后，后续播放和 seek 才是真正高效的本地 HTTP Range。

如果希望真正边下边播，后续需要实现：

- Range → Agent offset/length 透传；
- sparse/range cache；
- 已下载区间索引；
- 多 Range 合并；
- 手机断线时对已缓存区间降级；
- 避免两个浏览器 Range 请求重复拉取相同区间。

## 十一、浏览器和播放状态的关系

浏览器在 [app.js](/E:/Workspaces/github/Ltre/MusicoletWeb/internal/webui/static/app.js:60) 中设置：

```text
audio.src = /api/media?path=...
```

音频解码和实际声音输出都发生在浏览器。

Agent 完全不知道：

- 当前 Queue；
- 是否正在播放；
- 播放速度；
- shuffle/repeat；
- progress；
- 播放次数；
- stop target。

这些由浏览器和服务端 SQLite 管理。浏览器播放期间大约每 15 秒保存一次 progress；歌曲自然播放结束后，浏览器调用播放完成 API，服务端再增加播放统计。

因此 Agent 只是“按路径给字节”，不是播放器。

## 十二、缓存一致性限制

缓存键目前只取决于路径，不包含：

- 文件 mtime；
- 文件 size；
- 文件内容 hash；
- Musicolet Version；
- Agent device ID。

如果手机上同一路径的文件内容被替换，服务端仍可能继续播放旧缓存，直到手动清除此歌曲或全部缓存。

这是有意避免每次播放都唤醒手机做 stat，但代价是同路径内容替换后不会自动失效。

## 十三、离线与故障行为

### Agent 离线、缓存命中

正常播放，不需要手机。

### Agent 离线、缓存未命中

返回 503，无法播放。

### 下载中途手机断线

当前请求失败，partial 删除。Agent 重连后，下次播放会从 offset 0 重新下载，不支持断点续传。

### 手机文件不存在

Agent 的 `os.Open` 返回错误，服务端最终向浏览器返回统一 503。

### 文件不在 roots

Agent 拒绝，不读取。

### 新 Agent 接管连接

旧 Agent 被断开，旧连接上的读取失败；后续请求走新 Agent。

### Termux 被 Android 杀死

不会自动恢复，除非 Termux/系统重新启动进程。Agent 内部重连只在进程仍存活时有效。

## 十四、当前实现的安全模型总结

已实现的保护：

- 手机只建立出站连接；
- 生产默认要求 WSS；
- 独立 Agent Bearer Token；
- Token 定长比较；
- 单次分块和 WebSocket message 大小限制；
- 只读协议；
- root containment；
- symlink escape 检查；
- 普通文件检查；
- partial + 原子缓存；
- 浏览器媒体接口仍需管理员 Session。

尚未实现：

- 多设备身份和设备选择；
- Token 轮换/撤销列表；
- mTLS；
- 每首歌曲一次性授权；
- Agent 审计日志上传；
- SD 卡 volume UUID 映射；
- 真正的 Range 透传/边下边播；
- 缓存自动失效；
- Android 前台 Service；
- tree/document URI 的正确解析。

最需要优先修复的是 tree/document URI 映射，因为它会直接阻断你这份真实 Musicolet 数据对应的手机文件读取。


