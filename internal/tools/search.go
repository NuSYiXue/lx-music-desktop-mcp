package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
)

// 注意：jsonschema 标签的描述里不能出现英文逗号或等号，否则 AddTool 会 panic。

type searchInput struct {
	Keyword     string `json:"keyword" jsonschema:"搜索关键词，可以是歌名、歌手名或两者组合"`
	Source      string `json:"source,omitempty" jsonschema:"限定单一音源。kw 酷我、kg 酷狗、mg 咪咕、tx QQ音乐、wy 网易云。留空则按用户设置的优先级聚合搜索多个音源"`
	Limit       int    `json:"limit,omitempty" jsonschema:"每页条数，默认 20"`
	Page        int    `json:"page,omitempty" jsonschema:"页码，默认 1"`
	Dedup       bool   `json:"dedup,omitempty" jsonschema:"是否去重。同歌名同歌手只保留音质最好的一条；批量找歌时建议设为 true"`
	MatchSinger string `json:"matchSinger,omitempty" jsonschema:"期望的歌手名。传入后结果按歌手匹配度排序并标注 matchLevel（exact 原唱、partial 合作或 feat、none 翻唱）"`
	MinQuality  string `json:"minQuality,omitempty" jsonschema:"音质下限，取 128k、320k、flac、flac24bit 之一。低于此音质的结果会被过滤掉"`
	Order       string `json:"order,omitempty" jsonschema:"音源优先级，用逗号分隔，如 kg、kw、mg、tx、wy。留空用用户设置"`
}

// searchResponse 对应 /search 的原始响应。
type searchResponse struct {
	List    []json.RawMessage `json:"list"`
	Total   int               `json:"total"`
	AllPage int               `json:"allPage"`
	Page    int               `json:"page"`
	Limit   int               `json:"limit"`
}

// searchOutput 是对模型暴露的精简结果。
type searchOutput struct {
	Total   int       `json:"total"`
	Page    int       `json:"page"`
	Limit   int       `json:"limit"`
	AllPage int       `json:"allPage"`
	Count   int       `json:"count"`
	Hint    string    `json:"hint,omitempty"`
	Songs   []songRef `json:"songs"`
}

func registerSearch(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_search",
		Description: "搜索歌曲，跨酷我/酷狗/咪咕/QQ音乐/网易云多个音源。" +
			"返回精简列表，每条带一个 ref；把 ref 交给 lx_playlist_songs 或 lx_play 即可操作，" +
			"不要试图自己构造歌曲对象。批量找歌请优先用 lx_batch_add。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.Keyword) == "" {
			return nil, nil, errors.New("keyword 不能为空")
		}

		raw, err := d.API.Search(ctx, lxapi.SearchParams{
			Keyword:     in.Keyword,
			Source:      in.Source,
			Page:        in.Page,
			Limit:       in.Limit,
			Dedup:       in.Dedup,
			MatchSinger: in.MatchSinger,
			MinQuality:  in.MinQuality,
			Order:       in.Order,
		})
		if err != nil {
			return nil, nil, err
		}

		var resp searchResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, nil, fmt.Errorf("解析搜索结果失败：%w", err)
		}

		items := d.Store.PutAll(resp.List)
		refs := toSongRefs(items)
		levels := extractMatchLevels(resp.List)
		for i := range refs {
			refs[i].MatchLevel = levels[refs[i].Ref]
		}

		out := searchOutput{
			Total:   resp.Total,
			Page:    resp.Page,
			Limit:   resp.Limit,
			AllPage: resp.AllPage,
			Count:   len(refs),
			Songs:   refs,
		}
		if len(refs) == 0 {
			out.Hint = "没有结果。可以试试放宽关键词、换 source，或去掉 minQuality 限制。"
		}
		return jsonResult(out)
	})
}

// extractMatchLevels 取出服务端在 matchSinger 模式下写入的 _matchLevel。
func extractMatchLevels(raws []json.RawMessage) map[string]string {
	type wire struct {
		ID         string `json:"id"`
		MatchLevel string `json:"_matchLevel"`
	}
	levels := make(map[string]string, len(raws))
	for _, raw := range raws {
		var w wire
		if err := json.Unmarshal(raw, &w); err != nil || w.ID == "" || w.MatchLevel == "" {
			continue
		}
		levels[w.ID] = w.MatchLevel
	}
	return levels
}
