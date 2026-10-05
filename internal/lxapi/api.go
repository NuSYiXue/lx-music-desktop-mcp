package lxapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// ---------------------------------------------------------------------------
// 播放器状态
// ---------------------------------------------------------------------------

// Status 返回当前播放状态。filter 为空时用服务端默认字段集。
//
// 可用字段：status name singer albumName duration progress playbackRate
// picUrl lyricLineText lyricLineAllText lyric tlyric rlyric lxlyric collect volume mute
func (c *Client) Status(ctx context.Context, filter string) (json.RawMessage, error) {
	q := url.Values{}
	if filter != "" {
		q.Set("filter", filter)
	}
	return c.do(ctx, request{method: http.MethodGet, path: "/status", query: q})
}

// Lyric 返回当前歌曲的 LRC 歌词纯文本。
func (c *Client) Lyric(ctx context.Context) ([]byte, error) {
	return c.do(ctx, request{method: http.MethodGet, path: "/lyric"})
}

// LyricAll 返回全部歌词类型 {lyric,tlyric,rlyric,lxlyric}。
func (c *Client) LyricAll(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, request{method: http.MethodGet, path: "/lyric-all"})
}

// ---------------------------------------------------------------------------
// 播放控制
// ---------------------------------------------------------------------------

// ControlAction 是播放控制动作。
type ControlAction string

const (
	ActionPlay      ControlAction = "play"
	ActionPause     ControlAction = "pause"
	ActionNext      ControlAction = "next"
	ActionPrev      ControlAction = "prev"
	ActionSeek      ControlAction = "seek"
	ActionVolume    ControlAction = "volume"
	ActionMute      ControlAction = "mute"
	ActionCollect   ControlAction = "collect"
	ActionUncollect ControlAction = "uncollect"
)

// Control 执行一个播放控制动作。
//
// value 的含义随 action 变化：seek 为秒数，volume 为 1-100，其余忽略。
// mute 仅 action=mute 时生效。
func (c *Client) Control(ctx context.Context, action ControlAction, value float64, mute bool) ([]byte, error) {
	switch action {
	case ActionPlay:
		return c.do(ctx, request{method: http.MethodGet, path: "/play"})
	case ActionPause:
		return c.do(ctx, request{method: http.MethodGet, path: "/pause"})
	case ActionNext:
		return c.do(ctx, request{method: http.MethodGet, path: "/skip-next"})
	case ActionPrev:
		return c.do(ctx, request{method: http.MethodGet, path: "/skip-prev"})
	case ActionCollect:
		return c.do(ctx, request{method: http.MethodGet, path: "/collect"})
	case ActionUncollect:
		return c.do(ctx, request{method: http.MethodGet, path: "/uncollect"})
	case ActionSeek:
		q := url.Values{}
		q.Set("offset", strconv.FormatFloat(value, 'f', 3, 64))
		return c.do(ctx, request{method: http.MethodGet, path: "/seek", query: q})
	case ActionVolume:
		q := url.Values{}
		q.Set("volume", strconv.Itoa(int(value)))
		return c.do(ctx, request{method: http.MethodGet, path: "/volume", query: q})
	case ActionMute:
		q := url.Values{}
		q.Set("mute", strconv.FormatBool(mute))
		return c.do(ctx, request{method: http.MethodGet, path: "/mute", query: q})
	default:
		return nil, fmt.Errorf("未知的播放控制动作 %q", action)
	}
}

// ---------------------------------------------------------------------------
// 搜索
// ---------------------------------------------------------------------------

// SearchParams 是搜索参数。
type SearchParams struct {
	Keyword string // 必填
	Source  string // kw kg mg tx wy，留空表示聚合搜索
	Page    int
	Limit   int
	Dedup   bool
	// MatchSinger 指定后期望的歌手，用于排序（exact/partial/none）。
	MatchSinger string
	// MinQuality 音质下限：128k 320k flac flac24bit。
	MinQuality string
	// Order 音源优先级，逗号分隔，留空用用户设置。
	Order string
}

