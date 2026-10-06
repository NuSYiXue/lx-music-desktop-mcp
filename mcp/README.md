# LX Music MCP Server — 接入指南

## 怎么用这份文件

**这是写给 AI agent 的操作指南**，所以多数情况下你不需要自己动手：

把 `README.md`（连同同目录的 `lx-music-mcp.exe`，或整个 zip）交给你的 AI agent，
说一句「按这份 README 把 LX Music 接入 MCP」——它会自己把 server 装到统一位置、
按你的 host 写好配置，然后让你重启。

想手动装也完全可以，下面每一步都是人可照做的。

---

本目录是一个**分发单元**，里面有两个文件：

| 文件 | 说明 |
|---|---|
| `lx-music-mcp.exe` | MCP server，单文件、无运行时依赖、不需要装 Node 或 Python |
| `README.md` | 就是本文件 |

---

## 安装位置：只有一处

**不管用户怎么说、不管你从哪个副本装，都装到同一个位置：**

| 平台 | 目录 |
|---|---|
| Windows | `%UserProfile%\.lx-music-desktop-mcp\`（例如 `C:\Users\ZACK\.lx-music-desktop-mcp\`） |
| macOS / Linux | `~/.lx-music-desktop-mcp/` |

把 `lx-music-mcp.exe` 放进该目录，**所有 host 的配置都指向这一份**：

```
Windows       %UserProfile%\.lx-music-desktop-mcp\lx-music-mcp.exe
macOS / Linux ~/.lx-music-desktop-mcp/lx-music-mcp.exe
```

### 为什么必须只有一处

**不要**把它复制到工作目录、项目目录、绿色版目录里，再让配置指向那些副本。
多份副本之间不会自动同步，升级时必然漏掉正在用的那一份——这个坑真实踩过：
机器上曾同时存在四份，升级时更新了三份，偏偏漏掉了正在用的那份，于是「功能没生效」。

所以：**这台机器上只保留 `%UserProfile%\.lx-music-desktop-mcp\` 这一份。**
升级就是覆盖这一份，不需要同步任何其他地方。

---

## 给 AI agent 的操作指令

如果你是一个 AI agent，用户让你「把 LX Music 接入 MCP」——**无论他怎么描述、无论你在哪个工作区、
无论你读到的是哪一份 README**——都照下面做：

1. **把 exe 装到统一位置**（目录不存在就先建；已存在就覆盖，覆盖即升级）：

   Windows（bash / git-bash）：
   ```bash
   mkdir -p "$USERPROFILE/.lx-music-desktop-mcp" && \
     cp "<你读到本文件的目录>/lx-music-mcp.exe" "$USERPROFILE/.lx-music-desktop-mcp/"
   ```

   macOS / Linux：
   ```bash
   mkdir -p ~/.lx-music-desktop-mcp && \
     cp "<你读到本文件的目录>/lx-music-mcp.exe" ~/.lx-music-desktop-mcp/
   ```

   把本文件也一并复制过去（`cp "<你读到本文件的目录>/README.md"` 同目标目录），
   方便以后在这个位置阅读。

2. **算出绝对路径**：`%UserProfile%\.lx-music-desktop-mcp\lx-music-mcp.exe`
   （macOS / Linux 为 `~/.lx-music-desktop-mcp/lx-music-mcp.exe`）。
   **不要写占位符，也不要指向别处的副本。**

3. **按 host 写配置**（格式见下），`command` 指向第 2 步的路径。

4. **告诉用户重启 host / 重连该 server。**

5. **验证**：调用一次 `lx_status`，能返回当前歌曲信息就说明接好了。

> ⚠️ **只重启 host 不够。** 工具的名称与描述会被 host 缓存——重启后读到的可能仍是旧 schema，
> 要等 server **实际连接一次**（发起任一次工具调用）才会刷新。若你看到的工具描述与实际不符，
> 就再调用一次 `lx_status` 触发刷新。

如果 `lx_status` 报「无法连接 LX Music 的 Open API 服务」，见文末常见问题。

---

## 前置条件

- **LX Music Desktop 正在运行**。MCP server 只是个客户端，LX 没开就没法工作。
- **已在 LX Music 设置里开启 Open API 服务**（默认端口 `23330`）。
- 如果用户改过端口，配置里加上环境变量 `LX_OPEN_API`，指向完整地址，例如
  `LX_OPEN_API=http://127.0.0.1:12345`。

