# Musicolet 镜像系统技术需求文档

## 1. 项目定位

本系统用于近乎完整地复刻 Musicolet 的核心播放器能力、数据组织方式与主要 UI 布局，并在此基础上增加：

* Musicolet Backup ZIP 全量导入；
* Musicolet 数据版本化；
* 服务器侧独立修改；
* Git 版本控制；
* 三方合并与冲突处理；
* 可暂存、可恢复的 Import Procedure；
* PCM SHA-256 音轨信息参照库；
* Android/Termux 手机侧只读数据执行器；
* 按需歌曲缓存；
* Web 分享；
* 本系统独立备份/还原。

系统不是 Musicolet App 数据库的简单查看器，而是一个可以长期演进的、以 Musicolet 数据为上游来源之一的 Web 音乐数据库与播放器。

UI、信息层级和主要交互应尽可能接近 Musicolet，但所有实现应独立完成，不依赖 Musicolet 私有代码或资源。

---

# 2. 核心设计原则

## 2.1 Musicolet 原始数据不可变

每一次 Musicolet Backup ZIP 都是一次独立的上游事实来源。

每次正式导入后形成：

```text
Musicolet V1
Musicolet V2
Musicolet V3
...
```

每个版本均为完整 Snapshot，不通过“修改上一版数据库”生成下一版。

必须永久保存：

```text
原始加密 ZIP
解密/解析结果
标准化 Snapshot
版本信息
导入记录
```

旧 Musicolet Version 永远不可修改。

---

## 2.2 Musicolet Version 与服务器当前状态永久分离

例如：

```text
Musicolet V18:

Playlist X
A B C
```

服务器上随后修改：

```text
A C D
```

则系统必须同时保留：

```text
① Musicolet V18 原始状态
A B C

② 当前服务器状态
A C D

③ Server Changes
REMOVE B
ADD D
```

三者均永久保存。

服务器当前状态绝不能反向污染 Musicolet V18 Snapshot。

---

## 2.3 Server Change 是正式数据

以下服务器操作均属于 M（Modification）：

```text
歌曲 Metadata 修改
歌曲删除
Playlist 增删歌曲
Playlist 调整顺序
Queue 增删歌曲
Queue 调整顺序
Favorite 修改
播放行为
其他服务器业务数据修改
```

Server Change：

* 必须有 Git 历史；
* 必须持久保存；
* 对应歌曲/Playlist/Queue 等对象应具有“存在服务器修改”的显式状态标记；
* 新 Musicolet ZIP 未真正修改对应数据时，不能覆盖 Server Change。

---

# 3. 明确排除的错误模型

以下模型禁止引入。

## 3.1 禁止建立 recording_id / file_instance_id 双层身份模型

系统不定义：

```text
recording_id
file_instance_id
```

也不定义：

> “一首歌曲对应多个文件实例”。

不同路径就是两个独立歌曲数据块。

例如：

```text
/Music/A.mp3
/Backup/A.mp3
```

即使：

```text
PCM_SHA256 完全相同
```

仍然是两首不同歌曲。

它们可能分别：

* 位于不同 Playlist；
* 位于不同 Queue；
* 有不同播放次数；
* 有不同收藏状态；
* 有不同 Metadata。

不得自动合并。

---

## 3.2 file_id 仅为内部技术编号

可以建立：

```text
file_id
```

作为服务器内部记录编号。

但 `file_id`：

* 不承担跨 Musicolet Version 的永久歌曲身份；
* 不作为 Musicolet 数据的业务关联依据；
* 不要求路径改变后保持原值；
* 不参与判断 MOVE。

例如：

```text
V17:
/Music/A.mp3
file_id=123
```

V18：

```text
/Music/A.mp3 删除
/Music/JPOP/A.mp3 新增
```

则完全可以：

```text
旧 file_id=123 删除
新路径产生 file_id=456
```

不尝试把 456 重新认定成 123。

---

# 4. Musicolet 原始歌曲定位模型

已解析的实际 Musicolet Backup 表明：

```text
DB_SONGS_LOG.TABLE_SONGS.COL_PATH
```

以路径/URI作为主键。

Playlist `.mpl` 中歌曲通过：

```text
S_P[index]
```

保存路径。

Queue `0.qstk` 同样通过：

```text
S_P[index]
```

保存路径。

Play-count 数据库同样以：

```text
COL_PATH
```

作为歌曲定位依据。

因此服务器在解析某一 Musicolet Snapshot 时，应忠实保留这种数据关系。

---

# 5. Song Core

用于 Musicolet 三方冲突判断时，将歌曲本身定义为一个整体 Song Core。

Song Core 至少包括：

```text
文件路径
Title
Artist
Album
Album Artist
Composer
Genre
Lyrics
Track No.
Disc No.
Year
Comment
Duration
以及其他歌曲本体 Metadata
```

## 5.1 Song Core 是整体冲突单元

不得把 Song Core 拆到字段级进行自动 merge。

例如：

```text
BASE:
Title=A
Artist=X

SERVER:
Title=B
Artist=X

NEW MUSICOLET:
Title=A
Artist=Y
```

虽然服务器改 Title、手机改 Artist，但：

```text
SERVER 修改了 Song Core
NEW MUSICOLET 也修改了 Song Core
```

因此视为冲突。

不得自动生成：

```text
Title=B
Artist=Y
```

必须进入 Conflict Resolution。

---

# 6. 路径变化处理原则

不尝试识别 MOVE。

例如：

```text
V17:
/Music/A.mp3
```

V18：

```text
/Music/A.mp3 消失

新增：
/Music/JPOP/A.mp3
```

直接理解为：

```text
DELETE /Music/A.mp3
ADD /Music/JPOP/A.mp3
```

如果 Musicolet App 本身正确迁移了：

* Queue；
* Playlist；
* Play-count；

