package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 注意：jsonschema 标签的描述里不能出现英文逗号或等号，否则 AddTool 会 panic。

// ---------------------------------------------------------------------------
// lx_playlists —— 歌单级操作
// ---------------------------------------------------------------------------

type playlistsInput struct {
	Action string   `json:"action" jsonschema:"list 列出所有用户歌单、create 新建歌单、remove 删除歌单"`
	Name   string   `json:"name,omitempty" jsonschema:"create 时必填，新歌单的名字"`
	IDs    []string `json:"ids,omitempty" jsonschema:"remove 时必填，要删除的歌单 id 列表，形如 userlist_xxx"`
}

func registerPlaylists(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_playlists",
		Description: "管理歌单本身：列出全部歌单、新建歌单、删除歌单。" +
			"要操作歌单里的歌曲请用 lx_playlist_songs。新建歌单前建议先 list 看看是否已存在同名歌单，避免重复创建。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playlistsInput) (*mcp.CallToolResult, any, error) {
		switch in.Action {
		case "list", "":
			raw, err := d.API.Playlists(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(raw))

		case "create":
			name := strings.TrimSpace(in.Name)
			if name == "" {
				return nil, nil, errors.New("create 需要 name")
			}
			raw, err := d.API.PlaylistCreate(ctx, name)
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(raw))

		case "remove":
			if len(in.IDs) == 0 {
				return nil, nil, errors.New("remove 需要 ids")
			}
			raw, err := d.API.PlaylistRemove(ctx, in.IDs)
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(raw))

		default:
			return nil, nil, fmt.Errorf("未知的 action %q，可用：list / create / remove", in.Action)
		}
	})
}

// ---------------------------------------------------------------------------
// lx_playlist_songs —— 歌单内歌曲操作
// ---------------------------------------------------------------------------

type playlistSongsInput struct {
	Action  string   `json:"action" jsonschema:"list 查看歌单里的歌曲、add 向歌单追加歌曲、remove 从歌单移除歌曲、overwrite 用给出的歌曲替换整个歌单"`
	ListID  string   `json:"listId" jsonschema:"目标歌单 id，形如 userlist_xxx"`
	Refs    []string `json:"refs,omitempty" jsonschema:"add 或 overwrite 时必填。歌曲 ref 列表，来自 lx_search、lx_playlist_songs 的 list 动作或 lx_queue 的结果"`
	SongIDs []string `json:"songIds,omitempty" jsonschema:"remove 时必填。要从歌单移除的歌曲 id 列表"`
}

type playlistSongsResponse struct {
	ListID string            `json:"listId"`
	Songs  []json.RawMessage `json:"songs"`
}

type playlistSongsOutput struct {
	ListID string    `json:"listId"`
	Count  int       `json:"count"`
	Hint   string    `json:"hint,omitempty"`
	Songs  []songRef `json:"songs"`
}

func registerPlaylistSongs(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_playlist_songs",
		Description: "操作歌单里的歌曲：查看、追加、移除、整体替换。" +
			"传歌曲一律用 ref（来自搜索或本工具的 list 结果），不要自己拼歌曲对象。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playlistSongsInput) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.ListID) == "" {
			return nil, nil, errors.New("listId 不能为空")
		}

		switch in.Action {
		case "list", "":
			raw, err := d.API.PlaylistSongs(ctx, in.ListID)
			if err != nil {
				return nil, nil, err
			}
			var resp playlistSongsResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				return nil, nil, fmt.Errorf("解析歌单内容失败：%w", err)
			}
			items := d.Store.PutAll(resp.Songs)
			out := playlistSongsOutput{
				ListID: in.ListID,
				Count:  len(items),
				Songs:  toSongRefs(items),
			}
			if len(items) == 0 {
				out.Hint = "这个歌单是空的。"
			}
			return jsonResult(out)

		case "add", "overwrite":
			if len(in.Refs) == 0 {
				return nil, nil, fmt.Errorf("%s 需要 refs", in.Action)
			}
			items, missing := d.Store.GetMany(in.Refs)
			if len(items) == 0 {
				return nil, nil, errors.New(
					"这些 ref 全都失效了（服务可能重启过）。请用 lx_search 重新搜索，再传入新的 ref",
				)
			}
			raws := make([]json.RawMessage, 0, len(items))
			for _, item := range items {
				raws = append(raws, item.Raw)
			}
			var (
				raw json.RawMessage
				err error
			)
			if in.Action == "add" {
				raw, err = d.API.PlaylistAdd(ctx, in.ListID, raws)
			} else {
				raw, err = d.API.PlaylistOverwrite(ctx, in.ListID, raws)
			}
			if err != nil {
				return nil, nil, err
			}
			if len(missing) > 0 {
				return jsonResult(map[string]any{
					"ok":        true,
					"applied":   len(items),
					"staleRefs": missing,
					"warning":   "部分 ref 已失效，未参与本次操作；需要的话请重新搜索后再补上",
					"apiResult": json.RawMessage(raw),
				})
			}
			return textResult(string(raw))

		case "remove":
			if len(in.SongIDs) == 0 {
				return nil, nil, errors.New("remove 需要 songIds")
			}
			raw, err := d.API.PlaylistRemoveSongs(ctx, in.ListID, in.SongIDs)
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(raw))

		default:
			return nil, nil, fmt.Errorf("未知的 action %q，可用：list / add / remove / overwrite", in.Action)
		}
	})
}

// ---------------------------------------------------------------------------
// lx_play —— 播放指定歌曲
// ---------------------------------------------------------------------------

type playInput struct {
	ListID string `json:"listId" jsonschema:"歌曲所在歌单的 id。若只想播搜索结果里的歌，可以先用 lx_playlist_songs 的 add 动作把它加进某个歌单"`
	Ref    string `json:"ref" jsonschema:"要播放的歌曲 ref"`
}

func registerPlay(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_play",
		Description: "播放某个歌单里的指定歌曲。" +
			"播放会加载完整歌曲信息，需要几秒才开始出声，这是正常的，不要立刻去查状态就判定失败。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playInput) (*mcp.CallToolResult, any, error) {
		raw, ok := d.Store.GetRaw(in.Ref)
		if !ok {
			return nil, nil, fmt.Errorf(
				"ref %q 不在缓存里（可能是服务重启后失效，或该 ref 来自别的会话）。"+
					"请先用 lx_search 或 lx_playlist_songs 的 list 动作拿到有效 ref", in.Ref,
			)
		}
		res, err := d.API.PlayerPlay(ctx, in.ListID, raw)
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(res))
	})
}
