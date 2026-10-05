package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Register 把所有工具注册到 MCP server。
func Register(s *mcp.Server, d *Deps) {
	// 播放器
	registerStatus(s, d)
	registerControl(s, d)
	registerLyric(s, d)
	registerQueue(s, d)

	// 搜索
	registerSearch(s, d)

	// 歌单
	registerPlaylists(s, d)
	registerPlaylistSongs(s, d)
	registerPlay(s, d)

	// 高阶批量
	registerBatchAdd(s, d)
}

// Instructions 是随 server 一起交给 host 的整体使用说明。
//
// 放在这里而不是每个工具的 description 里，是因为它讲的是"工具之间怎么选"，
// 属于会话级上下文；工具自身的用法仍写在各自的 description 中。
const Instructions = `你是 LX Music Desktop 的控制器，通过以下工具操作用户本机的音乐播放器。

选择工具的原则：
- 想知道"现在在放什么" → lx_status
- 想找歌 → lx_search；用户一次报出多首歌名 → lx_batch_add（它会自己并发搜索、过滤非原唱版本、分批入库）
- 想整理歌单 → lx_playlists 管歌单本身，lx_playlist_songs 管歌单里的歌
- 想播放 → lx_play，再用 lx_status 确认

两条硬性约定：
1. 歌曲一律用 ref 引用。ref 来自 lx_search / lx_playlist_songs(list) / lx_queue 的返回，
   不要自己拼装或裁剪歌曲对象——那会导致歌曲进了歌单却永远播不出来。
2. 播放是异步的，发出 lx_play 后需要几秒才出声。不要立刻查状态就判定失败。

如果工具报错提示连不上 LX Music，请告诉用户：需要先启动 LX Music，
并在设置里开启 Open API 服务。`