那么这些关系在新 ZIP 中自然体现为：

```text
原位置删除旧路径
原位置加入新路径
```

服务器只忠实处理数据变化。

PCM SHA-256 不参与这个判断。

---

# 7. PCM SHA-256 参照库

## 7.1 定位

PCM SHA-256 是：

> 音轨内容信息参照库。

不是：

* song_id；
* file_id；
* Musicolet import identity；
* Playlist/Queue business key；
* 自动合并依据。

---

## 7.2 数据组织

数据主体仍是具体文件路径。

例如：

```text
/Music/A.mp3

scanner_ffmpeg_v1:
PCM_SHA256 = AAA

scanner_ffmpeg_v2:
PCM_SHA256 = BBB
```

如果未来不同 FFmpeg/扫描规则算出不同值，允许同时记录。

这表示：

> 不同扫描方法曾对这个具体文件得到这些 PCM 特征。

不产生新歌曲身份。

---

## 7.3 PCM 相同不意味着歌曲相同

例如：

```text
/Music/A.mp3
/Music/A-copy.mp3
```

即使：

```text
PCM_SHA256 相同
```

也必须完全割裂为两个独立歌曲数据块。

PCM 参照库适合回答：

```text
这个 PCM 音轨特征曾出现在哪些歌曲信息中？
```

例如：

```text
PCM abc...

版本/观察 1
路径=/Music/A.mp3
Title=A
Artist=X

版本/观察 2
路径=/Music2/A.mp3
Title=B
Artist=Y
```

但不自动建立二者业务关系。

---

## 7.4 不使用 Chromaprint

系统明确不采用 Chromaprint 作为歌曲识别方案。

原因是曲库中可能存在音轨极度相似但实际上需要严格区分的歌曲。

本项目采取：

> 宁可不进行模糊识别，也不错误合并歌曲。

---

# 8. 手机侧 PCM 扫描范围

PCM 扫描范围：

```text
Musicolet → All songs
```

所列的全部歌曲。

不建立额外复杂的“文件实例发现系统”。

Musicolet 导出的 All songs 即扫描任务来源。

---

# 9. Git 版本控制架构

Git 用于：

* Server Change 历史；
* Musicolet source history；
* 三方 Merge 基础；
* Merge Base；
* Commit；
* Conflict 状态；
* Resolution 审计；
* Procedure 最终 Merge Commit。

推荐：

```text
libgit2
+
git2go
```

libgit2 本身是可嵌入应用程序的 Git 核心库，提供 commit/tree merge 和冲突 index 等能力；Go 可通过 git2go 使用。当前 go-git 官方兼容表仍将普通 merge 标记为 partial，主要支持 Fast-forward，因此不能承担本系统核心三方 merge 引擎。

Git 只负责：

```text
版本图
commit
tree
merge-base
branch/ref
ours/theirs
conflict index
最终 merge commit
```

Git 不负责理解：

```text
Playlist MOVE
Queue MOVE
播放次数 delta
歌曲整体冲突
服务器删除遮蔽
```

这些由：

```text
Musicolet Semantic Merge Engine
```

负责。

---

# 10. Import Procedure

“从 Musicolet 备份数据导入”不是立即导入，而是创建：

```text
Import Procedure
```

---

## 10.1 同一时间只能有一个未结束 Procedure

只要当前存在：

```text
PARSING
REVIEWING
RESOLVING
READY
```

状态的 Procedure：

**禁止上传新的 Musicolet Backup ZIP。**

必须先：

```text
提交当前 Procedure
```

或者：

```text
取消当前 Procedure
```

之后才能上传下一份 ZIP。

---

## 10.2 Procedure 中 ZIP 固定

一个 Procedure 对应一份确定的 ZIP。

不允许：

```text
P42 原来处理 A.zip
↓
中途替换成 B.zip
```

---

## 10.3 取消 Procedure

取消后：

```text
status = CANCELLED
```

但以下数据不删除：

```text
原始 ZIP
解析结果
Candidate Snapshot
Diff
Conflict
已经完成的 Resolution
Procedure 元数据
```

ZIP 保存在：

```text
data/
```

目录下合适的持久化子目录，并建立 Procedure 关联。

取消后的 Candidate 不成为正式 Musicolet Version。

---

# 11. Procedure Diff

Procedure 必须同时提供两类 Diff。

## 11.1 Semantic Diff

默认主要界面显示业务语义：

```text
歌曲：
ADD
DELETE
MODIFY

Playlist：
CREATE
DELETE
ADD SONG
REMOVE SONG
MOVE SONG

Queue：
CREATE
DELETE
ADD
REMOVE
MOVE

Favorites：
ADD
REMOVE

Playback statistics：
播放次数变化
月统计变化
周统计变化
年统计变化
Last Played 变化

Settings：
变化项
```

---

## 11.2 Musicolet 原始结构字符 Diff

同时允许查看：

> 新旧 Musicolet 解密后 ZIP 的字符级差异。

对于文本：

```text
JSON
.mpl
配置文件
```

直接生成稳定文本 Diff。

对于：

```text
SQLite
Java Serialization
其他二进制结构
```

必须先解析成 canonical text。

例如 SQLite：

```text
SQLite
↓
固定 Schema 顺序
固定 Table 顺序
固定 Row 排序
固定字段顺序
↓
Canonical Text Dump
↓
字符/行级 Diff
```

例如：

```diff
TABLE_SONGS /Music/A.mp3

-COL_NUM_PLAYED=100
+COL_NUM_PLAYED=103
```

原始结构 Diff 与业务 Semantic Diff 是两个独立视图。

---

# 12. 三方合并基本模型

每个 Procedure：

```text
BASE
=
上一次正式 Musicolet Version

OURS
=
当前服务器工作状态

THEIRS
=
本次新 Musicolet Candidate Snapshot
```

---

