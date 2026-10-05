# lx-music-desktop-mcp

[LX Music Desktop](https://github.com/lyswhut/lx-music-desktop) 的 MCP server。

编译成一个**单文件可执行程序**，无运行时依赖（不需要 Node、Python 或任何运行库），
任何支持 MCP 的 agent（Reasonix / Claude Desktop / Cursor / Cline 等）都能直接接进来，
用自然语言搜索歌曲、整理歌单、控制播放。

它替代了原先的 skill 方案（`SKILL.md` + `batch_add.js`），
并把 `batch_add.js` 的批量能力原样内迁成服务端逻辑。

---

## 快速开始

### 1. 构建

```bash
go build -trimpath -ldflags "-s -w -X main.version=dev" -o mcp/lx-music-mcp.exe ./cmd/lx-music-mcp
```

或者用 PowerShell 脚本：

```powershell
.\build.ps1                    # 本机平台
.\build.ps1 -All               # 交叉编译 windows / linux / darwin
```

产物约 8.3 MB，静态链接，`CGO_ENABLED=0`。

### 2. 接入

见 [`mcp/README.md`](mcp/README.md)——那份文件是随产物一起分发的接入指南，
里面的说明对人和 AI 都适用。

最简形式（Reasonix，项目根 `./reasonix.toml`）：

```toml
[[plugins]]
name    = "lx-music"
command = 'C:\path\to\mcp\lx-music-mcp.exe'
```

### 3. 前置条件

- LX Music Desktop 正在运行
- 在 LX Music 设置里开启 **Open API 服务**（默认端口 23330；改过端口就用环境变量
  `LX_OPEN_API` 指定完整地址）

---

## 可用工具

| 工具 | 用途 |
|---|---|
| `lx_status` | 当前播放状态 |
| `lx_control` | 播放 / 暂停 / 切歌 / 跳转 / 音量 / 静音 / 收藏 |
| `lx_lyric` | 歌词 |
| `lx_queue` | 播放队列 |
| `lx_search` | 搜索（酷我 / 酷狗 / 咪咕 / QQ音乐 / 网易云） |
| `lx_playlists` | 歌单的列出 / 新建 / 删除 |
| `lx_playlist_songs` | 歌单内歌曲的查看 / 追加 / 移除 / 覆盖 |
| `lx_play` | 播放歌单里的指定歌曲 |
| `lx_batch_add` | 批量找歌并入库（原 `batch_add.js` 的能力） |

---

## 设计要点

### ref 缓存：歌曲信息不出进程

搜索结果的完整 `MusicInfo` 留在服务端内存里，对模型只暴露一个精简列表：

```json
{"ref":"kg_abc_123","name":"夜曲","singer":"周杰伦","source":"kg","quality":["flac","320k"]}
```

播放和入库时，模型传的是 `ref`，服务端自己取回完整对象。

这样做同时解决两个问题：

1. **上下文开销**——50 首歌从约 25 KB 降到约 2 KB。
2. **静默播放失败**——`MusicInfo.meta` 的字段集因音源而异（kg 有 `hash`、
   tx 有 `strMediaMid`、mg 有 `copyrightId`），模型一旦手动裁剪或重构对象，
   歌就会"进了歌单但永远播不出来"。用 ref 之后，这在结构上不可能发生。

`internal/musicstore/store_test.go` 里有一条测试专门守着这个保证：
原始 JSON 字节必须逐字节不变。

### 并发与重试都在服务端

LX 的 Open API 服务端是单线程的，超过约 5 个并发连接会偶发 `ECONNRESET`。
所以限流做在 `lxapi.Client` 里（信号量，5 路），另带一次针对连接重置的重试。

这样无论 host 怎么并发调工具，都不会打爆服务端——不再依赖"叮嘱模型少发并发请求"。

### 高阶批量工具

`lx_batch_add` 把原 `batch_add.js` 的流程搬到服务端：

```
解析输入（歌名 / 歌手 - 歌名 / 歌手 歌名）
  → 并发搜索（5 路）
  → 过滤疑似非原唱的版本（Live / DJ / 翻唱 / 伴奏 / 纯音乐…，可 --all 旁路）
  → 分批入库（30 首一批）
  → 返回结构化汇总（added / failed / uncertain）
```

让模型自己循环调 `lx_search` 几十次，既慢又会撑爆上下文，所以做成一个工具。

---

## 项目结构

```
cmd/lx-music-mcp/      入口：组装 server，走 stdio
internal/lxapi/        LX Open API 客户端（限流、重试、字节透传）
internal/musicstore/   MusicInfo 缓存与音质工具
internal/batch/        批量找歌逻辑（纯逻辑，可单测）
internal/tools/        MCP 工具定义与注册
mcp/                   分发目录：产物 exe + 接入指南
test/e2e.js            端到端冒烟测试（手写 JSON-RPC，无 npm 依赖）
```

---

## 测试

```bash
go test ./...          # 单元测试（含注册 schema 的防 panic 测试）
node test/e2e.js       # 端到端冒烟，不需要 LX 以外的任何依赖
node test/full.js      # 全面功能测试（9 个工具每条分支 + 错误路径 + 并发可靠性）
```

`test/full.js` 是主力验收脚本，覆盖：协议层握手与 schema、`lx_status` 字段过滤、
`lx_control` 全部动作及其非法参数、歌词、队列、5 个音源逐一搜索、搜索各修饰参数、
歌单 CRUD、歌单内歌曲增删改覆盖、批量入库的汇总自洽性，以及连续 20 次调用、
并发 8 请求、并发 6 搜索等可靠性场景。

两个脚本都会创建并自动删除测试歌单（`__MCP-SMOKE-TEST__` / `__MCP-FULL-TEST__`），
但**都会切换当前播放**，跑之前请先知会用户。

> 关于清理顺序：`full.js` 会**先把播放挪出测试歌单、再删除它**。顺序反了会让播放队列
> 指向一个已不存在的歌单，LX 播放器随即卡死——`status` 恒为 `playing` 但 `duration`
> 恒为 0，`play`/`pause` 全部失效，且只能靠重启播放器恢复。这个坑真实踩到过。

---

## 与 lx-music-desktop 的关系

本仓库**只依赖 LX 的 Open API**，不改 LX 一行代码。

lx-music-desktop 那边只需要三处配合（属于后续工作，尚未实施）：

1. `build-config/build-pack.js` 加 `extraFiles`，把本仓库产物复制到绿色版根目录的 `mcp/`
2. `.gitignore` 忽略产物暂存目录
3. 构建前确保产物就位

这样绿色版解压后就是：

```
lx-music-desktop.exe
mcp/
├── lx-music-mcp.exe
└── README.md
```