---

## 接入配置

### Reasonix

写在项目根目录的 `./reasonix.toml`，或全局 `config.toml`。
Reasonix 会展开 `${VAR}`，所以 Windows 下推荐直接用环境变量（换机器/换用户名都不用改）：

```toml
[[plugins]]
name    = "lx-music"
command = '${USERPROFILE}\.lx-music-desktop-mcp\lx-music-mcp.exe'
```

也可以写死绝对路径（TOML 单引号字面量字符串，反斜杠不需要转义）：

```toml
[[plugins]]
name    = "lx-music"
command = 'C:\Users\ZACK\.lx-music-desktop-mcp\lx-music-mcp.exe'
```

> 建议 `name` 统一用 `lx-music`。每个工作区用不同名字会让 host 把它们当成不同的 server
> ——工具前缀不同、schema 缓存也各存一份，刷新时得各刷一次。

### Claude Desktop

编辑 `claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "lx-music": {
      "command": "C:/Users/ZACK/.lx-music-desktop-mcp/lx-music-mcp.exe"
    }
  }
}
```

### Cursor / Cline / 其他 host

这些 host 用的是同一套 `mcpServers` 结构（Cursor 在 `.cursor/mcp.json`，
Cline 在扩展设置里），照抄上面 Claude Desktop 的写法即可。

> JSON 里的路径建议用**正斜杠 `/`**，这样不必处理反斜杠转义；
> 且 JSON 不支持 `${VAR}` 展开，必须写真实绝对路径。

### 需要改端口时

在配置里加 `env`：

```json
{
  "mcpServers": {
    "lx-music": {
      "command": "C:/Users/ZACK/.lx-music-desktop-mcp/lx-music-mcp.exe",
      "env": { "LX_OPEN_API": "http://127.0.0.1:12345" }
    }
  }
}
```

---

## 升级

1. 用新版 `lx-music-mcp.exe` 覆盖 `%UserProfile%\.lx-music-desktop-mcp\lx-music-mcp.exe`；
   如果 `README.md` 也更新了，一并覆盖过去（它记录当前的用法与已知坑）。
2. 在 host 里重连该 server，或让它实际连接一次（触发工具描述刷新）。

就这样——因为只有一个位置，**不需要同步任何副本**。

---

## 可用工具

| 工具 | 用途 |
|---|---|
| `lx_status` | 当前播放状态（歌名、歌手、进度、音量…） |
| `lx_control` | 播放 / 暂停 / 切歌 / 跳转 / 音量（直接设定或相对加减） / 静音 / 收藏 |
| `lx_lyric` | 当前歌词（LRC 纯文本，或全部歌词类型） |
| `lx_queue` | 当前播放队列 |
| `lx_search` | 搜索歌曲（酷我 / 酷狗 / 咪咕 / QQ音乐 / 网易云） |
| `lx_playlists` | 列出 / 新建 / 删除歌单 |
| `lx_playlist_songs` | 查看 / 追加 / 移除 / 整体替换歌单里的歌 |
| `lx_play` | 播放歌单里的指定歌曲 |
| `lx_batch_add` | 批量找歌并入库：一次完成「建歌单 → 并发搜索 → 过滤非原唱版本 → 分批入库」 |

### `lx_status` 返回哪些字段

不传 `filter` 时的默认返回：
`status` `name` `singer` `albumName` `lyricLineText` `duration` `progress` `playbackRate` `volume` `mute`

其中 `volume` 是 0-100 的整数、`mute` 是布尔值；**读它们纯只读，不会改动任何状态**。

想取默认集之外的字段，用 `filter` 传逗号分隔的字段名，可选的还有
`picUrl`、`collect`、`lyric`、`tlyric`、`rlyric`、`lxlyric`、`lyricLineAllText`。
例如 `filter: "name,volume"` 就只回这两个字段。

### `lx_control` 的动作

`action` 取值：`play` / `pause` / `next` / `prev` / `seek` / `volume` / `volume_up` / `volume_down` / `mute` / `collect` / `uncollect`。

- `seek` 与音量类动作用 `value` 传参：`volume` 传 1-100 的绝对值，
  `volume_up` / `volume_down` 传步长（不传则默认 4）。
- `mute` 用 `mute: true|false` 传参。
- 其余动作不需要额外参数。

### 一个重要的使用约定