## 12.1 最核心规则

如果：

```text
THEIRS == BASE
OURS != BASE
```

说明：

> Musicolet 手机端根本没有修改该数据。

因此：

```text
保留 OURS
```

不产生冲突。

这是整个系统最关键的 Merge 原则之一。

---

## 12.2 真正冲突

当：

```text
OURS != BASE
THEIRS != BASE
OURS != THEIRS
```

说明双方都修改了同一业务数据。

进入：

```text
Conflict Resolution
```

---

## 12.3 双方做了相同修改

如果：

```text
OURS == THEIRS
```

则不产生冲突。

---

# 13. Conflict Resolution

每项冲突支持：

```text
保留服务器当前改动
采用新版 Musicolet
手动处理
```

不要使用容易混淆的：

```text
“以旧版为准”
```

UI 应明确写：

```text
保留服务器当前改动
采用新版 Musicolet
手动处理
```

---

## 13.1 手动处理

用户可以进入对应完整对象编辑器。

不限于 OURS/THEIRS 二选一。

例如：

```text
BASE:
A B C D

OURS:
A C D E

THEIRS:
A B D F
```

用户可以手工生成：

```text
C A F D E
```

并保存：

```text
最终状态
+
详细 Resolution Patch
```

例如：

```text
REMOVE B
KEEP C
MOVE C
ADD F
ADD E
```

---

# 14. Procedure 暂存后发生新的 Server M

Procedure 可以长期暂存。

但用户可能在 Procedure 尚未提交时，在系统其他页面继续产生 M。

因此 Procedure 必须记录：

```text
last_analyzed_server_head
```

下次打开时：

```text
current_server_head
!=
last_analyzed_server_head
```

则必须重新计算冲突。

---

## 14.1 已解决冲突被新的 M 命中

如果某个新 M 命中了用户之前已经处理的 Conflict：

该 Resolution 立即：

```text
STALE
```

不能自动继续生效。

UI 必须醒目显示：

> 该数据在你作出冲突处理决定后，又发生了新的服务器修改。

并同时显示：

```text
之前处理时的服务器状态
之前的 Resolution
当前服务器状态
新版 Musicolet 状态
```

要求用户重新确认。

---

# 15. Procedure 最终提交

Procedure Commit 必须为严格原子事务：

```text
全成功
或者
全失败
```

提交前再次检查：

```text
current_server_head
==
procedure_last_validated_head
```

如果不一致：

```text
拒绝提交
重新刷新 Diff / Conflict
```

不得带着过期 Resolution 强制提交。

提交成功时一次完成：

```text
Candidate → 正式 Musicolet V(n+1)

保存 Snapshot

推进 Musicolet source history

应用所有 Resolution

生成服务器新工作状态

生成 Git merge commit

更新 Server Change 状态

Procedure → COMMITTED
```

任一步失败：

```text
全部回滚
```

---

# 16. Song Core 删除冲突规则

播放次数不属于 Song Core。

属于：

```text
外挂数据
```

因此：

```text
BASE:
歌曲存在

SERVER:
永久删除歌曲

NEW MUSICOLET:
Song Core 完全没改
只是播放次数增加
```

结果：

```text
继续保持服务器删除
```

不产生冲突。

只有新 Musicolet 修改：

```text
歌曲 Metadata
或
同路径 Song Core
```

才可能与服务器删除构成冲突。

---

## 16.1 删除遮蔽

如果服务器删除：

```text
/Music/A.mp3
```

后续 Musicolet V18、V19、V20 中：

```text
/Music/A.mp3
```

Song Core 始终没变，则服务器 DELETE 持续有效。

但如果以后 Musicolet 变成：

```text
/Music/JPOP/A.mp3
```

则这是：

```text
旧路径 DELETE
新路径 ADD
```

新路径不继承旧路径的 Server DELETE。

---

# 17. Playback Count Merge

播放统计不使用普通 OURS/THEIRS 覆盖。

对于第 n 次导入：

```text
base_n
=
上一次成功导入的 Musicolet 数值

server_n
=
当前服务器综合数值

previous_resolve
=
上一次导入完成后的综合数值

server_change
=
server_n - previous_resolve

import_n
=
新 Musicolet ZIP 数值

musicolet_change
=
import_n - base_n

resolve_n
=
previous_resolve
+ server_change
+ musicolet_change
```

示例：

```text
V(n):

base=100
server=102
server_change=+2

import=103
musicolet_change=+3

resolve=105
```

下一次：

```text
V(n+1):

base=103

server=115
server_change=115-105=+10

import=108
musicolet_change=108-103=+5

resolve=105+10+5
=120
```

再下一次：

```text
base=108
server=138
previous_resolve=120

server_change=18

import=116
musicolet_change=8

resolve=146
```

再下一次：

```text
base=116
server=150
previous_resolve=146

server_change=14

import=120
musicolet_change=4

resolve=164
```

每次成功导入后：

```text
下一轮 base = 本轮 import
下一轮 previous_resolve = 本轮 resolve
```

`server_change` 是本轮动态计算值，不持久累计。

---

## 17.1 周/月/年播放数

以下统计同样应用该规则：

```text
总播放数
周播放数
月播放数
年播放数
```

分别按各自对应周期独立计算。

---

## 17.2 Last Played

Last Played 取：

```text
服务器真实 Last Played
与
新 Musicolet Last Played
```

中较晚的时间。

不要求记录毫秒级时间戳。

---

# 18. 播放队列与播放状态严格分离

Queue Content：

```text
Queue 内有哪些歌曲
歌曲顺序
```

属于持久数据，可以产生 M 和冲突。

Playback State：

```text
当前 Queue
当前歌曲
播放进度
播放状态
```

属于瞬时运行状态。

导入新 Musicolet ZIP 时：

> Playback State 永远以服务器状态为准。

不做冲突。

