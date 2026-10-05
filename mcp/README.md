# LX Music MCP Server — 接入指南

本目录里有两个文件：

| 文件 | 说明 |
|---|---|
| `lx-music-mcp.exe` | MCP server，单文件、无运行时依赖、不需要装 Node 或 Python |
| `README.md` | 就是本文件 |

---

## 给 AI agent 的操作指令

如果你是一个 AI agent，用户让你「把 LX Music 接入 MCP」，请照下面做：

1. **先算出 exe 的绝对路径**：本文件所在目录 + `lx-music-mcp.exe`。
   本文件的位置是已知的（你正在读它），据此推出 exe 的路径。**不要猜路径，也不要写什么占位符。**
2. **判断当前 host 是哪一种**（Reasonix / Claude Desktop / Cursor / Cline / 其他），按下面对应的格式写配置。
3. **写完后告诉用户重启 host**，配置才会生效。
4. **验证**：让用户重启后，调用一次 `lx_status`。能返回当前歌曲信息就说明接好了。

如果 `lx_status` 报「无法连接 LX Music 的 Open API 服务」，见文末的常见问题。

---

## 前置条件

- **LX Music Desktop 正在运行**。MCP server 只是个客户端，LX 没开就没法工作。
- **已在 LX Music 设置里开启 Open API 服务**（默认端口 `23330`）。
- 如果用户改过端口，配置里加上环境变量 `LX_OPEN_API`，指向完整地址，例如
  `LX_OPEN_API=http://127.0.0.1:12345`。

---

## 接入配置

把下面的路径换成第 1 步算出来的真实绝对路径。

### Reasonix

写在项目根目录的 `./reasonix.toml`，或全局 `config.toml`（路径见 Reasonix 文档）：

```toml
[[plugins]]
name    = "lx-music"
command = 'C:\path\to\mcp\lx-music-mcp.exe'
```

Windows 路径建议用 **TOML 单引号字面量字符串**（如上），这样反斜杠不需要转义。

### Claude Desktop

编辑 `claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "lx-music": {
      "command": "C:/path/to/mcp/lx-music-mcp.exe"
    }
  }
}
```

### Cursor / Cline / 其他 host

这些 host 用的是同一套 `mcpServers` 结构（Cursor 在 `.cursor/mcp.json`，
Cline 在扩展设置里），照抄上面 Claude Desktop 的写法即可。

> JSON 里的路径建议用**正斜杠 `/`**，这样不必处理反斜杠转义。

### 需要改端口时

在配置里加 `env`：

```json
{
  "mcpServers": {
    "lx-music": {
      "command": "C:/path/to/mcp/lx-music-mcp.exe",
      "env": { "LX_OPEN_API": "http://127.0.0.1:12345" }
    }
  }
}
```

---

## 可用工具

| 工具 | 用途 |
|---|---|
| `lx_status` | 当前播放状态（歌名、歌手、进度、音量…） |
| `lx_control` | 播放 / 暂停 / 切歌 / 跳转 / 音量 / 静音 / 收藏 |
| `lx_lyric` | 当前歌词（LRC 纯文本，或全部歌词类型） |
| `lx_queue` | 当前播放队列 |
| `lx_search` | 搜索歌曲（酷我 / 酷狗 / 咪咕 / QQ音乐 / 网易云） |
| `lx_playlists` | 列出 / 新建 / 删除歌单 |
| `lx_playlist_songs` | 查看 / 追加 / 移除 / 整体替换歌单里的歌 |
| `lx_play` | 播放歌单里的指定歌曲 |
| `lx_batch_add` | 批量找歌并入库：一次完成「建歌单 → 并发搜索 → 过滤非原唱版本 → 分批入库」 |

### 一个重要的使用约定

歌曲一律用 **ref** 引用。`ref` 来自 `lx_search`、`lx_playlist_songs(list)` 或 `lx_queue` 的返回，
服务端会用它在内部取回完整的歌曲信息。

**不要让模型自己拼装或裁剪歌曲对象**——那会导致歌曲明明进了歌单却永远播不出来。
`lx-search` 之类的工具也不会把完整歌曲信息丢给模型，返回的就是精简列表加 ref。

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

**Q：一次加很多歌会很慢吗？**

`lx_batch_add` 内置并发（5 路）和限流，几十首在十几秒内能完成。
不要改成让模型循环调用 `lx_search` 几十次——那既慢又会撑爆上下文。