// Search 调用 /search。
func (c *Client) Search(ctx context.Context, p SearchParams) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("keyword", p.Keyword)
	if p.Source != "" {
		q.Set("source", p.Source)
	}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Dedup {
		q.Set("dedup", "true")
	}
	if p.MatchSinger != "" {
		q.Set("matchSinger", p.MatchSinger)
	}
	if p.MinQuality != "" {
		q.Set("minQuality", p.MinQuality)
	}
	if p.Order != "" {
		q.Set("order", p.Order)
	}
	return c.do(ctx, request{method: http.MethodGet, path: "/search", query: q})
}

// ---------------------------------------------------------------------------
// 歌单
// ---------------------------------------------------------------------------

// Playlists 返回全部用户歌单。
func (c *Client) Playlists(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, request{method: http.MethodGet, path: "/playlists"})
}

// PlaylistCreate 新建歌单，返回 {"id":"userlist_xxx","name":"..."}。
func (c *Client) PlaylistCreate(ctx context.Context, name string) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/playlist/create",
		body:   map[string]string{"name": name},
	})
}

// PlaylistSongs 读取歌单内的歌曲，返回 {"listId":"..","songs":[MusicInfo...]}。
func (c *Client) PlaylistSongs(ctx context.Context, listID string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("listId", listID)
	return c.do(ctx, request{method: http.MethodGet, path: "/playlist/list", query: q})
}

// PlaylistAdd 追加歌曲到歌单。
//
// musicInfos 必须是完整 MusicInfo 的原始 JSON，直接取自搜索或歌单列表。
func (c *Client) PlaylistAdd(ctx context.Context, listID string, musicInfos []json.RawMessage) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/playlist/add",
		body:   playlistMusicsBody{ListID: listID, MusicInfos: musicInfos},
	})
}

// PlaylistOverwrite 整表覆盖歌单内容。
func (c *Client) PlaylistOverwrite(ctx context.Context, listID string, musicInfos []json.RawMessage) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/playlist/overwrite",
		body:   playlistMusicsBody{ListID: listID, MusicInfos: musicInfos},
	})
}

// PlaylistRemove 删除歌单（传歌单 id）。
func (c *Client) PlaylistRemove(ctx context.Context, ids []string) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/playlist/remove",
		body:   idsBody{IDs: ids},
	})
}

// PlaylistRemoveSongs 从歌单里移除歌曲（传歌曲 id）。
func (c *Client) PlaylistRemoveSongs(ctx context.Context, listID string, ids []string) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/playlist/songs",
		body:   removeSongsBody{ListID: listID, IDs: ids},
	})
}

// ---------------------------------------------------------------------------
// 播放队列
// ---------------------------------------------------------------------------

// PlayerPlay 播放指定歌单里的指定歌曲。
//
// musicInfo 必须是完整 MusicInfo 的原始 JSON。
func (c *Client) PlayerPlay(ctx context.Context, listID string, musicInfo json.RawMessage) (json.RawMessage, error) {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/player/play",
		body:   playBody{ListID: listID, MusicInfo: musicInfo},
	})
}

// PlayerQueue 返回当前播放队列 {listId,currentIndex,count,list}。
func (c *Client) PlayerQueue(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, request{method: http.MethodGet, path: "/player/list"})
}

// ---------------------------------------------------------------------------
// 请求体
//
// 注意 MusicInfo 一律用 json.RawMessage：序列化时直接输出原始字节，
// 不做任何解析重组，这样 meta 里的源特有字段一个都不会丢。
// ---------------------------------------------------------------------------

type playlistMusicsBody struct {
	ListID     string            `json:"listId"`
	MusicInfos []json.RawMessage `json:"musicInfos"`
}

type playBody struct {
	ListID    string          `json:"listId"`
	MusicInfo json.RawMessage `json:"musicInfo"`
}

type idsBody struct {
	IDs []string `json:"ids"`
}

type removeSongsBody struct {
	ListID string   `json:"listId"`
	IDs    []string `json:"ids"`
}