---

# 19. Queue 基本约束

## 19.1 Queue 不允许重复歌曲

任何 Queue 内：

```text
同一歌曲路径
```

只能出现一次。

---

## 19.2 Playlist 同样不允许重复歌曲

任何 Playlist 内同一歌曲不能重复出现。

---

# 20. Queue TAB

顶栏第一主 TAB：

```text
播放队列
```

---

## 20.1 Queue 下拉切换

页面顶部提供多 Queue 下拉组件。

点击后显示居中/浮层 Queue 管理列表。

支持：

```text
切换 Queue
拖动调整 Queue 上下位置
修改 Queue 名称
删除 Queue
```

---

## 20.2 切换 Queue 与播放 Queue 分离

单纯切换下拉：

> 只切换当前查看的 Queue。

不会改变正在播放 Queue。

点击 Queue 顶部：

```text
▶
```

即：

> 继续播放这个队列。

才切换实际播放 Queue。

---

## 20.3 每个 Queue 独立保存播放状态

例如：

```text
Queue A:
当前第20首
进度01:30

Queue B:
当前第7首
进度03:11
```

从 A 播到 B：

```text
恢复 B #7 / 03:11
```

再回 A：

```text
恢复 A #20 / 01:30
```

---

## 20.4 删除当前播放 Queue

允许删除当前正在播放 Queue。

删除后：

```text
切换到下一个 Queue
```

并从下一个 Queue 自己的：

```text
最后一次播放歌曲
+
播放进度记忆点
```

继续播放。

---

# 21. Queue 歌曲列表

每首歌显示：

```text
标题
艺术家
专辑
时长
```

右侧：

```text
···
```

---

## 21.1 Queue 顶部操作栏

横向显示：

```text
▶ 继续播放当前 Queue

排序

当前播放歌曲位置编号 / Queue 总歌曲数

保存

···
```

---

## 21.2 保存按钮

支持：

```text
保存为播放清单
默认名称 = Queue 名

加入到其他歌单
```

---

## 21.3 Queue 顶部三点菜单

包括：

```text
分享 Queue 中所有歌曲
导出 M3U
进入多选模式
```

---

# 22. Queue 排序

Queue 排序会真实修改 Queue 顺序，因此产生 Server M。

支持：

```text
随机化
反向

标题 - 递增
标题 - 递减

文件名 - 递增
文件名 - 递减

文件夹 - 递增
文件夹 - 递减

专辑 - 递增
专辑 - 递减

歌手 - 递增
歌手 - 递减

专辑作者 - 递增
专辑作者 - 递减

作曲人 - 递增
作曲人 - 递减

风格 - 递增
风格 - 递减

修改日期 - 递增
修改日期 - 递减

新增日期 - 递增
新增日期 - 递减

上次播放日期 - 递增
上次播放日期 - 递减

最常播放优先
最少播放优先
```

“随机化”是真正永久重排 Queue，而不是临时随机播放模式。

---

# 23. 单曲三点菜单

所有类似歌曲列表中，每首歌曲右侧提供：

```text
···
```

点击后显示居中纵向窄浮层。

Queue 中包括：

```text
歌曲信息
移出队列
插队待播
排到队尾
预览即听
此曲播完即停
编辑元数据
音频裁剪器
分享
永久删除
```

浮层右上角：

```text
♡ / ♥
```

Favorite 按钮。

---

# 24. 插队待播

当前：

```text
A ← playing
B
C
D
```

插入：

```text
X
Y
Z
```

结果：

```text
A
X
Y
Z
B
C
D
```

保持所选顺序。

如果歌曲已经存在 Queue：

> 移动已有歌曲，而不是生成重复项。

---

# 25. 排到队尾 / 加入 Queue

如果歌曲已经存在目标 Queue：

```text
移动到队尾
```

而不是：

```text
跳过
```

也不是：

```text
复制一份
```

适用于：

```text
排到队尾
加入当前播放队列
加入 Queue
```

---

# 26. Queue / Playlist 有序 Merge

应实现成员语义 Merge。

例如：

```text
BASE:
A B C D

SERVER:
A C B D

MUSICOLET:
A B C D E
```

自动：

```text
A C B D E
```

不冲突。

---

## 26.1 MOVE/MOVE

服务器：

```text
B → #1
```

手机：

```text
B → #4
```

冲突。

---

## 26.2 ADD/ADD 不同位置

BASE 没有 X。

服务器：

```text
ADD X → #3
```

手机：

```text
ADD X → #8
```

冲突。

---

## 26.3 SERVER DELETE + PHONE MOVE

服务器删除 B：

```text
REMOVE B
```

手机只是移动 B：

```text
MOVE B
```

不产生冲突。

服务器删除继续成立。

手机侧变化可按新数据变化逻辑处理。

---

# 27. Playlist 名称

服务器端：

> 禁止修改已有 Playlist 名称。

允许：

```text
创建个人歌单
删除个人歌单
加入歌曲
移出歌曲
调整歌曲顺序
```

不允许：

```text
重命名现有 Playlist
```

因此不需要处理服务器侧 Playlist rename merge。

---

# 28. 来源与 Queue 隐式关联

从：

```text
专辑
歌单
风格
作者
文件夹
```

直接播放时，应建立该来源对象与 Queue 的隐式关联。

例如来源：

```text
Playlist: AAA
```

系统可能已经有：

```text
Queue AAA
```

但这个 Queue 实际来自：

```text
Album AAA
```

则不能错误复用。

新建：

```text
AAA #编号
```

例如：

```text
AAA #2
```

后台应保存：

```text
Source Type
Source Object
↕
Queue
```

的隐性关联。

因此 Queue 的复用不是纯字符串 Queue Name Match。

---

# 29. 普通直接播放

在：

```text
专辑
作者
风格
文件夹
歌单
```

