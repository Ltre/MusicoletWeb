

以下是初步探讨系统基本形态和系统主数据处理主要方案。

围绕 Musicolet 备份数据的解密、结构解析与镜像系统设计，重点讨论歌曲身份、版本化全量导入、差异检测、服务器改动及 Git 式冲突合并流程。

注意，这个文档的一些决策是初步的，不要当作最终版。

由于篇幅过长，本文档尾部将由second.md继续讨论承接。

------------------------------------------


问：
	Musicolet是功能很全的android本地播放器，我在里面保存了很多播放列表、播放队列，而且还有歌曲的按时间多次播放的记录等数据。最近想备份这些数据出来并做出一个镜像站（复刻自己的播放器数据库）。
	它有个备份功能：Backup now (Backup Musicolet's Playlists, Favorites, Play-count and Settings.)功能，但导出的压缩包里的文件几乎是乱码，怎么解析出来
	
	
答：


能解析。你看到的“乱码”大概率不是字符编码问题，而是 **Musicolet 对备份中的数据库文件又做了一层 Blowfish 加密**。

我查到的公开逆向结果与现在 Musicolet 的功能描述能对应起来：Musicolet 的备份确实包含 Playlists、Favorites、Playback history / play-counts、Settings 等数据。官方隐私说明也明确说 backup ZIP 中包含播放历史（recently played、play-counts）。([krosbits.in][1])

更关键的是，已有用户逆向出了旧版至较新版一直使用的处理方法：

> Blowfish ECB
> key：`JSTMUSIC_2`

解密之后，像 `DB_SONGS_LOG`、`PCs_M_20xx` 这样的文件实际上可以变成正常的 **SQLite 数据库**。([Reddit][2])

### 为什么直接用 SQLite 软件打不开

因为它不是：

`SQLCipher 加密的 SQLite`

而更像：

`SQLite 文件 → 整个文件用 Blowfish 加密 → 放进 ZIP`

所以你拿 DB Browser for SQLite 直接打开，会认为文件损坏；拿 SQLCipher 输入 `JSTMUSIC_2` 也不行。

正确过程是：

`Musicolet backup.zip → 解压 → Blowfish 解密 → SQLite DB → 读取表`

---

### 你可以先在 Termux 试

你本身就用 Termux，这个最方便。

```bash
pip install blowfish
```

例如目录里有：

```text
DB_SONGS_LOG
PCs_M_2025
PCs_M_2026
PCs_Y_2025
...
```

建一个 `decrypt_musicolet.py`：

```python
import sys
import blowfish
from pathlib import Path

KEY = b"JSTMUSIC_2"

if len(sys.argv) < 2:
    print("usage: python decrypt_musicolet.py <file>")
    sys.exit(1)

src = Path(sys.argv[1])
dst = src.with_name(src.name + ".db")

data = src.read_bytes()

if len(data) % 8 != 0:
    print(f"Warning: file size {len(data)} is not a multiple of 8")

cipher = blowfish.Cipher(KEY)

plain = b"".join(cipher.decrypt_ecb(data))

dst.write_bytes(plain)

print("written:", dst)

if plain.startswith(b"SQLite format 3\x00"):
    print("OK: SQLite database detected")
else:
    print("Not a standard SQLite header.")
    print("First 32 bytes:", plain[:32])
```

比如：

```bash
python decrypt_musicolet.py DB_SONGS_LOG
```

成功的话会看到：

```text
written: DB_SONGS_LOG.db
OK: SQLite database detected
```

然后 Termux 直接：

```bash
pkg install sqlite
sqlite3 DB_SONGS_LOG.db
```

进去以后：

```sql
.tables
```

再：

```sql
.schema
```

或者：

```sql
SELECT name FROM sqlite_master
WHERE type='table';
```

就可以开始把 Musicolet 的内部数据结构挖出来了。

---

### `DB_SONGS_LOG` 很值得重点研究

公开分析已经确认现代版本 APK 里仍然存在：

```text
DB_SONGS_LOG
TABLE_SONG_LOGS
TABLE_SONGS
COL_NUM_PLAYED_M
COL_NUM_PLAYED_W
COL_NUM_PLAYED_Y
COL_LASTPOS
COL_LOGPATH
```

至少到 Musicolet 6.12 的 APK 中仍能看到这些数据库字段和表名。([hybrid-analysis.com][3])

所以你想做的“自己的 Musicolet 镜像站”实际上非常有希望。

我建议镜像站不要只保存一个：

```text
歌曲 -> 总播放次数
```

而是尽量恢复成：

```text
Track
 ├─ 文件路径
 ├─ Title
 ├─ Artist
 ├─ Album
 ├─ Duration
 ├─ Rating / Favorite
 ├─ 总播放次数
 ├─ 年播放次数
 ├─ 月播放次数
 ├─ 周播放次数
 ├─ 最近播放时间
 └─ Play events
      ├─ 2025-08-13 01:22:31
      ├─ 2025-08-16 23:51:08
      ├─ 2026-01-02 03:14:29
      └─ ...
```

社区里有人读取这些数据库时，也发现日期相关字段是毫秒级整数，并推测/确认属于 Unix Epoch milliseconds。([Reddit][2])

也就是遇到：

```text
1756053745123
```

这种数字时，可以先尝试：

```python
from datetime import datetime

ts = 1756053745123

print(
    datetime.fromtimestamp(ts / 1000)
)
```

---

### 还有一个对你很有利的新功能

Musicolet **6.13 已经官方增加了 Most Played 数据手工导出 CSV**：

> Manually export most played data to a CSV file

当前 Google Play 页面仍明确写着这个功能。([Google Play][4])

所以可以同时做两路数据：

**官方 CSV：**

```text
歌曲 ↔ 播放次数
```

作为校验基准。

**Backup 解密数据库：**

```text
歌曲
播放历史
时间
播放列表
Favorites
Queues
Settings
...
```

作为完整数据源。

两边对一下，非常适合验证我们的逆向有没有理解错字段。

---

### 不过你的目标比网上这些帖子复杂得多

网上的人大多只是想：

> “看看自己今年播放了多少次。”

你实际上要做的是：

> **把 Musicolet 变成一个可持续同步、可查询、可视化的个人音乐数据库镜像。**

这就需要继续搞清楚整个 backup ZIP 中**每一个文件到底负责什么**。

例如看到：

```text
DB_SONGS_LOG
PCs_M_2019
PCs_M_2020
PCs_M_2021
PCs_Y_2025
……
一堆纯数字文件
一些配置文件
```

不要只解析 `DB_SONGS_LOG`。

`PCs` 很可能就是不同时间粒度的 **Play Counts** 数据。早年的用户也明确观察到 `PCs_{M,Y}_20xx` 与按月/按年的播放统计高度对应。([Reddit][5])

而你真正关心的：

* 所有播放列表
* 播放列表歌曲顺序
* Favorites
* 当前及历史 Queue
* Queue 顺序
* 每首歌曲累计播放次数
* 年/月/周统计
* 最近播放
* **逐次播放时间记录**
* 歌曲内部 ID 与实际文件路径映射
* Musicolet 自己记录的元数据
* Rating
* Last position
* 设置

必须把整个 ZIP 做一次结构审计。

### 最省事的下一步

**直接把 Musicolet 导出的原始 Backup ZIP 发给我。**

不要提前转换，也不要把“乱码文件”另存为文本，直接给原始 ZIP。

我可以直接帮你做：

1. 列出 ZIP 完整目录结构；
2. 判断每个文件的 magic / 熵 / 大小；
3. 自动尝试 `JSTMUSIC_2 + Blowfish ECB`；
4. 判断哪些解密后是 SQLite；
5. 导出所有 SQLite schema；
6. 分析表之间的 ID 关联；
7. 找出播放列表、队列、Favorite、歌曲和 play event；
8. 把毫秒时间转换成人类时间验证；
9. 最后给你整理成一套适合“镜像站”的数据库 Schema 和导入程序。

而且**先只读解析，不改原备份**。因为社区已经发现，改完数据库重新加密再导回 Musicolet，还涉及备份内的完整性/hash 校验问题；但你现在只是建立镜像站，完全不用碰这个麻烦。([Reddit][2])

你这个需求最关键的突破口已经有了：**这些基本不是乱码，至少核心文件是可以解密成 SQLite 的。**

[1]: https://krosbits.in/musicolet/docs/privacy_policy/musicolet_policy.htm?utm_source=chatgpt.com "Musicolet | Privacy policy"
[2]: https://www.reddit.com/r/androidapps/comments/t9zwow?utm_source=chatgpt.com "Musicolet: reading backup"
[3]: https://hybrid-analysis.com/sample/d0f2dfccfc948268262feebdbe221c05827cf9ee209817fcc52b2c17b4e05016/67645df82b44712b0704cf27?utm_source=chatgpt.com "Free Automated Malware Analysis Service - powered by Falcon Sandbox - Viewing online file analysis results for 'in.krosbits.musicolet_6.12 build501.apk'"
[4]: https://play.google.com/store/apps/details?hl=en-SG&id=in.krosbits.musicolet&utm_source=chatgpt.com "Musicolet Music Player – Apps on Google Play"
[5]: https://www.reddit.com/r/androidapps/comments/z94gc7?utm_source=chatgpt.com "Musicolet - Accessing Playback Statistics"






问：

	为什么musicolet要加密呢？有必要吗？这是用户自己的数据
	
	

答：




从用户立场看，你这个质疑是成立的：**如果备份文件只是用 Musicolet 内置的固定密钥加密，而用户既没设置密码、也没拿到密钥，那么这种设计对“保护用户自己的数据”意义很有限，反而降低了可移植性。**

比较可能的动机有几种，但 Musicolet 官方如果没明确说明，我们不能断言是哪一种。

最常见的是 **防止用户或其他程序随手修改备份内容**。播放器的数据库里可能有歌曲 ID、播放次数、播放列表顺序、内部路径、设置版本号等。开发者可能担心用户用文本编辑器/SQLite 工具改了以后再导入，造成数据库异常，然后反过来认为是 Musicolet 的 bug。整文件加密后，相当于把备份定义成“只供 Musicolet 自己读写的内部格式”。

第二个原因可能是 **防止普通人直接窥视隐私数据**。播放历史、收藏、歌曲路径本身确实有一定隐私性。比如备份文件被其他 App 扫到、上传到网盘、误发给别人时，直接 SQLite 至少能被随手打开。但如果密钥是写死在 APK 里的，那么这只能防“随手看”，防不了真正想分析的人。严格说这更接近 **obfuscation（混淆）或弱保护**，而不是强安全措施。

第三个原因是 **让备份格式保持私有和可演进**。开发者可以随版本改变内部 schema，不需要承诺：

> `DB_SONGS_LOG.db` 永远是公开稳定格式。

只要 Musicolet 自己知道怎样从旧版本恢复即可。对开发者来说，这能降低兼容性负担。

还有一种比较现实但不一定是 Musicolet 主观目的的结果，就是 **生态锁定**。如果备份是标准 SQLite/JSON/CSV：

```text
Musicolet
   ↓
任意第三方工具
   ↓
迁移到其他播放器 / 自建网站
```

会非常容易。

而加密之后变成：

```text
Musicolet backup
   ↓
Musicolet restore
```

第三方想利用数据就必须逆向格式。无论开发者是不是故意为了锁定，客观效果确实如此。

### 有没有必要？

要分目标。

如果目标是防止用户误改：

**可以理解，但没必要使用不透明加密。**

更合理的方案其实是：

```text
backup.zip
├── playlists.json
├── play_history.sqlite
├── settings.json
└── manifest.json
        └── checksum / version
```

恢复的时候做 checksum、schema version、数据校验即可。

如果目标是真正保护隐私，那么应该让用户设置备份密码，例如：

```text
Argon2 / PBKDF2
      ↓
AES-256-GCM
```

这样只有用户自己知道密码。

而：

```text
固定密钥写在 Musicolet APK
        ↓
所有用户使用同一个密钥
```

本质上并不能真正保密。任何人逆向一次 APK，理论上就获得了所有这种备份的解密能力。

所以从数据主权角度，理想设计应该是：

**用户数据既可以安全备份，也应该有开放导出能力。**

比如：

* 播放列表 → M3U / JSON
* 收藏 → CSV / JSON
* 播放统计 → CSV
* 完整播放历史 → CSV / JSON
* 设置 → JSON
* 完整数据库 → SQLite
* 私密备份 → 用户密码加密

Musicolet 后来加入“Most Played 导出 CSV”，其实已经说明他们至少部分意识到了“统计数据应该能由用户拿出来”。

而你的情况尤其明显：你已经积累了很多年的**播放时间序列数据**。这些数据实际上比歌曲文件本身还难重新获得。歌曲可以重新下载，而：

> 2021 年某月你听了哪首歌 37 次
> 2023 年某天晚上播放了哪些歌
> 某个播放列表过去是什么顺序
> 一首歌什么时候第一次/最后一次频繁播放

这些都是不可再生的个人数据。

所以我会建议你这次解析成功以后，不要继续把 Musicolet backup 当唯一档案。最好以后变成：

```text
Musicolet
     ↓ 定期备份
原始 Musicolet Backup
     ↓ 自动解密/解析
个人音乐数据库
     ↓
SQLite / PostgreSQL
     ↓
CSV / JSON 年度归档
     ↓
你的镜像站
```

这样以后哪怕 Musicolet 停更、换数据库格式，甚至 Android 上彻底不能用了，你过去十几年的音乐行为数据依然属于你自己。






问：


	你提到可以导出csv不过只能一个一个播放列表导出，我有近50个，这就很麻烦，而且不是这次50个导出以后就完事。因为播放列表会经常更新
	
	
	
	
答：




对，所以**逐个播放列表导出 CSV/M3U 不适合作为你的长期方案**。50 个列表只是第一次麻烦，更大的问题是之后每次播放列表增删、调整顺序，都要重新人工导出，根本无法形成镜像同步。

而且我前面提到 CSV 时应该区分两件事：Musicolet 的“播放统计 CSV”可以拿来做数据校验，但**不能代替完整备份解析**；至于播放列表逐个导出，更只能作为应急迁移手段。

你这个需求应该直接围绕 **Musicolet Backup → 自动解析 → 镜像数据库** 来做。

理想流程是：

```text
Musicolet 日常使用
        ↓
Backup now
        ↓
musicolet_backup.zip
        ↓
自动解密 / 解包
        ↓
解析歌曲、播放列表、队列、收藏、播放历史
        ↓
与你自己的数据库同步
        ↓
Web 镜像站
```

这样无论你有 5 个还是 500 个播放列表，处理成本基本一样。

## 关键是不要每次“全量覆盖”

我们可以让你自己的数据库保存 Musicolet 内部对象的身份。例如：

```text
tracks
playlists
playlist_tracks
play_events
play_counts
favorites
queues
```

其中播放列表：

```text
playlists
---------
musicolet_id
name
created_at
updated_at
```

列表中的歌曲关系：

```text
playlist_tracks
---------------
playlist_id
track_id
position
```

那么下一次导入 Musicolet 备份时：

旧：

```text
我的收藏
1 A
2 B
3 C
4 D
```

Musicolet 中后来改成：

```text
我的收藏
1 A
2 C
3 E
4 B
```

同步程序自动算出：

```text
删除 D
新增 E
B: position 2 → 4
C: position 3 → 2
```

你不需要做任何人工操作。

### 播放记录尤其应该采用增量同步

例如第一次解析得到：

```text
song_id=123

2026-08-20 10:31
2026-08-21 01:14
2026-08-22 22:08
```

下次：

```text
2026-08-20 10:31
2026-08-21 01:14
2026-08-22 22:08
2026-08-24 03:27
2026-08-25 17:51
```

那么只插：

```text
2026-08-24 03:27
2026-08-25 17:51
```

而不是把所有历史重新建立。

当然，这要看 Musicolet 究竟有没有保存**逐次 play event**。如果实际上只保存：

```text
2026-08:
song A = 37
song B = 12
```

那镜像数据库就按其真实粒度保存，不能凭播放次数伪造具体时间。

---

## 甚至 Backup now 这一步以后也可以自动化

我会分成三档。

**第一阶段：人工点一次 Backup now，其余全部自动。**

你只需要偶尔在 Musicolet：

```text
Backup now
```

然后脚本检测新 ZIP：

```text
/storage/emulated/0/...
```

自动：

```text
发现新备份
→ 解密
→ SQLite/配置解析
→ 导入
→ 增量更新镜像站
→ 保存原始备份
```

这已经比导出 50 个播放列表轻松太多。

**第二阶段：连“Backup now”也自动。**

如果 Musicolet 没提供 Intent/API，可以通过 Android 的 UI Automation，例如：

```text
Tasker
ADB / uiautomator
Accessibility automation
```

定期打开 Musicolet → 进入备份 → Backup now。

是否值得这么做要看 Musicolet 的备份 UI 实际交互。

**第三阶段：直接读取 Musicolet 内部数据库。**

如果设备有 root，那么还有更干净的一条路线：

```text
/data/data/in.krosbits.musicolet/
```

或者：

```text
/data/user/0/in.krosbits.musicolet/
```

直接研究 App 私有目录中的 SQLite/database 文件。

这样可以做到：

```text
Musicolet 数据库
       ↓
定期 snapshot
       ↓
你的镜像数据库
```

甚至根本不需要 Backup now。

不过**没有 root 时，Backup ZIP 是最稳妥的数据入口**。

---

## 我更建议保留“快照”，不要只保存当前状态

因为你既然要做个人播放器镜像，完全可以做到比 Musicolet 自己更强。

例如今天：

```text
Playlist: 夜间
A B C D
```

一个月后：

```text
Playlist: 夜间
A C E F
```

普通镜像只能告诉你：

> 现在是 A C E F。

而你的系统可以额外记录：

```text
playlist_revision

2026-08-25
A B C D

2026-09-11
A B C D E

2026-09-28
A C E F
```

这样以后甚至能看到：

> “这首歌什么时候进入我的某个播放列表？”
>
> “2024 年我的通勤列表是什么样？”
>
> “哪些歌在播放列表里待了三年？”
>
> “哪些歌被移除后又重新加入？”
>
> “一个播放列表过去几年是怎样演化的？”

这个数据是很有价值的，而 Musicolet 本身未必帮你保留历史版本。

因此我建议最终做成：

```text
Musicolet
│
├── 当前歌曲库
├── 当前播放列表
├── 当前队列
├── Favorites
├── Play counts
└── Playback history
        │
        ▼
   Sync importer
        │
        ├── current state
        │
        └── historical snapshots
                │
                ▼
          你的音乐镜像站
```

所以**现在真正值得做的不是研究怎么批量点 50 次 CSV 导出，而是把那个 Backup ZIP 格式彻底拆明白**。

你把一个原始 Musicolet Backup 压缩包发给我即可。我可以先把里面每类文件对应什么、哪些能解成 SQLite、播放列表究竟怎么存、有没有列表内部 ID 和更新时间这些关键结构查清楚。搞清这些之后，我们就能确定能不能做到“一次点击备份 → 全部播放列表和播放历史自动同步”。






问：



	你看看有没有办法解密，如果可以就解密了，然后重新打包给我
	
	
	

答：



可以，已经实际解密成功并重新打包。

这份备份确认使用的是 **Blowfish ECB + 固定密钥 `JSTMUSIC_2` + PKCS#5 padding**。解密后：

* `.mpl` 播放列表是可读 JSON；
* `DB_SONGS_LOG`、`DB_BDN`、`PCs_*` 是标准 SQLite；
* `0.names` 是 Java Serialization；
* 其他多数设置/队列/收藏文件是 JSON；
* `PCs_Y_2016`、`PCs_Y_2017` 解密后为空文件，这与原备份校验一致，不是解密失败。

我还用备份自身的 MD5 机制做了完整验证：`0.musicolet.backup` 记录的 **87 个文件 MD5 全部与解密后的明文一致**，`hash` 也与解密后的 manifest MD5 完全一致。因此可以确认这批明文是正确的。

重新打包后仍保持原来的 **89 个文件名和目录结构**：

[下载解密后的 Musicolet 备份](sandbox:/mnt/data/2026-08-25%2019-12-09-decrypted.zip)

这个 ZIP 是给你**分析、建镜像数据库**用的，不建议直接拿它让 Musicolet 恢复，因为 Musicolet 恢复流程本身预期读取的是加密文件。

下一步其实就很有意思了：可以直接开始把 `DB_SONGS_LOG`、`0.qstk`、`0.favs` 和近 50 个 `.mpl` 的结构全部拆出来，确定**歌曲、播放列表、队列、收藏、逐时间播放记录**分别存在哪里。





问：


	你看看数据里是不是有记录某一首歌曲多次播放的精确时间记录？
	
	
	
答：



我把这份解密后的备份完整查了一遍。结论是：

**没有发现“同一首歌每播放一次，就保存一个精确时间戳”的历史记录。**

也就是说，目前这份 Musicolet Backup 里没有类似这样的数据：

```text
南野陽子 - 悲しみモニュメント

2026-05-03 21:17:42
2026-05-04 00:31:18
2026-05-11 18:52:09
...
2026-08-17 19:51:23
```

### 实际保存的是什么

核心数据库 `DB_SONGS_LOG` 里，每首歌只有这些与播放历史有关的字段：

```sql
COL_NUM_PLAYED
COL_LAST_PLAYED
COL_NUM_PLAYED_W
COL_NUM_PLAYED_M
COL_NUM_PLAYED_Y
```

其中：

* `COL_NUM_PLAYED`：总播放次数
* `COL_LAST_PLAYED`：**最后一次播放的精确时间，精确到毫秒**
* `COL_NUM_PLAYED_W`：当前周期的周播放次数
* `COL_NUM_PLAYED_M`：当前周期的月播放次数
* `COL_NUM_PLAYED_Y`：当前周期的年播放次数

比如你数据库里真实存在这一条：

```text
悲しみモニュメント
艺术家：南野陽子

总播放次数：147
最后播放：
2026-08-17 19:51:23.880
```

所以它知道：

> 这首歌累计播放过 147 次，最后一次是在 2026-08-17 19:51:23.880。

但前面 **146 次各自在什么时间播放，并没有存在 `DB_SONGS_LOG` 中。**

---

另外那些：

```text
PCs_Y_2025
PCs_M_2025.7
PCs_M_2025.8
...
PCs_M_2026.6
PCs_W_2026.34
```

我也逐个看了 schema。

它们结构极其简单：

```sql
CREATE TABLE TABLE_SONGS (
    COL_PATH TEXT primary key,
    COL_NUM_PLAYED INTEGER DEFAULT 0
);
```

也就是说里面只是：

```text
歌曲 A → 2025 年播放 17 次
歌曲 B → 2025 年播放 3 次
歌曲 C → 2025 年播放 42 次
```

或者：

```text
歌曲 A → 2026 年 6 月播放 2 次
```

**没有时间戳字段。**

所以 Musicolet 的统计设计大概是：

```text
                   总次数
                      │
                      ▼
              DB_SONGS_LOG
              COL_NUM_PLAYED

                   +
                   │
       ┌───────────┼───────────┐
       ▼           ▼           ▼
      年            月           周
 PCs_Y_2025   PCs_M_2026.6  PCs_W_2026.34

                   +
                   │
                   ▼
           最后一次播放时间
           COL_LAST_PLAYED
```

而不是：

```text
play_event
────────────────────────────
song     timestamp
A        2026-08-01 03:21:18
A        2026-08-03 14:51:27
A        2026-08-17 19:51:23
```

### 我也检查了其他备份文件

我额外查了：

```text
0.qstk
0.favs
所有 .mpl
PP
USP
SSP
RPN
OMV
DB_BDN
```

并递归搜索了 JSON 中所有看起来像 Unix 毫秒时间戳的 13 位整数。

除了几个：

* 备份时间
* 软件设置相关时间
* 首次安装/配置时间之类的数据

之外，**没有发现大量歌曲播放时间戳**。

如果存在逐次播放历史，按你现在有 6652 首歌、很多歌几十乃至上百次播放的规模，备份中应该能看到成千上万个 13 位时间戳或者一个明显的 event/log 表；实际上完全没有。

另外 `DB_SONGS_LOG` 中有个名字乍看很可疑的：

```text
COL_LOGPATH
```

我也专门检查了。

它不是播放日志地址，而是类似：

```text
Storage/primary/存档/原机存档目录/音频整理/AlarmClock/南野陽子 - 悲しみモニュメント.mp3
```

本质上还是歌曲文件路径。

---

## 不过保存的历史粒度其实比“只有总次数”强不少

虽然拿不到：

> 每一次播放的时间

但我们可以建立：

```text
歌曲
├─ 总播放次数
├─ 最后一次播放精确时间
├─ 各年播放次数
├─ 最近约 12 个月各月播放次数
└─ 当前周期周/月/年播放次数
```

你这份备份里现在就有：

```text
年度：
2018
2019
2020
2021
2022
2023
2024
2025

月度：
2025-08
2025-09
2025-10
2025-11
2025-12
2026-01
2026-02
2026-03
2026-04
2026-05
2026-06
2026-07

以及近期周统计
```

这里 `.7` 很可能采用 **Java/Calendar 的 0 起始月份**：

```text
0 = January
1 = February
...
7 = August
11 = December
```

所以：

```text
PCs_M_2025.7
```

就是 **2025 年 8 月**，不是 7 月。

而当前周期的数据应该直接放在 `DB_SONGS_LOG` 的：

```text
COL_NUM_PLAYED_W
COL_NUM_PLAYED_M
COL_NUM_PLAYED_Y
```

到了周期结束再滚进相应 `PCs_*` 数据库。

---

这对你做镜像站还有一个很重要的启示：

**从现在开始，我们完全可以自己补上 Musicolet 没有保存的逐次历史。**

例如你每天或每几小时生成一次 backup，我们比较前后两次：

```text
18:00 backup
歌曲 A：
COL_NUM_PLAYED = 37

20:00 backup
歌曲 A：
COL_NUM_PLAYED = 38
COL_LAST_PLAYED = 19:43:17.381
```

就可以确定：

```text
歌曲 A
2026-08-25 19:43:17.381
+1 play
```

以后你的镜像数据库就真正拥有：

```text
play_events
id
song_id
played_at
```

只是**过去已经发生的那 147 次播放无法从这一份现有 Backup 反推出每一次具体时间**；能保留下来的过去信息，是年/月等统计以及最后一次播放时间。




问：



	了解了
	还有个重要的事
	假定我做好了镜像musicolet功能的系统，那么第一次导入数据是全量的
	第二次开始，就应该知道有哪些数据差异，是应该对哪些数据增加、删除、修改、移动等等

	我希望系统应该留存每次导入数据时的musicolet原始zip备份包，方便对比差异，再根据差异调整系统中的数据库

	这个没问题吧




答：





完全没问题，而且我认为**这是这个镜像系统最应该从一开始就确定下来的核心原则之一**：

> **每次 Musicolet 导入都不是“覆盖数据库”，而是一次不可变的快照（snapshot）。原始 ZIP 永久保留，然后由新旧快照计算差异，再把差异应用到镜像数据库。**

这样设计非常稳。

建议整个链路做成：

```text
Musicolet Backup ZIP
        │
        ▼
① 原始包永久归档
        │
        ▼
② 解密 / 解析
        │
        ▼
③ 生成标准化快照
        │
        ▼
④ 与上一次快照比较
        │
        ▼
⑤ 生成 ChangeSet
        │
        ▼
⑥ 应用到镜像数据库
        │
        ▼
⑦ 保存本次变更历史
```

### 第一次导入

第一次没有 predecessor，所以：

```text
snapshot #1

所有歌曲          ADD
所有播放列表      ADD
所有列表成员      ADD
Favorites         ADD
Queue             ADD
播放统计          初始化
Settings          初始化
```

相当于 full import。

### 第二次以后

例如：

```text
snapshot #1
2026-08-25 19:12

snapshot #2
2026-08-28 03:20
```

系统比较两者：

```text
歌曲：
+ 新增 5 首
- 删除 2 首
~ 修改 3 首 metadata
↔ 1 首文件路径发生变化

播放列表：
+ 新建 1 个
- 删除 1 个
~ 重命名 2 个

「车播」：
+ 加入歌曲 E
- 移除歌曲 B
↕ 歌曲 C：第 17 位 → 第 4 位

Favorites：
+ 3
- 1

播放统计：
歌曲 A  121 → 124    +3
歌曲 B   38 → 39     +1

Last Played：
歌曲 A
2026-08-24 23:11
→
2026-08-27 19:32
```

然后产生一份明确的：

```text
ChangeSet #2
```

镜像数据库只根据 ChangeSet 修改。

---

这里有一个很重要的地方：

**原始 ZIP 用来留档和追溯，但不要直接拿两个 ZIP 做二进制 diff。**

因为 ZIP 本身可能因为：

* 文件排列变化
* 压缩结果变化
* SQLite page layout 变化
* Musicolet 内部字段变化
* backup timestamp 变化

导致整个文件 hash 不同，但业务数据可能压根没变化。

所以应该比较：

```text
ZIP A
 ↓
解析
 ↓
Normalized Snapshot A

          VS

ZIP B
 ↓
解析
 ↓
Normalized Snapshot B
```

例如统一成：

```json
{
  "tracks": [],
  "playlists": [],
  "favorites": [],
  "queues": [],
  "play_counts": {},
  "settings": {}
}
```

然后比较这些逻辑对象。

## 我甚至建议保存三层数据

每次导入都保存：

```text
imports/
└── 2026-08-25_19-12-09/
    ├── original.zip
    ├── decrypted.zip
    ├── snapshot.json
    └── changeset.json
```

其中：

**original.zip**

就是 Musicolet 原封不动导出的东西。

它是最高等级的证据源，永远不修改。

**decrypted.zip**

我们现在给你生成的这种版本。

以后即使解密工具发生变化，也不用再次研究旧格式。

这个可以选择永久保存，也可以认为能从 original.zip 重建而不保存。不过文件不大的情况下，我倾向于保留。

**snapshot.json**

这是我们自己的“标准化 Musicolet 状态”。

比如：

```text
snapshot_version: 1
source_backup_sha256: xxx
import_time: xxx
musicolet_version: xxx

tracks
playlists
playlist_items
favorites
queues
play_counts
...
```

这样以后即使 Musicolet 改数据库格式，我们只需要修改：

```text
Musicolet parser
```

而你的镜像站后半部分完全不用变。

**changeset.json**

记录：

```text
snapshot_001
        ↓
snapshot_002
```

到底发生了什么。

例如：

```json
{
  "playlists": {
    "created": [],
    "deleted": [],
    "renamed": [],
    "tracks_added": [],
    "tracks_removed": [],
    "tracks_moved": []
  }
}
```

这东西以后非常有价值。

---

### “移动”一定要作为一等变更，不要简单理解成删除+新增

比如：

```text
原列表

A
B
C
D
E
```

变成：

```text
A
D
B
C
E
```

不能记录成：

```text
删除 D
新增 D
```

应该记录：

```text
MOVE D
position 4 → 2
```

所以播放列表比较最好使用 sequence diff / LCS 一类算法，识别：

```text
ADD
REMOVE
MOVE
```

这样才能真正复刻 Musicolet 的编辑行为。

---

### 歌曲“身份”也必须认真设计

这可能会成为整个同步系统里最容易踩坑的一处。

假设：

```text
/storage/Music/A.mp3
```

改名：

```text
/storage/Music/Artist - A.mp3
```

如果我们简单用路径当主键，就会认为：

```text
DELETE A.mp3
ADD Artist - A.mp3
```

但实际上是：

```text
MOVE / RENAME
```

所以理想的 identity strategy 应该类似：

```text
Musicolet 自身稳定 ID
        ↓ 如果没有
文件内容 fingerprint
        ↓
音频 metadata + duration + filesize
        ↓
path
```

具体用哪一层，得继续解析你这份 Musicolet 数据后再决定。

如果 Musicolet 本身存在稳定的内部 song ID，那最好，直接继承。

---

### 播放次数还有一个特殊意义

比如：

```text
上次：
A = 147

本次：
A = 150
```

ChangeSet 应记录：

```text
play_count_delta = +3
```

而不是单纯：

```text
147 → 150
```

你的镜像数据库可以同时保存：

```text
musicolet_play_count = 150
```

以及：

```text
import-derived delta = +3
```

如果：

```text
COL_LAST_PLAYED
```

也发生变化，那么至少最后一次播放可以精确建立事件：

```text
2026-08-27 19:31:42.185
```

至于 +3 中另外两次什么时候发生，就像我们上一轮分析的一样，**如果两次备份之间没有更细粒度的快照，无法恢复精确时间**。

这也意味着未来如果你很看重播放事件，可以提高备份频率。

---

## 还有一个我很建议增加的东西：Import 不可修改

例如数据库：

```text
imports

id
source_sha256
source_filename
backup_time
import_time
parser_version
previous_import_id
status
```

一旦：

```text
Import #37
```

成功完成，就永远不要修改它。

以后哪怕我们发现 parser v1 有 bug，也不要改 #37，而是：

```text
原始 ZIP #37
    ↓
parser v2
    ↓
rebuild snapshot
```

这样系统可以随时从全部原始备份**重新构建整个镜像数据库**。

这是非常重要的能力。

最终你的系统应该满足：

```text
删除整个镜像数据库
        ↓
只留下历史 Musicolet ZIP
        ↓
重新运行 importer
        ↓
完整重建

当前状态
+
历史变更
+
播放统计
+
播放列表演化过程
```

能够做到这一点，架构就很可靠了。

所以你提出的“**永久保留每次 Musicolet 原始 ZIP，再根据相邻导入计算增删改移**”不但没问题，我认为应该直接把它定成整个系统的**数据来源原则（source of truth）**。镜像数据库是派生数据，**原始 Musicolet Backup 才是不可变的原始事实**。




问：




	如果拉取入库歌曲要对歌曲文件关联编号ID，不要改写歌曲在手机上的原路径（除非手机上的路径也改了才能改），而是要增加一个字段用于记录歌曲在服务器的存储位置。 
	 这里会有一个歌曲数据与编号是否准确关联的问题：手机上的歌曲本身没有编号，看看musicolet导出的数据中有没有类似编号的东西，如果有且每次导出的同一个歌曲所属编号是稳定不变的，那就好办

	你先看看musicolet导出的数据有没有歌曲编号这东西





答：




我已经把你这份解密后的备份里能涉及歌曲的地方都查了一遍。结论比较明确：

**目前没有发现 Musicolet 给每首歌曲分配一个独立、稳定的 `song_id`。它实际上主要把“歌曲路径”当作歌曲身份。**

最直接的证据是 `DB_SONGS_LOG`。歌曲主表结构开头是：

```sql
TABLE_SONGS

COL_PATH          TEXT PRIMARY KEY
COL_TITLE         TEXT
COL_ARTIST        TEXT
COL_ALBUM         TEXT
COL_GENRE         TEXT
COL_COMPOSER      TEXT
COL_YEAR          INTEGER
COL_DURATION      INTEGER
...
COL_NUM_PLAYED
COL_LAST_PLAYED
...
```

注意这里：

```sql
COL_PATH TEXT PRIMARY KEY
```

**主键就是路径，没有 `COL_ID`、`SONG_ID` 之类字段。**

而且不只是这一个数据库。

播放次数历史库：

```text
PCs_Y_2025
PCs_M_2026.6
PCs_W_2026.34
...
```

也全部是：

```sql
CREATE TABLE TABLE_SONGS (
    COL_PATH TEXT PRIMARY KEY,
    COL_NUM_PLAYED INTEGER
);
```

还是通过路径关联。

播放列表 `.mpl` 也一样。例如：

```json
{
  "S_P": [
    "content://com.android.externalstorage.documents/...xxx.mp3",
    "content://com.android.externalstorage.documents/...yyy.mp3"
  ],
  "S_T": [...],
  "S_AL": [...],
  "S_AR": [...],
  "S_D": [...]
}
```

其中：

* `S_P` = path
* `S_T` = title
* `S_AL` = album
* `S_AR` = artist
* `S_D` = duration

**没有对应的歌曲 ID 数组。**

收藏：

```text
0.favs
```

也是同样：

```text
S_P
S_T
S_AL
S_AR
S_D
```

队列：

```text
0.qstk
```

里面最终引用歌曲的仍然是：

```text
S_P = content://...
```

我还全局扫了这 89 个文件的 SQLite 字段和 JSON key，没有发现隐藏的：

```text
song_id
track_id
media_id
audio_id
```

之类歌曲编号。

唯一看到真正的：

```sql
COL_ID INTEGER PRIMARY KEY
```

是在 `DB_BDN`：

```text
TABLE_DEVICES
```

这个是**蓝牙设备编号**，不是歌曲。

另外 `USP` 中有一个：

```text
FID = 1690474162086
```

但这是全局设置值，不是逐歌曲 ID。

---

## 所以目前可以判断 Musicolet 的歌曲关联模型是

```text
                 COL_PATH
                    │
       ┌────────────┼────────────┐
       ▼            ▼            ▼
 DB_SONGS_LOG     Playlist      Favorites
       │
       ▼
 Play-count databases
```

例如同一首歌曲：

```text
content://com.android.externalstorage.documents/
tree/primary%3A存档...
document/primary%3A存档...%2Fxxx.mp3
```

这个完整 URI 就相当于 Musicolet 内部的“歌曲 key”。

这与你希望的设计有一个重要区别。

你希望：

```text
我们自己的 song_id = 17382

mobile_path =
/storage/emulated/0/Music/xxx.mp3

server_path =
/music/17/17382.mp3
```

这是**正确的设计**。

不要把：

```text
mobile_path
```

或者：

```text
server_path
```

拿来当我们自己数据库的主键。

应该自己产生一个永久 ID：

```text
songs

id                    17382
mobile_path           ...
musicolet_path        content://...
server_storage_path   ...
title
artist
duration
...
```

其中：

### `id`

我们自己的永久歌曲编号。

一旦建立：

```text
17382
```

以后永远是这首歌。

### `musicolet_path`

保存 Musicolet 当时记录的原始 URI。

### `mobile_path`

保存实际手机文件路径。

**手机文件没移动，就绝不能因为上传服务器而改这个字段。**

### `server_storage_path`

完全独立：

```text
/data/music/017/17382.mp3
```

服务器怎么存都不影响手机路径。

---

不过现在出现了你刚才已经预见到的真正难点：

> **如果 Musicolet 没有稳定 song ID，那么手机上的歌曲改名/移动以后，下一次导入怎么知道它还是原来的那首？**

例如：

第一次：

```text
song_id = 17382

/storage/Music/A.mp3
```

手机上改成：

```text
/storage/Music/JPop/Artist - A.mp3
```

下一份 Musicolet backup 中：

```text
COL_PATH
```

也会变。

如果单纯比较路径：

```text
旧路径不存在 → DELETE
新路径出现   → ADD
```

这显然是错的。

实际上应该识别成：

```text
song_id 17382

mobile_path:
旧路径
→
新路径
```

**歌曲 ID 不变。**

所以既然 Musicolet 没给我们稳定 ID，我们自己的镜像系统必须负责“歌曲身份解析”。

我建议至少做成这样的优先级：

```text
① 已知 Musicolet path 完全相同
        ↓
   直接命中已有 song_id

② path 变化
        ↓
   文件内容 hash / audio fingerprint
        ↓
   命中已有 song_id

③ 无法取得文件本体
        ↓
   title + artist + album + duration
   + filesize/date_modified 等综合匹配

④ 仍然无法可靠判断
        ↓
   标记为待确认
   绝不能擅自 DELETE + ADD
```

尤其你的系统以后既然本身就会把歌曲文件上传服务器，那么事情反而好办：

**第一次上传后，服务器已经拥有歌曲文件。**

可以计算：

```text
SHA-256
```

甚至音频 fingerprint。

例如：

```text
song_id: 17382
sha256: 37c93f...
mobile_path: /Music/A.mp3
server_path: /music/17382.mp3
```

下一次发现 Musicolet 出现一个新路径：

```text
/Music/JPop/A.mp3
```

读取手机文件算出：

```text
sha256 = 37c93f...
```

于是直接知道：

```text
不是新歌

而是：
song_id=17382
PATH_CHANGED
```

所以结论是：

> **你这份 Musicolet 备份中，没有找到可以直接继承使用的稳定“歌曲编号”；Musicolet 是以路径作为歌曲主键的。**

因此我们的系统最好从**第一次全量导入时自己给每首歌曲生成永久 ID**，以后 Musicolet 的路径只作为歌曲的一个可变属性，而不是我们的身份主键。

这反而更稳，因为以后哪怕彻底不用 Musicolet，`song_id=17382` 仍然不会变化。






问：



	歌曲路径也不能当作id依据吧？
	一首歌曲关联了播放记录、歌曲清单、播放队列等数据，如果改了路径怎么办。

	虽然musicolet已经尽力让文件路径变更后的歌曲依旧能够关联到这些数据，但是如果musicolet导出的数据给到服务器，你还能保证服务器也能处理这么好吗
	
	



答：





对，**歌曲路径绝不能作为我们服务器自己的歌曲 ID 依据**。我上一条说“Musicolet 用路径作为 key”，说的是它这份**导出格式的引用方式**，不是说我们的系统应该照抄这种设计。

而你指出的是整个镜像系统里一个非常核心的问题：

> Musicolet 自己在手机上可能有能力识别“这个文件只是改名/移动了，还是原来那首歌”；但它导出的备份却主要以路径引用歌曲。服务器只看到两次 ZIP 时，未必拥有 Musicolet 当时用来完成这种身份迁移的全部内部信息。

所以答案是：

**如果服务器只有 Musicolet ZIP，我不能保证 100% 正确识别所有路径变更。**

例如：

```text
第一次备份：

/Music/A.mp3
播放次数 127
属于：
- Playlist X
- Playlist Y
- Favorites
- Queue
```

之后手机上：

```text
/Music/A.mp3
↓
/Music/JPOP/南野陽子 - A.mp3
```

第二次备份可能只告诉服务器：

```text
旧路径消失
新路径出现
```

即使 Musicolet 手机端知道它们是同一首，**如果这个身份信息没有被写进 backup，服务器单凭 ZIP 无法凭空知道。**

---

## 因此必须把“歌曲身份”和“Musicolet 路径”彻底拆开

我们的数据库应该是：

```text
song
────────────────────────
id                 18427   ← 永久 ID
title
artist
duration
...

song_location
────────────────────────
song_id            18427
source             musicolet
path               /Music/A.mp3
first_seen_import  1
last_seen_import   17
active             false

song_location
────────────────────────
song_id            18427
source             musicolet
path               /Music/JPOP/南野陽子 - A.mp3
first_seen_import  18
last_seen_import   ...
active             true
```

也就是说：

**路径变化不是 song 变化，而是 song_location 变化。**

历史路径也不应该删除。

这样：

```text
song_id = 18427
```

始终关联：

```text
播放历史
播放次数
Favorites
播放列表
播放队列
服务器文件
标签
封面
备注
```

而路径只是它的一项历史属性。

---

# 真正的问题是：怎么判断两个路径对应同一个 `song_id`

这里不能只靠一条规则。

我会设计成一个多级 identity resolver。

### 第一层：路径没变

最简单：

```text
backup N:
/Music/A.mp3

backup N+1:
/Music/A.mp3
```

直接关联：

```text
song_id = 18427
```

没有任何歧义。

---

### 第二层：服务器已经拥有歌曲文件——使用文件 hash

这是非常可靠的一层。

第一次同步歌曲文件时计算：

```text
SHA-256:
845c0e76....
```

以后即使手机路径变成：

```text
/Music/JPOP/A.mp3
```

如果取得新位置的文件：

```text
SHA-256:
845c0e76....
```

那么直接：

```text
旧文件 == 新文件
```

所以：

```text
song_id 18427
path changed
```

而不是：

```text
delete song 18427
add song 29831
```

这种情况下我可以做到**确定性关联**。

---

## 但 SHA-256 也不能解决所有问题

例如用户给 MP3 改了一下：

```text
ID3 Title
Album
封面
ReplayGain
```

音频其实完全一样，但文件二进制已经变了：

```text
SHA256 old != SHA256 new
```

所以还要下一层。

### 第三层：音频指纹

例如计算类似：

```text
Chromaprint / AcoustID fingerprint
```

主要针对音频内容，而不是整个文件。

于是：

```text
旧：
A.mp3
SHA256 = xxx
fingerprint = ABC123

新：
南野陽子 - A.mp3
SHA256 = yyy
fingerprint = ABC123
```

可以非常有把握判断：

> 同一录音，只是文件 metadata / 容器发生变化。

对于你的系统，这比单纯 SHA-256 更适合作为长期歌曲身份依据。

---

# 甚至 fingerprint 也不能直接成为唯一 ID

这是另一个容易犯的错误。

假如你有：

```text
歌曲 A.mp3
歌曲 A copy.mp3
```

两份内容完全相同。

它们 fingerprint 相同，甚至 SHA-256 都可能一样。

但在 Musicolet 中它们可能是**两个独立文件项**：

```text
/Music/A.mp3
/Backup/A.mp3
```

它们可能分别出现在不同播放列表里。

因此不能：

```text
fingerprint = primary key
```

真正的模型应该还是：

```text
我们的内部 ID：
song_instance_id
```

然后它拥有：

```text
content_hash
audio_fingerprint
paths[]
```

---

# 我甚至倾向于区分两种 ID

这会更严谨：

```text
track_id
```

代表“音乐作品/录音身份”。

例如：

```text
南野陽子 - 悲しみモニュメント
某个具体 master / recording
```

另外：

```text
media_file_id
```

代表“手机里的这个文件实例”。

于是：

```text
track_id = 531

media_file_id = 18427
path = /Music/A.mp3
```

如果只是移动：

```text
media_file_id 18427
```

仍然不变。

如果复制一份：

```text
media_file_id 18427
media_file_id 29874
```

两者：

```text
track_id = 531
fingerprint = same
```

这样模型比 Musicolet 自身更完整。

对于播放列表、Queue 等从 Musicolet 导入的结构，我建议首先关联：

```text
media_file_id
```

因为 Musicolet 操作的实际上是文件项目。

---

# 还有一类非常关键的证据：相邻快照的关系

即使没有歌曲文件本体，也不应该简单：

```text
旧路径消失
+
新路径出现
=
DELETE + ADD
```

而应该进行关联推断。

例如 Import #15：

```text
/Music/A.mp3

title      悲しみモニュメント
artist     南野陽子
album      ...
duration   04:07.321

play_count 147

出现在：
Playlist 2 position 17
Playlist 8 position 4
Favorites
```

Import #16：

旧路径消失，同时出现：

```text
/Music/JPOP/南野陽子 - 悲しみモニュメント.mp3

title      悲しみモニュメント
artist     南野陽子
album      ...
duration   04:07.321

play_count 148

出现在：
Playlist 2 position 17
Playlist 8 position 4
Favorites
```

即使没有 hash，这个组合证据已经极强：

```text
title     一样
artist    一样
duration  一样
album     一样
play count 连续
playlist membership 一样
playlist position 一样
favorite 状态一样
旧项恰好消失
新项恰好出现
```

系统完全可以判断：

```text
RELOCATE candidate

old_path → new_path
confidence = 99.x%
```

而不是创建新歌曲。

---

# Musicolet 的播放数据本身还能作为“身份指纹”

这是你这个项目非常特别的地方。

例如旧文件：

```text
总播放 147

2024: 21
2025: 63
2026: 63

2026-06: 8
2026-07: 17
...
```

新路径出现后：

```text
总播放 148

2024: 21
2025: 63
2026: 64
```

这种统计序列其实非常难碰巧完全一致。

所以可以把：

```text
历史 play-count vector
```

作为 identity resolver 的证据之一。

同样包括：

```text
Favorites
Playlist memberships
Playlist positions
Queue presence
Last played
```

这甚至能够在**拿不到歌曲文件**时帮助恢复路径迁移。

---

# 但是一定要保留“不确定”状态

我不会设计成所有情况都强行自动判断。

例如旧：

```text
/Music/A.mp3
Title: Track 01
Artist: Unknown
Duration: 03:30
Play count: 0
```

同时新出现：

```text
/Music2/A.mp3
Title: Track 01
Artist: Unknown
Duration: 03:30
Play count: 0
```

如果没有文件 hash，也没有播放历史，也没有播放列表关系：

**不能假装知道它们是同一首。**

系统应该产生：

```text
possible relocation

old media_file_id: 1827
new candidate: /Music2/A.mp3

confidence: 64%
```

要求人工确认：

```text
[同一歌曲]
[新歌曲]
```

用户确认一次后：

```text
identity_link
```

永久保存，以后再也不用判断。

---

# 所以我会把导入流程设计成两阶段，而不是直接更新数据库

```text
Musicolet ZIP #37
        │
        ▼
解析 Snapshot #37
        │
        ▼
与 Snapshot #36 比较
        │
        ▼
Identity Resolution
        │
        ├─ EXACT MATCH
        ├─ PATH CHANGE
        ├─ CONTENT CHANGE
        ├─ NEW FILE
        ├─ REMOVED FILE
        └─ AMBIGUOUS
        │
        ▼
生成 ChangeSet
        │
        ▼
应用到镜像数据库
```

尤其注意：

**必须先完成 Identity Resolution，再处理 playlist diff。**

否则会出现最糟糕的问题：

```text
A 原路径删除
A 新路径新增
```

系统误认为：

```text
旧歌曲删除
新歌曲新增
```

然后连锁产生：

```text
Playlist：
REMOVE old A
ADD new A

Favorites：
REMOVE old A
ADD new A

Queue：
REMOVE old A
ADD new A
```

实际上只发生了一件事：

```text
SONG_LOCATION_CHANGED
```

其他所有关系应该**零变化**。

---

## 最终目标应该是这种效果

例如：

```text
song/media_file #18427
```

从建立开始永久不变。

历史：

```text
2026-01-01 ~ 2026-08-25
/storage/emulated/0/Music/A.mp3

2026-08-25 ~ 2027-03-11
/storage/emulated/0/Music/JPOP/A.mp3

2027-03-11 ~
/storage/emulated/0/Music/JPOP/南野陽子 - A.mp3
```

服务器：

```text
/media/18427/original.mp3
```

完全可以永远不变。

所有业务关系：

```text
play_events          → 18427
play_counts          → 18427
playlist_items       → 18427
favorite             → 18427
queue_items          → 18427
tags                 → 18427
```

都跟路径无关。

---

所以你问：

> 如果 Musicolet 导出的数据给到服务器，你还能保证服务器也能处理这么好吗？

准确答案是：

**只靠 Musicolet ZIP 本身，不能承诺任何路径变化场景都 100% 无误，因为它没有给我们导出一个明显的稳定 song ID。**

但如果我们的镜像系统同时拥有：

* 每次不可变的 Musicolet 原始 ZIP；
* 前后 Snapshot；
* 自己的永久 `media_file_id`；
* 手机歌曲文件；
* SHA-256；
* 音频 fingerprint；
* metadata + duration；
* 历史播放统计；
* Playlist / Favorite / Queue 关系；
* 历史路径映射；
* 不确定情况下的人工确认；

那么实际上可以做到**比仅仅依赖 Musicolet 导出格式健壮得多**。

而且有一个原则我会坚持：

> **宁可产生一个“待确认的歌曲身份变更”，也绝不因为路径变化而擅自给一首已有大量历史数据的歌曲换 ID。**

这应该成为整个 Musicolet 镜像同步系统的数据完整性红线。





问：



	你再考察下musicolet备份数据中的歌曲清单、播放队列是如何组织数据的，歌曲是以某种编号还是直接以路径来关联到歌曲清单或播放队列？
	
	
	
	
	
	
答：





我重新按你这份真实备份逐项核对过了。结论现在可以说得很确定：

**Musicolet 备份里的播放列表和播放队列，都不是通过歌曲编号关联歌曲，而是直接保存歌曲路径/URI。没有发现 song_id / track_id。**

而且这次我做了逐条交叉验证，不只是看字段名。

### 1. 播放列表 `.mpl`：直接保存路径

比如你的：

```text
车播：常规.mpl
```

解密后结构是：

```json
{
  "S_P": [
    "content://com.android.externalstorage.documents/.../南野陽子 - 悲しみモニュメント.mp3",
    "content://com.android.externalstorage.documents/.../岡田有希子 - ファースト・デイト....mp3"
  ],
  "S_T": [
    "悲しみモニュメント",
    "ファースト・デイト (オリジナルカラオケ)"
  ],
  "S_AL": [
    "悲しみモニュメント",
    "贈りものⅢ..."
  ],
  "S_AR": [
    "南野陽子",
    "岡田有希子"
  ],
  "S_D": [
    245891,
    170527
  ]
}
```

字段实际上是五个**下标一一对应的数组**：

```text
S_P  Path
S_T  Title
S_AL Album
S_AR Artist
S_D  Duration
```

例如下标 `0`：

```text
S_P[0]  = 南野陽子那首歌的 URI
S_T[0]  = 悲しみモニュメント
S_AL[0] = 悲しみモニュメント
S_AR[0] = 南野陽子
S_D[0]  = 245891
```

这五项共同描述播放列表第 1 首歌。

**没有这样的东西：**

```json
{
    "song_id": 12345
}
```

播放列表的顺序，本质上就是 `S_P[]` 数组的顺序。

---

### 2. 我检查了全部 54 个 `.mpl`

你的备份里：

```text
54 个播放列表
29265 条播放列表歌曲记录
5168 个不同歌曲路径
```

五个数组：

```text
S_P
S_T
S_AL
S_AR
S_D
```

在全部 54 个播放列表里长度都严格一致。

因此可以明确认为：

```text
playlist item #N
    ↓
S_P[N]
S_T[N]
S_AL[N]
S_AR[N]
S_D[N]
```

就是一个播放列表项目。

不是：

```text
playlist item
    ↓
song_id
    ↓
song table
```

---

## 3. `DB_SONGS_LOG` 也是以路径为主键

歌曲数据库：

```sql
TABLE_SONGS
```

第一列就是：

```sql
COL_PATH TEXT PRIMARY KEY
```

完整结构里没有独立歌曲 ID：

```text
COL_PATH              PRIMARY KEY
COL_TITLE
COL_ARTIST
COL_ALBUM
COL_GENRE
COL_COMPOSER
COL_YEAR
COL_DURATION
COL_TRACK_NO
COL_DATE_ADDED
COL_DATE_MODIFIED
COL_NUM_PLAYED
COL_LAST_PLAYED
...
```

也就是说 Musicolet 备份的数据模型实际上类似：

```text
TABLE_SONGS

COL_PATH = "content://....../A.mp3"
        ↑
        │
播放列表 S_P[] 也是这个字符串
```

我实际逐条对过。

54 个播放列表的 29265 条记录中：

```text
29180 条
```

其 `S_P` 可以**原字符串精确匹配**：

```text
DB_SONGS_LOG.TABLE_SONGS.COL_PATH
```

所以这不是“根据歌名猜出来”的关联。

它真的是：

```text
playlist.S_P
        ==
TABLE_SONGS.COL_PATH
```

---

# 4. 播放队列 `0.qstk` 更明显：只有路径

这个文件很有意思。

顶层：

```json
{
    "S0_CPQ": 13,
    "S0_PQ": [...]
}
```

你当前一共保存了：

```text
14 个播放队列
```

其中一个队列大概是：

```json
{
    "S0_PQ_T": "至喜²｜H↑",
    "S0_PQ_CPS": 78,
    "S0_PQ_LKP": 56625,
    "S0_PQ_OQS": {
        "S_P": [
            "content://....../A.mp3",
            "content://....../B.mp3",
            "content://....../C.mp3"
        ]
    }
}
```

尤其关键的是：

```text
S0_PQ_OQS
    └── S_P[]
```

**播放队列连 title / artist / duration 都没有重复保存。**

它直接就是：

```text
歌曲路径
歌曲路径
歌曲路径
歌曲路径
...
```

完全没有：

```text
song_id
track_id
media_id
```

我递归扫了整个 `0.qstk` 所有 key，也没有任何 ID 类字段。

---

## 5. 播放队列的路径关联验证是 100%

你的 14 个 Queue 中总共有：

```text
15775 个队列项
6331 个不同歌曲
```

我把全部 15775 条 `S_P` 与：

```text
DB_SONGS_LOG.TABLE_SONGS.COL_PATH
```

做了精确字符串匹配。

结果：

```text
15775 / 15775
```

**全部匹配。**

也就是 100%。

所以播放队列的数据关系可以直接画成：

```text
0.qstk

Queue A
   │
   ├── S_P[0] ─────────────┐
   ├── S_P[1] ──────────┐  │
   ├── S_P[2] ───────┐  │  │
   │                  │  │  │
   ▼                  ▼  ▼  ▼
DB_SONGS_LOG.TABLE_SONGS

COL_PATH ← PRIMARY KEY
```

---

# 6. `S0_CPQ` 等应该是 Queue 自身状态，不是歌曲 ID

例如你的：

```text
S0_CPQ = 13
```

而一共：

```text
S0_PQ = 14 个队列
```

所以它**高度符合**：

```text
S0_CPQ = Current Playing Queue
```

也就是当前使用第 13 号（0-based）队列。

这个队列正好是：

```text
13 = 至喜²｜H↑
```

里面：

```text
S0_PQ_CPS = 78
S0_PQ_LKP = 56625
```

很可能分别是类似：

```text
CPS = 当前歌曲位置/index
LKP = 当前歌曲播放 position
```

例如 `56625` 很像：

```text
56.625 秒
```

但这两个缩写我目前把它们视为**队列播放状态字段**，并不是歌曲身份字段。

歌曲本身还是靠：

```text
S_P[78]
```

确定。

---

# 7. 更重要的是，我发现了一个对我们系统很关键的异常

播放队列：

```text
所有 15775 条路径
```

现在全部存在于 `DB_SONGS_LOG`。

但是播放列表不是。

播放列表共有：

```text
5168 个不同路径
```

其中：

```text
5147 个
```

还能在当前 `DB_SONGS_LOG` 找到。

另外有：

```text
21 个旧路径
```

已经不在当前歌曲数据库中。

而且这些旧路径在播放列表里累计出现：

```text
85 次
```

例如其中一个旧播放列表项目是：

```text
file:///storage/emulated/0/存档/临时/timsend/
南野陽子 - 夜の东侧.mp3
```

它已经不在当前：

```text
DB_SONGS_LOG.COL_PATH
```

里面。

但是我根据：

```text
title
artist
duration
```

又在数据库里发现了一首：

```text
南野陽子 - 夜の东侧
```

当前变成了：

```text
content://com.android.externalstorage.documents/.../
音频整理/2025Q2/南野陽子 - 夜の东侧.mp3
```

也就是非常像发生过：

```text
旧：
/存档/临时/timsend/南野陽子 - 夜の东侧.mp3

          ↓ 移动

新：
/音频整理/2025Q2/南野陽子 - 夜の东侧.mp3
```

但某个 `.mpl` 里**依然保留着旧路径**。

这个发现非常重要。

---

# 所以我们之前担心的事情确实存在

Musicolet 当前备份中的逻辑不是：

```text
Song ID = 58372

Playlist
    ↓
58372

Queue
    ↓
58372

Play Count
    ↓
58372
```

而更接近：

```text
             PATH / URI

DB_SONGS_LOG.COL_PATH
        ↑
        │
        ├──── Playlist S_P[]
        │
        ├──── Queue S_P[]
        │
        ├──── Favorites S_P[]
        │
        └──── Play-count DB COL_PATH
```

而且不同模块**甚至可能保存不同年代的路径**。

这一点比“Musicolet 用路径当主键”还要麻烦。

---

# 对我们的镜像系统意味着什么

服务器绝对不能照抄 Musicolet：

```text
songs
PRIMARY KEY (path)
```

否则：

```text
A.mp3
↓ 移动
/JPOP/A.mp3
```

就会变成：

```text
DELETE song A
ADD song B
```

然后连锁毁掉：

```text
播放列表关系
Queue
Favorites
play count
历史数据
服务器文件关联
```

我们必须做：

```text
media_file_id = 100861
```

然后：

```text
media_file_id: 100861

musicolet_path history:
────────────────────────
#1  /临时/A.mp3
#2  /Music/A.mp3
#3  /Music/JPOP/A.mp3

server_path:
/storage/music/100861.mp3
```

所有关系都指向：

```text
100861
```

例如：

```text
playlist_items
    media_file_id = 100861

queue_items
    media_file_id = 100861

favorites
    media_file_id = 100861

play_counts
    media_file_id = 100861
```

而不是指向路径。

---

## 因此，现在可以正式确认一个设计前提

**Musicolet Backup 不提供我们可以直接继承的稳定歌曲编号。**

至少在你这份备份中：

| 数据             | Musicolet 如何标识歌曲          |
| -------------- | ------------------------- |
| `DB_SONGS_LOG` | `COL_PATH`，且是 PRIMARY KEY |
| `.mpl` 播放列表    | `S_P[index]` 路径           |
| `0.qstk` 播放队列  | `S_P[index]` 路径           |
| `0.favs` 收藏    | `S_P[index]` 路径           |
| `PCs_*` 播放次数   | `COL_PATH`                |
| 独立 `song_id`   | **没有发现**                  |

这也说明我们后面设计“第二次及以后导入”时，**歌曲身份解析器会是整个系统最关键的底层模块之一**。

尤其刚才发现的那 21 个“播放列表还保留旧路径、当前歌曲 DB 已经没有这个路径”的实例，非常值得下一步继续研究——因为我们可以反过来利用它们，实际研究 **Musicolet 遇到歌曲移动以后，到底哪些关联会自动迁移，哪些关联不会迁移，以及服务器应该怎么还原这种关系**。这比理论讨论有价值很多。







问：




	你提到以歌曲的元数据作为音频指纹，但是我一定会经常修改某一首歌的meta字段，那么就不可用这个指纹。

	有没有办法抛开歌曲元数据，只对音轨数据采集指纹，以此作为ID依据？

	另外你也提到一首歌曲copy到第二个文件副本，那么这个纯音频轨的指纹也没有意义了？

	你先研究我上面几句话，再考虑怎样出一套精确、靠谱地定位一首歌曲的具体方案，即便其改了元数据、文件名甚至文件目录，甚至copy了一份。

	不过我也考虑到最坏情况下，实在定位不了，那就不搞每次导入musicolet备份zip包时根据数据差异修改服务器数据，不如直接整个备份包load进数据库另一个分区（这个可以称作版本号）以全量加载，反正也不会在服务器保存歌曲文件，工作量应该不大。  如果真这样处理了，还要考虑如果在导入服务器之前，就已经在服务器上修改了播放队列、歌曲清单等数据，就要考虑记录到底改了那些，以便下次新的zip包加载后，提供人工处理这些差异
	
	
	
	


答：






你这里实际上碰到了一个必须严格区分的问题：

> **“这是同一个音频内容” ≠ “这是同一个具体歌曲文件”。**

所以单独找一种“万能指纹”是不够的。我重新考虑后，认为最靠谱的方案应该同时解决**音轨身份**和**文件实例身份**，并且你后面提出的“每次全量建立版本”反而非常适合作为底层架构。

### 一、可以完全抛开 Meta，只对音频本身做指纹

可以，而且应当这么做。

我前面如果把 `title + artist + album + duration` 之类称为“音频指纹”，是不严谨的。它们只能算**辅助匹配特征**，绝不能作为身份依据，因为你会修改 Meta。

可以至少计算三种东西：

| 标识            | 修改 Meta | 改文件名/目录 |     转码 | Copy 文件 |
| ------------- | ------: | ------: | -----: | ------: |
| 整文件 SHA-256   |    ❌ 会变 |    ✅ 不变 |      ❌ |      相同 |
| 解码音轨 PCM Hash |    ✅ 不变 |       ✅ |   通常会变 |      相同 |
| Chromaprint   |    ✅ 不变 |       ✅ | 通常仍可匹配 |      相同 |

FFmpeg 官方的 `hash/streamhash` 就可以对**解码后的音频帧**计算 SHA-256，而且时间戳被忽略。也就是说 ID3、封面、文件名、路径等都不参与。([FFmpeg][1])

例如概念上：

```text
MP3 / FLAC / M4A 文件
        ↓
只解码 Audio Stream
        ↓
固定 PCM 格式
        ↓
SHA-256
```

那么：

```text
A.mp3
Title = ABC
封面 = 1.jpg
```

改成：

```text
南野陽子 - A.mp3
Title = 新标题
封面 = 2.jpg
```

只要真正的音频 samples 没变：

```text
PCM_HASH
```

就不会因为这些 Meta 修改而变化。

而 Chromaprint 更进一步，它直接吃 raw audio stream，设计目标就是识别**近似相同的完整音频**和检测重复音频，而不是读取 Metadata。([acoustid.org][2])

因此即使：

```text
MP3 320k
↓
重新转成 AAC
```

精确 PCM Hash 很可能已经不同，但 Chromaprint 仍可能判断二者属于同一个录音。

不过 Chromaprint 官方自己也强调，它为了搜索效率在精度/鲁棒性上有所取舍，所以我不会把它直接当数据库 PRIMARY KEY，而只把它当很强的身份匹配证据。([GitHub][3])

---

# 二、但是你说 Copy 的问题完全成立

假设：

```text
/Music/A.mp3
```

复制成：

```text
/Backup/A.mp3
```

那么二者：

```text
file SHA256       一样
PCM SHA256        一样
Chromaprint       一样
duration          一样
metadata          一样
```

**没有任何基于文件内容的算法能够把它们区分出来。**

因为内容真的完全相同。

所以不能设计：

```text
PCM_HASH = song_id
```

否则两个 Copy 会被错误合并。

这里必须区分两个 ID：

```text
recording_id
```

表示：

> 这是哪一个音频/录音内容。

以及：

```text
file_instance_id
```

表示：

> 这是手机文件系统里的哪一个具体文件实例。

例如：

```text
recording_id = R10086

file_instance_id = F217
path = /Music/A.mp3

file_instance_id = F891
path = /Backup/A.mp3
```

二者：

```text
recording_id = R10086
```

可以相同。

但：

```text
F217 != F891
```

这样 Musicolet 播放列表如果引用的是 `/Music/A.mp3`，我们就知道它引用的是 `F217`，而不是模糊的“这首录音”。

---

# 三、怎样让 `file_instance_id` 在改名、移动以后仍保持不变？

这是最困难的部分。

服务器第一次看到：

```text
/Music/A.mp3
```

自己生成：

```text
file_instance_id = F217
```

然后客户端额外维护一份**独立于 Musicolet 的本地歌曲身份库**：

```text
F217
current_path
filesystem inode/dev（能取得则记录）
MediaStore ID（能取得则记录）
file_sha256
pcm_sha256
chromaprint
duration/sample_count
first_seen
last_seen
```

假设只是：

```text
/Music/A.mp3
→
/Music/JPOP/A.mp3
```

本地扫描器综合：

```text
旧文件消失
新文件出现

PCM hash 完全一样
Chromaprint 一样
文件大小/音频结构一样

并且 filesystem / MediaStore identity
如果还能连续对应
```

就把：

```text
F217
```

继续给它。

而不是生成 F218。

如果是 Copy：

```text
/Music/A.mp3      仍然存在
/Backup/A.mp3     新出现
```

那么事情反而很好判断：

```text
F217 → 仍在 /Music/A.mp3

/Backup/A.mp3
→ 新文件实例 F218
→ recording_id 同样是 R10086
```

---

## 但存在一种理论上无法自动解决的情况

例如两次扫描之间用户做了：

```text
复制 A.mp3 → 新目录
删除原 A.mp3
```

最后只剩：

```text
/New/A.mp3
```

从两个静态快照来看，它与：

```text
直接移动 A.mp3
```

可能完全没有区别。

也就是说：

```text
旧：
/Old/A.mp3

新：
/New/A.mp3
```

单凭最终状态，**没有任何算法能够知道用户到底执行了：**

```text
MOVE
```

还是：

```text
COPY + DELETE ORIGINAL
```

因为两种操作留下的最终信息完全相同。

这不是算法不够先进，而是**信息已经丢失**。

所以任何号称仅凭两个 Musicolet ZIP 就能 100% 还原这种身份关系的方案都是不可靠的。

只有两种办法解决：

**持续监听文件系统变化**，在操作发生时就知道是 rename/move/copy；或者在无法确定时保留 `AMBIGUOUS`，让你人工确认。

我更倾向第二种原则：

> 可以自动确认的就自动确认；不能证明的绝不擅自合并歌曲身份。

---

# 四、所以我反而很赞成你后面提出的“每次 ZIP 全量建立一个版本”

进一步想过以后，我甚至不把它当“最坏情况下的退路”。

**我建议直接把它定成正式架构。**

因为你的服务器又不保存 6000 多首歌曲的音频文件，Musicolet 的数据库数据本身非常小。

根本没必要为了节约这么一点数据库空间，把整个系统搞成：

```text
Version 1
↓
计算几百项复杂 diff
↓
直接修改唯一数据库
↓
万一 identity resolver 判断错
↓
历史数据永久被污染
```

更安全的是：

```text
Musicolet ZIP #1
      ↓
Snapshot Version 1

Musicolet ZIP #2
      ↓
Snapshot Version 2

Musicolet ZIP #3
      ↓
Snapshot Version 3
```

每个版本都是 Musicolet 当时状态的**完整事实快照**。

例如数据库不一定真的开三个物理 DB，可以只是：

```text
import_versions
---------------
id
backup_sha256
backup_time
raw_zip_path

songs_snapshot
---------------
version_id
source_path
title
artist
...

playlist_snapshot
---------------
version_id
playlist_name
position
source_path

queue_snapshot
---------------
version_id
queue_name
position
source_path

playcount_snapshot
---------------
version_id
source_path
count
```

所有数据都有：

```text
version_id
```

因此：

```text
V1 永远不会被 V2 修改。
V2 永远不会被 V3 修改。
```

---

# 五、Diff 仍然要做，但不再让 Diff 决定数据真相

这是一个很重要的改变。

以前方案：

```text
ZIP2
↓
diff ZIP1
↓
ADD / DELETE / MOVE / UPDATE
↓
修改现有数据库
```

我现在更推荐：

```text
ZIP2
↓
完整建立 Version 2
↓
Version 1 vs Version 2
↓
计算 Diff
```

Diff 只是告诉我们：

```text
发生了什么变化
```

而不是：

```text
靠 Diff 才能生成 Version 2
```

这样即使某一次：

```text
A旧路径
→
A新路径
```

被 identity resolver 错误判断成：

```text
DELETE A
ADD B
```

也没关系。

因为 Version 2 保存的 Musicolet 原始事实没有被破坏。

以后我们改进算法：

```text
Identity Resolver v2
```

重新分析：

```text
V1 ↔ V2
```

就可能得出：

```text
其实是 PATH_CHANGED
```

**不用重新导入数据，更不用修复已经被错误修改的数据库。**

这个架构非常适合你的项目。

---

# 六、你提出的“服务器侧修改”则应该再独立成第三层

这个问题尤其关键。

假设：

```text
8月25日
导入 Musicolet Version 10
```

此时播放列表：

```text
A
B
C
D
```

然后你在镜像网站上修改：

```text
A
C
D
E
```

之后手机 Musicolet 自己又修改，8 月 28 日导入 Version 11：

```text
A
B
D
F
```

绝对不能直接说：

```text
Version 11 覆盖服务器
```

否则服务器上的：

```text
删除 B
加入 E
```

全部丢失。

正确模型应该很像 Git：

```text
                Version 10
                /        \
               /          \
服务器修改                Musicolet修改
A C D E                  A B D F
   \                       /
    \                     /
        三方合并
```

也就是：

```text
BASE
= Version 10

OURS
= Version 10 + 服务器侧修改

THEIRS
= Version 11
```

然后做 **three-way merge（三方合并）**。

---

比如只有服务器新增：

```text
E
```

而 Musicolet 新增：

```text
F
```

两边互不冲突，可以自动得到：

```text
A
B
C
D
E
F
```

具体位置再根据两边 position diff 合并。

但如果：

服务器：

```text
把 C 移到第一位
```

而 Musicolet：

```text
删除 C
```

那就是：

```text
CONFLICT
```

系统弹出来：

> Version 10 → Version 11：Musicolet 删除了 C
> 但服务器侧曾将 C 移动到第一位。
>
> 保留 C / 接受删除 / 手工处理

这正是你说的：

> “提供人工处理这些差异。”

---

# 七、服务器修改不要直接写进 Musicolet Snapshot

这是我最建议坚持的地方。

不要：

```text
Version 10
→ 用户在网站改
→ 直接 UPDATE Version 10
```

而应该：

```text
Version 10                  ← 永远只读

local_changes
────────────────────────────
base_version = 10
entity = playlist:xxx
operation = REMOVE
song = xxx

base_version = 10
entity = playlist:xxx
operation = ADD
song = xxx
position = 17
```

也就是说服务器自己的操作另外保存成：

```text
Overlay / Change Log
```

最终你平时看到的是：

```text
Musicolet Version 10
        +
Server Overlay
        =
Current View
```

等 Version 11 来了：

```text
Version 10
Version 11
Server Overlay since V10
        ↓
三方合并
        ↓
新的 Server Overlay based on V11
```

这样数据来源永远不会混。

---

# 最终我建议的整个数据架构

我现在会定成四层：

```text
① RAW SOURCE
────────────────────────
Musicolet 原始 ZIP
永不修改


② MUSICOLET SNAPSHOT
────────────────────────
Version 1
Version 2
Version 3
...

每一次都是完整全量解析
永不修改


③ IDENTITY MAP
────────────────────────
recording_id
file_instance_id

file SHA256
PCM SHA256
Chromaprint
历史 path
匹配置信度
人工确认结果


④ SERVER OVERLAY
────────────────────────
服务器上的 Playlist 修改
Queue 修改
收藏修改
其它用户操作
```

然后：

```text
当前实际视图
=
最新 Musicolet Snapshot
+
Identity Resolution
+
Server Overlay
```

我认为这比“必须每次完美判断 ZIP 差异，然后增量修改唯一数据库”可靠得多。

尤其因为**服务器根本不存歌曲音频文件**，每次把 6000 多歌曲、50 多播放列表、十几个 Queue 全量重新入一个 `version_id`，数据库成本其实非常低。

而歌曲 identity resolver 可以逐步完善，它**不再成为导入成功与否的前置条件**。

即使某一天出现：

```text
这首旧路径歌曲
到底对应新版本里的 A 还是 B？
```

也完全可以：

```text
Version 17：原样保存
Version 18：原样保存

Identity：
UNRESOLVED
```

等你以后人工确认：

```text
V17/F123
=
V18/F891
```

再建立跨版本 identity linkage。

**这才是我目前认为最精确、最不容易把多年 Musicolet 数据搞坏的方案。**

而如果以后你愿意在手机端增加一个很轻量的扫描器，那就可以在不上传歌曲文件的前提下，只上传 `PCM SHA-256 + Chromaprint + 文件实例辅助信息`，进一步大幅提高跨版本歌曲身份匹配率。([FFmpeg][1])

[1]: https://www.ffmpeg.org/ffmpeg-formats.html?utm_source=chatgpt.com "FFmpeg Formats Documentation"
[2]: https://acoustid.org/chromaprint?utm_source=chatgpt.com "Chromaprint | AcoustID"
[3]: https://github.com/acoustid/chromaprint/blob/master/README.md?plain=1&utm_source=chatgpt.com "chromaprint/README.md at master · acoustid/chromaprint · GitHub"







问：




	可以考虑按照musicolet备份数据所指定的全部歌曲列表来扫描手机并上传PCM SHA-256 文件实例辅助信息，不考虑Chromaprint，因为我手机里存在音轨极度相似的多首歌曲，怕被错误识别为同一个歌曲。

	即便已经有识别唯一歌曲的能力了，也只是把这些标记ID的数据存到全局数据库里仅作参考，目前暂时不参与musicolet数据导入工作。

	你还提到用git merge甚至git resolve处理导入前已在服务器的changes跟最新版musicolet备份数据之冲突，这个方案可以考虑，而且最好是直接使用具备git完全能力的sdk来做这件事。

	那么导入musicolet最新备份数据时，应该这么处理：先创建一个数据导入过程（procedure），提示最新版本有哪些数据变动（增、删、改、移动等等，甚至对于播放记录的变化也有），如果存在冲突，就要列出所有冲突待解决，用户可以选择解决方式（以旧版改动为准、以新版改动为准、手动精准以自己的决定修改数据），用户不必立即全部处理完，可以暂存该过程（procedure）。用户可以下次再进来这个导入过程（procedure）处理冲突，直到所有冲突解决完毕，最后点击了该过程（procedure）的提交按钮，这才开始真正将新版musicolet导入到数据库，并且也正式将用户在该过程（procedure）对于冲突处理的细节应用到数据库。
	由于可以暂存对于数据的导入过程（procedure），用户却可能会在正式提交导入过程（procedure）之前，又对旧版的数据库产生改动（后文将“对于某处数据的改动动作”简称为M），那么就要求在下次进入这个导入过程时，要重新load一遍冲突列表，原则上，某个M改动如果命中用户在该过程（procedure）中标记处理过的冲突，那么就要以醒目的UI交互方式提醒用户“这里在旧版又有改动，而且还存在用户已经冲突处理决定意见”。






答：






这个方案我赞成，而且你现在已经把架构从“同步 Musicolet 数据”推进到了更准确的模型：

**Musicolet 每次备份是一个不可变的上游版本；服务器上的修改是另一条持续发展的分支；导入 procedure 本质上就是一次可暂停、可恢复、带人工 resolve 的三方合并。**

有几个地方我建议正式定死。

### 1. PCM SHA-256 可以做，但只进入“全局身份参考库”

你现在这个定位比较稳：

```text
Musicolet Backup
       ↓
得到备份中出现的全部歌曲路径
       ↓
手机端扫描对应文件
       ↓
上传：
- PCM SHA-256
- 文件实例辅助信息
       ↓
Global Song Identity DB
```

**暂时不让它参与 Musicolet import 的判断。**

PCM Hash 应只针对解码后的音频流，不包含：

* ID3 / Vorbis Comment 等 Meta
* 封面
* 文件名
* 目录
* 容器 Metadata

最好把输入规范固定，例如：

```text
第一音频流
→ 解码
→ 固定 PCM sample format
→ 不改变采样率
→ 不改变声道数
→ SHA-256
```

这样改 Title、Artist、Album、封面、文件名、目录都不会改变 `pcm_sha256`。

它比 Chromaprint 更符合你的用途，因为你明确不想做 fuzzy matching。Chromaprint 的目标本身就是在一定差异下仍认为是相同录音，而你存在大量极度相似歌曲，宁可“不认出来”，也不能“错认”。FFmpeg 本身支持对解码音视频数据计算哈希，而且时间戳不参与哈希。([Libgit2][1])

但 Copy 问题仍然必须保留：

```text
/Music/A.mp3
/Music/Copy/A.mp3
```

如果是逐字节复制：

```text
PCM_SHA256 相同
```

这是正确结果——它说明：

> 两个文件拥有相同音轨内容。

不是：

> 两个文件是同一个文件实例。

所以全局参考库还是应该至少有：

```text
recording_identity
    pcm_sha256

file_instance
    local_instance_id
    current_path
    path_history
    whole_file_sha256
    pcm_sha256
    file_size
    mtime
    MediaStore ID
    first_seen
    last_seen
```

其中 `MediaStore ID`、路径、mtime 都只能算辅助证据，不应单独承担永久 ID。

如果一个 Copy 同时存在：

```text
F001 → /Music/A.mp3
F002 → /Backup/A.mp3

F001.pcm_sha256 == F002.pcm_sha256
```

完全允许。

而且既然目前不参与导入，它甚至判断错了都不会污染 Musicolet 数据。

---

# 2. Musicolet Import 不再做“增量覆盖”，而是完整候选版本

这一点我建议正式改成：

```text
最新正式版本：Musicolet V17

上传新 ZIP
       ↓
创建 Import Procedure P42
       ↓
完整解析 ZIP
       ↓
生成 Candidate Snapshot
       ↓
暂时不是 V18
```

在 Procedure 真正点击“提交”以前，它都只是：

```text
candidate
```

不要提前给它正式的：

```text
V18
```

否则一个 procedure 被放弃以后版本号会出现很多洞。

最终 Commit 时才：

```text
Candidate → Musicolet V18
```

并永久保存：

```text
原始 ZIP
解密结果（可选）
标准化 Snapshot
```

---

# 3. “旧版”本身不能被修改

这里术语上我建议稍微修正一下。

例如已经正式导入：

```text
Musicolet V17
```

那么 V17 永远不可变。

你后面在网站上做的 M：

```text
播放列表加入歌曲
删除歌曲
移动歌曲
改 Queue
改收藏
……
```

不是：

> 修改 V17

而是：

```text
V17
 │
 ├─ M1
 ├─ M2
 ├─ M3
 └─ M4
      ↓
Current Server State
```

这样才真正符合 Git 模型。

---

# 4. Git 拓扑可以非常漂亮地表达这个关系

我建议实际上就维护两条正式历史。

```text
musicolet-source
```

只放 Musicolet 正式导入版本：

```text
S1 ─── S2 ─── S3 ─── S4
```

另一条：

```text
main
```

表示服务器当前真实工作状态：

```text
S1
 \
  M1 ─ M2 ─ M3
             \
              Merge S2
                 \
                  M4 ─ M5
                         \
                          Merge S3
```

例如现在：

```text
musicolet-source HEAD = S17
main HEAD = O17
```

其中 O17 已经包含：

```text
S17
+
服务器 M1 M2 M3...
```

来了新 ZIP以后，Procedure 创建：

```text
procedure/P42/incoming
```

得到：

```text
S18-candidate
```

而且这个 commit 的 parent 是：

```text
S17
```

所以天然形成：

```text
                  O17
                 /
                /
              S17
                \
                 \
                S18-candidate
```

这就是标准三方 Merge：

```text
BASE   = S17
OURS   = O17
THEIRS = S18-candidate
```

完全符合你要求的：

> 旧版服务器上的改动 vs 最新 Musicolet 数据。

---

# 5. Procedure 创建后首先展示两类变化

第一部分：

## Musicolet 本身发生了什么

也就是：

```text
BASE → THEIRS
S17  → S18
```

这里应该完整显示：

```text
歌曲数据
  + 新增
  - 删除
  ~ 修改

播放列表
  + 创建
  - 删除
  ~ 改名/属性修改
  + 歌曲加入
  - 歌曲移除
  ↕ 顺序移动

Queue
  + / - / move
  当前歌曲变化
  当前播放位置变化

Favorites
  + / -

播放统计
  总播放次数 +N
  年播放次数变化
  月播放次数变化
  周播放次数变化
  Last Played 变化

Settings
  ...
```

播放次数尤其不要伪装成具体播放事件。

例如只能说：

```text
南野陽子 - xxx

总播放次数
147 → 150
Δ +3

Last Played
2026-08-17 19:51
→
2026-08-25 21:13
```

不能凭空生成另外两个播放时间。

---

第二部分则是：

## Server 从 BASE 以后做了什么

```text
BASE → OURS
S17 → O17
```

例如：

```text
播放列表 X
服务器：
+ A
- B
move C 12 → 3
```

然后：

```text
BASE → THEIRS
```

又出现：

```text
Musicolet：
- C
+ D
```

语义合并器再判断是否冲突。

---

# 6. Git SDK 可以用，但绝不能直接依赖 Git 的文本 Merge

这一点非常重要。

libgit2 确实提供完整的：

* merge base
* merge commits
* merge trees
* conflict index
* ours
* theirs
* rename detection
* 自定义 merge driver

等能力。([GitHub][2])

而且 libgit2 本来就是为了“把 Git 能力作为库嵌入应用程序”设计的，并提供多语言 binding。([GitHub][3])

如果后端最终使用 Go，那么官方列出的 Go binding 是 `git2go`。([GitHub][3])

反过来，我不建议用 `go-git` 承担这一核心层，因为它当前自己的兼容表仍然明确标记：

```text
merge: partial
Fast-forward only
```

也没有完整 mergetool。([GitHub][4])

不过：

**“歌曲在播放列表里从第 81 位移到第 3 位”不是 Git 所理解的 rename。**

Git 默认只看到 JSON 文本发生变化。

因此应该：

```text
Git
负责：
历史图
Commit
Branch
Merge base
Ours/Theirs
procedure branch
最终 merge commit
```

而：

```text
Musicolet Semantic Merge Engine
负责：
playlist ADD/REMOVE/MOVE
queue ADD/REMOVE/MOVE
play-count delta
favorite change
歌曲属性
业务冲突
```

libgit2 本身允许注册 custom merge driver，所以两层可以很好结合。([Libgit2][5])

---

# 7. Procedure 本身一定要是持久化实体

不是一个临时页面 Session。

例如：

```text
import_procedures

id                    P42
status                resolving
base_source_commit    S17
incoming_commit       S18_candidate
last_analyzed_ours    O17
source_zip_sha256
created_at
updated_at
```

状态大概：

```text
PARSING
↓
REVIEWING
↓
RESOLVING
↓
READY_TO_COMMIT
↓
COMMITTED
```

另外可以有：

```text
ABORTED
FAILED
```

用户什么时候退出都没有关系。

下次进去继续。

---

# 8. 每一个冲突 resolution 不能只保存“我选了旧版”

这是你这次提出的问题里最关键的一点。

错误设计：

```text
Conflict #17
resolution = OURS
```

这不够。

因为用户选择 OURS 以后，又发生 M。

必须保存：

```text
Conflict #17

base_value_hash
ours_value_hash
theirs_value_hash

resolution:
    OURS

resolved_against_ours_commit:
    abc123

dependency_scope:
    playlist/xxx
```

也就是：

> 这个决定，是基于当时哪一个 OURS 作出的。

---

# 9. Procedure 暂存期间继续产生 M 怎么处理

假定处理 Procedure 时：

```text
last_analyzed_ours = O17
```

用户解决：

```text
Conflict C1
→ 采用 OURS
```

然后退出。

之后服务器发生：

```text
M20
```

于是：

```text
main HEAD
O17 → O18
```

下一次进入 P42 时，首先发现：

```text
current HEAD != last_analyzed_ours
```

那么 Procedure 自动：

```text
重新计算

BASE   S17
OURS   O18
THEIRS S18
```

这就是你说的：

> 重新 load 一遍冲突列表。

完全正确。

---

# 10. 但并不是所有已经处理的冲突都要推翻

例如之前处理：

```text
播放列表 A 的 conflict
```

后来 M 修改的是：

```text
播放列表 B
```

那么 C1 完全不用动。

所以每个 Resolution 应有一个：

```text
dependency set
```

比如：

```text
playlist:A
```

如果新的 M 没碰它：

```text
Resolution 保留
```

如果 M 命中它：

```text
Resolution → STALE
```

这就是你要求的醒目提示。

---

# 11. UI 上我建议明确出现一个“已过期的决定”

例如原来：

```text
⚠ 冲突
播放列表「车播：常规」

Musicolet 新版：
删除歌曲 A

服务器：
将歌曲 A 移到第 3 位
```

用户之前决定：

```text
✓ 保留服务器版本
```

后来 M 又：

```text
把 A 从第 3 位移到第 8 位
```

下次打开 Procedure：

```text
🔴 已处理冲突发生新的服务器改动

你之前的决定：
采用服务器版本

当时服务器状态：
A → #3

当前服务器状态：
A → #8

Musicolet 新版：
删除 A
```

然后：

```text
[维持采用当前服务器版本]
[采用 Musicolet 新版]
[手动处理]
```

我甚至不建议偷偷沿用：

```text
OURS
```

虽然“采用 OURS”逻辑上似乎仍然可以解释为“采用最新 OURS”。

因为用户当时实际上看到的是：

```text
A 在 #3
```

而不是：

```text
A 在 #8
```

因此必须重新提示。

这就是所谓：

```text
stale resolution
```

---

# 12. “命中”要做语义判断，而不是只判断同一个 JSON 文件

比如：

```text
Conflict：
Playlist X / song A / position
```

后来 M：

```text
Playlist X / song B / title
```

不一定影响它。

但是对于**有序列表**，需要更加保守。

因为：

```text
A 从 10 → 5
```

后来：

```text
删除了第 2 位 B
```

A 的绝对位置已经变成：

```text
9 → 4
```

所以 Playlist / Queue 的结构操作：

```text
ADD
REMOVE
MOVE
```

最好把依赖范围扩到：

```text
整个 ordered container
```

也就是说：

> 如果同一个播放列表发生新的结构修改，那么这个列表里以前解决过的 MOVE/ADD/REMOVE conflict 至少全部标记“需要重新检查”。

这虽然稍微保守，但不会错。

对于普通 scalar：

```text
title
artist
playcount
lastplayed
```

则可以精确到字段。

---

# 13. Procedure 的 conflict 应该保存三份值

比如：

```text
BASE
OURS
THEIRS
```

而不是只有：

```text
old
new
```

手动解决以后，再有第四份：

```text
RESOLVED
```

例如：

```text
BASE
[A,B,C,D]

OURS
[A,C,D,E]

THEIRS
[A,B,D,F]

RESOLVED
[A,C,D,E,F]
```

这样以后才能准确知道：

> 用户当时到底作了什么决定。

---

# 14. “手动精准决定”也必须记录成正式 Patch

不能只存最后结果。

最好存：

```text
manual_resolution

REMOVE B
KEEP C
ADD E after D
ADD F after E
```

然后同时保存最终值。

因为以后审计时：

```text
为什么这个 Playlist 最后变成这样？
```

我们能明确回答：

```text
Musicolet：
- C
+ F

Server：
- B
+ E

用户解决 conflict：
保留 C
接受 F
```

而不是只有最终数组。

---

# 15. Procedure Commit 前必须再做一次最终 revalidation

这是防止最后一个竞态条件。

假定：

```text
22:00
用户已经处理完全部冲突
```

与此同时另一个页面：

```text
22:01
产生 M100
```

用户：

```text
22:02
点击 Commit
```

如果直接提交，就错了。

所以 Commit 实际应该是：

```text
BEGIN COMMIT

expected_main_head
==
current_main_head ?
```

如果不等：

```text
拒绝提交
↓
重新刷新冲突
↓
提示：
“服务器数据在你处理期间又发生了变化”
```

只有完全相等才真正 Commit。

可以用数据库 transaction + Git ref CAS/锁完成。

---

# 16. 最终 Commit 时发生的事情

当所有冲突：

```text
RESOLVED
```

并且 HEAD 没有变化：

```text
S17 = BASE
O18 = OURS
S18 = THEIRS
```

根据用户所有 resolution 得到：

```text
R18 = RESOLVED TREE
```

然后一次原子过程：

```text
① Candidate 正式成为 Musicolet V18

② musicolet-source:
   S17 → S18

③ main:
       O18
        \ 
         R18
        /
      S18

④ 创建标准 Git merge commit

⑤ 把 V18 完整 Snapshot 写入不可变 version partition

⑥ 当前业务数据库更新为 R18

⑦ 计算：
   V18 → R18
   得到新的 Server Overlay

⑧ Procedure → COMMITTED
```

这一步很漂亮：

以前 Server 的 M 并没有消失。

只是从：

```text
V17 + Server Changes
```

重新表达成：

```text
V18 + New Server Overlay
```

---

# 17. 这也解决你最开始那个歌曲 ID 问题

因为现在正式 Import 完全不依赖：

```text
PCM SHA256
recording_id
file_instance_id
```

Musicolet V17 怎么描述，就是 V17。

Musicolet V18 怎么描述，就是 V18。

例如：

```text
V17:
/old/A.mp3

V18:
/new/A.mp3
```

现阶段 source snapshot 就忠实保存：

```text
DELETE /old/A.mp3
ADD /new/A.mp3
```

与此同时 Global Identity DB 可以在旁边说：

```text
参考信息：

F187
V17 /old/A.mp3
V18 /new/A.mp3

PCM_SHA256 相同
很可能是相同音轨
```

但是：

**绝不偷偷改变 Import 语义。**

以后等你对歌曲 identity 系统足够有信心，再决定是否让它成为 Semantic Merge Engine 的一个辅助因素。

我非常赞成这个渐进路线。

---

## 所以我现在建议正式确定为这个模型

```text
                    Musicolet ZIP
                         │
                         ▼
                Import Procedure
                         │
            ┌────────────┴────────────┐
            ▼                         ▼
       Candidate Snapshot       RAW ZIP Archive
            │
            ▼
       Semantic Diff
            │
            ▼
       Git Three-way Merge
            │
      BASE / OURS / THEIRS
            │
            ▼
        Conflict List
            │
       ┌────┴────┐
       │         │
     resolve    暂存
       │         │
       │      Server继续M
       │         │
       └────► Refresh
                 │
          stale resolutions
                 │
                 ▼
          All Resolved
                 │
                 ▼
          Final Revalidate
                 │
                 ▼
              COMMIT
                 │
       ┌─────────┴─────────┐
       ▼                   ▼
Musicolet Version N+1    Main Merge Commit
```

这个方案里，**Git 负责版本历史与三方合并框架，业务层负责理解“播放列表移动”“Queue 顺序”“播放统计变化”这些 Git 本身不认识的语义。**

这样既真正用了 Git 的能力，又没有错误地期待 Git 自己理解 Musicolet 数据。

[1]: https://libgit2.org/?utm_source=chatgpt.com "libgit2"
[2]: https://github.com/libgit2/libgit2/blob/main/include/git2/merge.h?utm_source=chatgpt.com "libgit2/include/git2/merge.h at main · libgit2/libgit2 · GitHub"
[3]: https://github.com/libgit2/libgit2?utm_source=chatgpt.com "GitHub - libgit2/libgit2: A cross-platform, linkable library implementation of Git that you can use in your application. · GitHub"
[4]: https://github.com/go-git/go-git/blob/main/COMPATIBILITY.md?utm_source=chatgpt.com "go-git/COMPATIBILITY.md at main · go-git/go-git · GitHub"
[5]: https://libgit2.org/docs/reference/main/sys/merge/git_merge_driver.html?utm_source=chatgpt.com "git_merge_driver (libgit2 main)"