歌曲一律用 **ref** 引用。`ref` 来自 `lx_search`、`lx_playlist_songs(list)` 或 `lx_queue` 的返回，
服务端会用它在内部取回完整的歌曲信息。

**不要让模型自己拼装或裁剪歌曲对象**——那会导致歌曲明明进了歌单却永远播不出来。
`lx-search` 之类的工具也不会把完整歌曲信息丢给模型，返回的就是精简列表加 ref。

### 音质等级

从高到低：`flac24bit` > `flac` > `320k` > `128k`。

`lx_search` 的 `minQuality` 就是按这个层级过滤的；返回结果里每首歌的 `quality`
字段会列出该曲可用的全部等级。

### 歌手匹配等级

`lx_search` 传了 `matchSinger` 之后，每条结果会带 `matchLevel`：

| 值 | 含义 |
|---|---|
| `exact` | 原唱 |
| `partial` | 合作 / feat |
| `none` | 翻唱 |

`lx_batch_add` 返回的 `uncertain` 列表，就是「找到了但 `matchLevel` 不是 `exact`」的那些歌，
值得向用户提一句。

---

## 常见问题

**Q：工具报「无法连接 LX Music 的 Open API 服务」**

依次检查：

1. LX Music 是否在运行？
2. LX Music 设置里 **Open API 服务** 是否已开启？
3. 端口是否被改过？默认是 `23330`。改过的话需要设 `LX_OPEN_API` 环境变量。
4. 是否有防火墙 / 代理拦了 `127.0.0.1` 的本地回环请求？

**Q：提示「ref 已失效，请重新搜索」**

`ref` 是存在 server 进程内存里的，host 重启后会清空。
重新搜索一次、用新的 `ref` 即可。这是设计如此——服务端不落地持久状态。

**Q：播放没反应**

播放是异步的：`lx_play` 返回成功只代表播放已开始加载，真的出声要几秒钟。
等几秒再用 `lx_status` 确认。短于这个时间查到的 `error` / `stoped` 是正常过渡态。

**Q：调用播放控制后，状态没有立刻变**

`lx_control` 的 pause / play / volume / mute 都是先改 renderer 侧的状态、
再由它回传上来，实测有 **1~2 秒**延迟。发出命令后等 2 秒再查 `lx_status`；
短于这个时间读到旧值，不代表命令失败。

**Q：怎么读当前音量？**

`lx_status` 的默认返回里就有 `volume`（0-100）与 `mute`，纯只读，不改动任何状态。
只想单独取音量时用 `lx_status` 的 `filter`：传 `volume`，返回 `{"volume":82}`。

别用 `volume_up` / `volume_down` 反推当前音量——它们是「读 → 加减 → 写回」，
会真的改一次音量。

**Q：`volume_up` / `volume_down` 一次调多少？**

不传 `value` 时一次调 4（与 LX 自己的音量快捷键一致：renderer 侧
`handleSetVolumeUp` 的 step 是 `0.04`，换算到这个 0-100 分制就是 4）；
传了 `value` 就按传入的步长走。

调到两端会**停住**：加过头停在 100、减过头停在 1，都不报错，所以连着调几次是安全的。
返回值直接给出新音量（`{"ok":true,"action":"volume_up","volume":51}`），不必再查一次。

server 侧会记住 **5 秒内**自己设过的音量，作为下一次相对增减的基准——这是为了绕开
renderer 的回传延迟（见上一条 Q）。超过 5 秒就回落到读 `lx_status`；所以你要是中途
手动拖了 LX 的音量滑块，下一次增减依然基于真实值。

**Q：`seek` 报 `Invalid offset`**

`seek` 的上限由服务端按**当前歌曲时长**校验。歌曲还没加载出时长时
（`lx_status` 里 `duration` 为 0），任何跳转都会被合法拒绝。
这是 LX 的状态，不是 server 的错误。

**Q：一次加很多歌会很慢吗？**

`lx_batch_add` 内置并发（5 路）和限流，几十首在十几秒内能完成。
不要改成让模型循环调用 `lx_search` 几十次——那既慢又会撑爆上下文。

**Q：装了新版本，但 host 里的工具描述还是旧的**

工具描述被 host 缓存了。重启 host 只是让它重新读缓存；**要真正刷新，得让 server
实际连接一次**——随便调用一个工具（例如 `lx_status`）即可。详见上文「给 AI agent 的操作指令」第 5 步。