内直接点击某首歌：

系统使用与该来源关联的 Queue。

如果已有：

```text
与当前来源对象关联的 Queue
```

则：

```text
复用 Queue
跳转到所点歌曲播放
```

不重建 Queue。

如果不存在：

```text
创建以来源名称命名的 Queue
```

并加载该来源歌曲列表。

---

# 30. Playlist 乱序播放

点击 Playlist：

```text
乱序播放
```

逻辑：

### 已存在与该 Playlist 隐式关联的 Queue

```text
复用该 Queue
```

并且：

> 不重新打乱其现有 Queue 顺序。

### 不存在对应 Queue

创建 Queue：

```text
名称 = Playlist 名称
```

如果名称冲突：

```text
名称 #编号
```

并将 Playlist 歌曲随机化后写入 Queue。

---

# 31. 顺序播放 / 随机播放

Now Playing 中：

```text
顺序播放
随机播放
```

共用一个控件位置，相互切换。

该状态属于播放方式，不等同于 Queue 的“随机化排序”。

---

# 32. 循环控件

另有独立循环控件。

状态包括：

```text
单曲循环
列表循环
此曲播完即停
```

---

# 33. 此曲播完即停

可以在 Queue 中当前播放歌曲之后的任意歌曲 S 上执行：

```text
此曲播完即停
```

例如：

```text
A ← 当前
B
C
S ← stop target
D
```

播放器继续正常播放：

```text
A → B → C → S
```

直到：

```text
S 播放到进度条末端
```

停止。

不会进入 D。

---

## 33.1 Stop Target 跟随歌曲

设置绑定歌曲 S，不绑定 position number。

如果 S：

```text
移动位置
```

Stop Target 跟着 S。

如果 S：

```text
从 Queue 删除
```

Stop Target 自动取消。

如果切换 Queue：

```text
原 Queue 的 Stop Target 保留
```

以后恢复该 Queue 时仍有效。

---

# 34. 睡眠倒计时

汉堡菜单：

```text
睡眠倒计时
```

标准模式：

```text
在 hh:mm 之后停止

在 N 首歌之后停止

在指定歌曲播放完即停
```

第三项点击时显示操作提示：

```text
队列
→
找到想要的歌曲
→
[···]
→
[->||] 此曲播完即停
```

如果当前已经设置了 Stop Target：

睡眠面板显示：

```text
将在播放这首歌后停止：

<S歌曲标题>
```

---

# 35. 预览即听

预览是旁路播放。

不得：

```text
修改 Queue
改变当前 Queue
改变 Queue Current Song
```

预览结束/关闭后恢复原播放状态。

---

# 36. 多选模式

Queue、Folder、Album、Artist、Genre、Playlist 等歌曲列表支持 Multi-select。

底部固定显示宽扁操作浮层：

```text
选项
高级选择器
取消
```

---

## 36.1 高级选择器

支持：

```text
全选
反选
连续选择两首歌之间的多首歌曲
```

---

## 36.2 Queue 多选菜单

```text
移出队列
立即播放
乱序播放
加入喜爱
移出喜爱
编辑元数据（仅选择1首）
分享
永久删除
```

---

## 36.3 Library 类型列表多选

Folder / Album / Artist / Genre 等：

```text
立即播放
乱序播放
插队待播
加入当前播放队列
加入队列
加入歌单
加入喜爱
移出喜爱
编辑元数据（仅单曲）
分享
永久删除
```

---

# 37. 底部列表内搜索

各歌曲列表页面底部固定悬浮：

```text
搜索当前视图歌曲...
```

仅筛选当前视图中的歌曲。

---

# 38. 正在播放 TAB

布局从上到下：

```text
封面

歌曲标题
艺术家
专辑

功能按钮行

播放进度

播放控制
```

---

## 38.1 封面

显示歌曲封面。

点击封面：

```text
显示歌词
```

---

## 38.2 信息按钮

点击圆形 Info：

显示纵向窄浮层。

内容包括：

```text
封面
文件名
文件路径

歌曲标题
艺术家
专辑
专辑作者
作曲人
风格

歌词编辑/查看按钮

曲目编号
Disc编号
年份
备注

时长
比特率
采样率
采样位数
格式
编码
声道
文件大小

入库时间
修改时间
最近播放时间
播放数
```

底部：

```text
左：
编辑元数据

右：
完成
```

---

## 38.3 加入歌单

浮层顶部：

```text
艺术家 - 歌曲标题
```

大 TAB：

```text
加入歌单
移出歌单
```

每个歌单：

```text
Checkbox
```

底部：

```text
创建新歌单
```

仅“加入歌单”TAB显示。

以及：

```text
取消
加入/移出
```

---

## 38.4 播放速度

支持：

```text
0.25
0.5
0.75
1.0
1.25
1.5
1.75
2.0
```

---

## 38.5 其他播放按钮

包括：

```text
Favorite
Info
Playlist
Playback Speed
Equalizer
···

顺序/随机
循环
```

---

## 38.6 进度控制

显示：

```text
当前时间
进度条
总时长
```

底部控制：

```text
上一曲
快退
播放/暂停
快进
下一曲
```

---

# 39. 文件夹 TAB

第一行：

```text
搜索栏
排序按钮
```

排序至少支持：

```text
名称
歌曲数
总时长
更新日期
```

升序/降序。

列表：

```text
Folder icon
Folder name
完整路径
```

点击进入该文件夹歌曲列表。

歌曲列表功能遵循统一 Library Song List 规范。

---

# 40. 专辑 TAB

第一行：

```text
搜索栏
排序
```

列表：

```text
Album Cover
Album Name
```

点击进入 Album Songs。

排序支持：

```text
名称
歌曲数
总时长
更新日期
```

及升/降序。

---

# 41. 艺术家 TAB

子 TAB：

```text
Artists
Album Artists
Composers
```

下方：

```text
搜索
排序
```

列表：

```text
一行一个 Artist
```

---

## 41.1 Artist 详情

顶部：

```text
←
Artist Name
```

下一行：

```text
7个专辑
```

下一行：

无限横向滚动 Album 选择器：

```text
全部专辑
Album 1
Album 2
...
```

每个具体 Album：

```text
封面
Album Name
```

下方：

```text
11首歌
合计时长 53:36
```

右对齐：

```text
乱序播放
排序
···
```

三点菜单：

```text
立即播放
插队待播
加入当前播放队列
加入队列
加入歌单
分享
```

---

# 42. 风格 TAB

顶部：

```text
搜索
排序
```

纵向：

```text
Genre Name
```

点击进入歌曲列表。

---

# 43. 歌单 TAB

首先显示系统保留 Playlist：

```text
全部歌曲
喜爱
最近加入
最近播放
最多播放
尚未播放
```

随后：

```text
个人歌单                         + 新建歌单
```

再显示：

```text
Playlist 1
Playlist 2
...
```

底部：

```text
搜索歌单...
```

---

## 43.1 系统 Playlist 权限

以下为派生数据，禁止手工增删成员：

```text
全部歌曲
最近加入
最近播放
最多播放
尚未播放
```

`喜爱`：

```text
允许通过 ♥ 修改成员
```

---

## 43.2 全部歌曲 / 喜爱歌曲列表

显示：

```text
歌曲数
总播放时长
```

顶部：

```text
随机播放
排序
···
```

歌曲项：

```text
封面
Title
Artist
Album
Duration
···
```

支持 Multi-select。

底部固定当前视图搜索。

---

# 44. Playlist 的标签化意义

个人 Playlist 除播放列表意义外，也允许用户长期将其用于：

> 对歌曲进行标签化分类。

一首歌曲可以属于多个 Playlist。

但不得因此：

```text
把 Playlist 自动写入 Genre
```

也不得为了同步 Playlist：

```text
修改原音频 Metadata
```

Playlist 关系保持为独立业务层。

---

# 45. 搜索 TAB

顶部：

```text
Search Input
```

输入即搜索。

不要额外“搜索”按钮。

下方显示：

```text
搜索历史关键词
```

底部固定无边框按钮：

```text
清除搜索历史
```

---

## 45.1 搜索结果分组

第一行：

```text
46个专辑
6个歌手
7个专辑作者
1个文件夹
...
```

只显示非零分组。

点击分组统计：

打开悬浮面板，列出该分组类型结果。

例如：

```text
Albums
Artists
Album Artists
Folders
...
```

---

## 45.2 Song Results Header

第二行：

左：

```text
找到21首歌曲
```

右：

```text
随机播放
···
```

下方列出 Song Results。

---

# 46. 标签解析偏好

汉堡菜单：

```text
标签解析偏好
```

默认：

> 所有标签均允许通过分隔符识别多值作者/分类。

默认分隔符集合包括：

```text
,
;
|
&
ft.
feat.
```

允许分别设置：

```text
演唱者
专辑作者
作曲人
风格
```

的分隔符。

---

## 46.1 只影响解析层

例如：

```text
Artist raw:
A feat. B & C
```

解析：

```text
A
B
C
```

供：

```text
Artist 页面
搜索
分类
统计
```

使用。

但原 Metadata 必须继续保存：

```text
A feat. B & C
```

**绝不能自动改写原 Metadata。**

---

# 47. Metadata 编辑

服务器：

```text
编辑元数据
```

只修改：

```text
服务器数据库
```

形成 Server M。

不会：

```text
写手机文件
修改 Android 上的 ID3
修改 Musicolet App
```

---

## 47.1 新 ZIP Merge

例：

```text
BASE:
Title=AAA

SERVER:
Title=BBB

NEW MUSICOLET:
Title=AAA
```

手机没改。

结果：

```text
BBB
```

不冲突。

如果：

```text
NEW MUSICOLET:
Title=CCC
```

则：

```text
SERVER 修改 Song Core
MUSICOLET 也修改 Song Core
```

产生冲突。

---

# 48. 永久删除歌曲

服务器点击：

```text
永久删除
```

含义：

```text
删除服务器 file_id 对应歌曲数据块

删除与：
Queue
Playlist
Favorite
等关系
```

不会：

```text
删除手机文件
删除 Musicolet 数据
修改手机目录
```

产生一个正式 Server M 和 Git Commit。

---

# 49. 手机侧执行者

必须提供一个 Go 手机侧 Agent。

不是 APK。

交付：

```text
Go 源码
arm64-v8a 构建脚本
```

用户自行编译为：

```text
Linux/Android arm64 Go binary
```

然后在：

```text
Termux
```

运行。

---

## 49.1 手机 Agent 权限边界

Agent 必须为严格只读组件。

禁止：

```text
修改手机文件
删除手机文件
移动手机文件
重命名手机文件
新增手机文件
修改 Metadata
修改 Musicolet 数据
```

允许：

```text
读取歌曲
读取必要文件信息
计算 PCM SHA-256
按需读取并传输歌曲字节
```

---

## 49.2 不采用轮询

手机 Agent：

> 不向服务器周期性轮询请求。

要求节能。

架构可参考 FRP 思路：

```text
手机主动建立长期出站连接
↓
保持低成本长连接
↓
服务器在连接内下发请求
↓
复用连接传输数据
```

应：

```text
低频 heartbeat
断线指数退避
连接复用
按需数据传输
无任务时近乎空闲
```

---

# 50. 歌曲缓存

服务器不建立永久：

```text
server_storage_path
```

歌曲音频文件不属于服务器事实数据。

---

## 50.1 缓存策略

播放到某首歌曲时：

```text
优先浏览器缓存
↓
服务器缓存
↓
从手机在线拉取
```

缓存按需产生。

---

## 50.2 手机不可达

如果：

```text
Browser cache = MISS
Server cache = MISS
Phone = Offline
```

则无法播放。

显示：

> 当前歌曲未缓存，且无法从手机在线取得歌曲文件。

---

## 50.3 缓存清理

必须提供：

服务器：

```text
清除单首歌曲缓存
批量清除歌曲缓存
一键清除全部歌曲缓存
```

浏览器：

```text
清除单首歌曲缓存
批量清除歌曲缓存
一键清除全部歌曲缓存
```

第一阶段不要求自动 LRU 清理。

---

# 51. 音频裁剪器

获取：

```text
手机文件
或
已有缓存
```

进行临时音频裁剪。

输出新文件：

```text
下载
分享
```

不得：

```text
自动替换原歌曲
修改手机歌曲
修改 Musicolet
自动加入服务器曲库
```

---

# 52. 分享系统

系统除以下页面外：

```text
分享页面
正在播放公开页
```

其他页面均要求管理员登录。

---

## 52.1 单曲分享

点击分享：

提供：

```text
分享歌曲文件链接

分享该单曲播放页面
```

歌曲文件分享页显示：

```text
文件基本信息
Metadata
专业音频信息
下载按钮
```

---

## 52.2 列表类分享

以下对象：

```text
Queue
Playlist
Album
Artist
Folder
```

分享时：

生成：

```text
当前特定歌曲列表的公开分享页链接
```

并记录到：

```text
分享记录
```

---

# 53. 管理员认证

除公共播放/分享页面外：

```text
全部系统功能
```

要求 Admin 权限。

认证方式：

```text
账号
+
密码
+
Google Authenticator / TOTP
```

服务端 Go 程序启动前：

通过环境变量配置管理员：

```text
账号
密码
```

不得硬编码进源码。

认证成功后建立安全 Session。

---

# 54. 汉堡菜单

包含：

```text
睡眠倒计时

语言

界面
  - 主题
  - Tab-bar 位置

标签解析偏好

备份 / 还原本系统数据

从 Musicolet 备份数据导入
```

---

# 55. 本系统 Backup / Restore

本系统 Backup 与 Musicolet Backup 完全不同。

不能用于 Musicolet App Restore。

必须包含全部持久化事实数据：

```text
业务数据库

Git repository
完整 Commit History

全部正式 Musicolet Snapshots

全部原始 Musicolet Backup ZIP

Import Procedures
包括未完成和已取消 Procedure

Diff
Conflict
Resolution
Manual Patch

Server M / Change Marks

PCM SHA-256 Reference DB

Settings
标签解析偏好
UI 偏好

Share Records
```

---

## 55.1 不包含缓存

不备份：

```text
服务器歌曲缓存
浏览器歌曲缓存
```

缓存不是事实数据，可重新取得。

---

# 56. Musicolet Backup Archive

原始 ZIP 永久保存。

建议目录逻辑：

```text
data/
  imports/
    procedure-000001/
      original.zip
      decrypted/
      snapshot/
      diff/
      ...
```

最终具体目录结构可由实现阶段决定，但必须保证：

```text
ZIP ↔ Procedure ↔ Musicolet Version
```

关系明确可追溯。

---

# 57. Import Parser Version

解析器必须有：

```text
parser_version
```

历史 ZIP 永远保留，因此未来：

```text
Parser v2
```

即使修复 v1 解析错误，也可以重新解析旧 ZIP。

不得修改原始 ZIP。

---

# 58. 数据可重建性

整个系统应满足：

即使业务数据库损坏，在拥有：

```text
原始 Musicolet ZIP 历史
Git History
Procedure / Resolution
Server Changes
```

的前提下，应尽最大可能重建：

```text
Musicolet Versions
当前服务器状态
历史 Changes
```

原始来源与 Change History 优先于派生缓存。

---

# 59. 前端权限边界

公开：

```text
分享页面
正在播放公开页面
```

管理员：

```text
Queue 管理
Library
Folder
Album
Artist
Genre
Playlist
Search
Metadata Edit
Permanent Delete
Import Procedure
Settings
Backup
Cache Management
Share Management
```

---

# 60. UI 总体布局

顶栏主 TAB：

```text
播放队列
正在播放
文件夹
专辑列表
艺术家列表
风格
歌曲清单
搜索
汉堡菜单
```

Artist 下设：

```text
Artists
Album Artists
Composers
```

要求：

* 页面信息密度接近 Musicolet；
* 优先适配移动端；
* Desktop Web 同样可完整使用；
* 各功能控件位置尽量保持 Musicolet 使用习惯；
* 弹层主要采用居中纵向窄浮层；
* 歌曲列表底部搜索、多选操作栏采用 fixed/floating 形式；
* Queue 多选和 Library 多选保持统一操作范式。

---

# 61. 数据修改优先级总结

对于同一路径、同一业务对象：

```text
BASE
旧 Musicolet Version

OURS
服务器当前状态

THEIRS
新 Musicolet Version
```

核心：

### THEIRS 未变

```text
THEIRS == BASE
```

无权覆盖 Server M。

### THEIRS 有变，OURS 未变

```text
THEIRS != BASE
OURS == BASE
```

采用 THEIRS。

### BOTH changed，结果相同

```text
OURS == THEIRS
```

直接接受。

### BOTH changed，结果不同

```text
OURS != BASE
THEIRS != BASE
OURS != THEIRS
```

产生 Conflict。

Song Core 整体判断。

Queue/Playlist 按成员语义判断。

Playback statistics 使用增量公式。

Playback State 永远服务器优先。

---

# 62. Server Change 标记

发生 Server M 后，对相关对象设置可快速查询的标记：

```text
has_server_changes = true
```

并允许查看：

```text
Metadata changed
Deleted on server
Playlist changed
Queue changed
Favorite changed
...
```

Git 负责完整历史。

标记负责告诉用户：

> 当前对象存在尚未被后续 Musicolet 数据真正取代/解决的服务器改动。

新 Musicolet ZIP 未修改相同数据时：

```text
标记继续存在
```

发生真正 conflict 并完成 Resolution 后：

根据 Resolution 更新标记状态。

---

# 63. 非功能性要求

## 63.1 原子性

Import Commit：

```text
All-or-Nothing
```

---

## 63.2 可审计性

必须能够回答：

```text
这个值来自哪个 Musicolet ZIP？
这个值什么时候被服务器修改？
谁选择了哪个 Conflict Resolution？
当时 BASE / OURS / THEIRS 分别是什么？
```

---

## 63.3 可恢复性

Procedure 可以：

```text
退出
下次继续
```

Resolution 不因关闭页面丢失。

---

## 63.4 一致性

不允许：

```text
Song 删除成功
但 Playlist 关系删除失败
```

这类半提交状态。

---

## 63.5 安全性

服务端：

```text
Admin Password 不写源码
敏感配置使用环境变量/安全持久层
```

手机 Agent：

```text
只读
最小权限
不具备文件写操作 API
```

公开分享页面不得暴露管理 API。

---

## 63.6 节能

手机 Agent：

```text
禁止轮询
长期出站连接
低频 heartbeat
断线退避
按需读取歌曲
```

---

# 64. 第一阶段明确不做

暂不：

```text
用 PCM SHA-256 自动识别歌曲身份

用 Chromaprint

跨 Musicolet Version 恢复原 file_id

判断 path change 是否 MOVE

让服务器修改手机文件

让服务器删除手机歌曲

让服务器写回 Metadata

将 Playlist 自动写入 Genre

服务器永久保存全量歌曲文件

自动 LRU 清理歌曲缓存

服务器侧修改已有 Playlist 名称
```

---

# 65. 核心验收原则

系统完成后至少必须证明以下场景正确：

### 场景 A：服务器 Metadata 修改，手机未改

```text
BASE Title=A
SERVER Title=B
NEW MUSICOLET Title=A
```

结果：

```text
B
```

无冲突。

---

### 场景 B：双方修改 Song Core

```text
BASE A
SERVER B
MUSICOLET C
```

进入 Conflict。

---

### 场景 C：播放次数双端增加

按照：

```text
resolve
=
previous_resolve
+
(server-current - previous_resolve)
+
(import - previous-import)
```

正确累计。

---

### 场景 D：服务器删除歌曲，手机仅继续播放

服务器 DELETE 继续有效。

播放次数变化不得把歌曲恢复。

---

### 场景 E：手机改路径

```text
/A.mp3 DELETE
/B.mp3 ADD
```

不试图恢复 file_id。

---

### 场景 F：Queue Server MOVE + Phone ADD

自动合并。

---

### 场景 G：Queue 双方 MOVE 同一歌曲到不同位置

产生 Conflict。

---

### 场景 H：Procedure Resolve 后服务器再次修改该对象

Resolution 显示：

```text
STALE
```

要求重新确认。

---

### 场景 I：Procedure 未结束上传新 ZIP

直接拒绝。

---

### 场景 J：最终 Commit 前服务器 HEAD 已变化

拒绝 Commit，并重新分析。

---

### 场景 K：两条 PCM SHA-256 相同的不同路径歌曲

必须继续作为两个完全独立歌曲处理。

---

### 场景 L：手机离线且两级缓存 MISS

歌曲不可播放，但 Metadata 页面仍正常。

---

### 场景 M：手机 Agent

验证其不存在：

```text
write
delete
rename
move
create
```

手机歌曲文件操作能力。

---

# 66. 总体技术架构

```text
┌─────────────────────────────┐
│        Android / Termux      │
│                             │
│ Read-only Go Agent          │
│ - PCM SHA-256               │
│ - On-demand file streaming  │
└──────────────┬──────────────┘
               │
      long-lived outbound
               │
               ▼
┌─────────────────────────────┐
│         Go Backend          │
│                             │
│ Auth / TOTP                 │
│ Player API                  │
│ Library API                 │
│ Import Parser               │
│ Semantic Diff               │
│ Merge Engine                │
│ Procedure Engine            │
│ Cache                       │
│ Share Service               │
└───────┬───────────┬─────────┘
        │           │
        ▼           ▼
┌────────────┐  ┌──────────────┐
│Business DB │  │ Git/libgit2  │
│            │  │ + git2go     │
└────────────┘  └──────────────┘
        │
        ▼
┌─────────────────────────────┐
│ data/                       │
│                             │
│ Original Musicolet ZIP      │
│ Snapshot                    │
│ Procedure                   │
│ Backup                      │
│ Temporary song cache        │
└─────────────────────────────┘
               │
               ▼
┌─────────────────────────────┐
│        Web Frontend         │
│                             │
│ Musicolet-style UI          │
│ Indexed/Browser Cache       │
│ Public Share/Now Playing    │
│ Admin Library               │
└─────────────────────────────┘
```

---

# 67. 最终架构原则

整个系统的核心不是：

> 想办法给每一首歌曲制造一个永远不变的神奇 ID。

而是：

> 忠实保存每一个 Musicolet Version 的真实状态，忠实保存服务器上的每一个 Change，通过可审计的三方比较和人工 Resolution 管理两条数据演进历史。

因此：

```text
Musicolet Snapshot
```

负责表达手机当时是什么。

```text
Server Changes
```

负责表达服务器后来做过什么。

```text
Git
```

负责表达历史和合并关系。

```text
Import Procedure
```

负责让新 Musicolet 数据安全进入系统。

```text
PCM SHA-256 Reference DB
```

只负责提供音轨内容参考信息。

这几套机制彼此分工，不互相越权。
