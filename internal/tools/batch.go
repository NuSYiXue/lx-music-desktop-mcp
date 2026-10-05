package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/batch"
)

// 注意：jsonschema 标签的描述里不能出现英文逗号或等号，否则 AddTool 会 panic。

// ---------------------------------------------------------------------------
// lx_batch_add —— 高阶批量工具
//
// 这是原 skill 方案里 batch_add.js 的等价物，但逻辑在服务端内部完成，
// 模型只发一次调用、只收一份汇总。这样既不会撑爆上下文，
// 也不会因为模型自己循环而丢掉并发保护。
// ---------------------------------------------------------------------------

type batchAddInput struct {
	PlaylistName string   `json:"playlistName" jsonschema:"目标歌单名。同名歌单已存在就直接追加进去，不存在则新建"`
	Songs        []string `json:"songs" jsonschema:"待添加的歌曲列表。每条支持『歌名』『歌手 - 歌名』『歌手 歌名』三种写法；单条内用逗号分隔多条也可以"`
	IncludeAll   bool     `json:"includeAll,omitempty" jsonschema:"默认会跳过翻唱、Live、DJ、伴奏、纯音乐等疑似非原唱的版本；设为 true 则不过滤"`
	MaxPerQuery  int      `json:"maxPerQuery,omitempty" jsonschema:"每条查询取多少条候选用于挑选，默认 10"`
}

type playlistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Created 为 true 表示本次新建，false 表示复用了同名歌单。
	Created bool `json:"created"`
}

type batchSummary struct {
	Total     int `json:"total"`
	Found     int `json:"found"`
	Added     int `json:"added"`
	Failed    int `json:"failed"`
	Uncertain int `json:"uncertain"`
}

type batchAddedItem struct {
	Query  string `json:"query"`
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Singer string `json:"singer"`
	Source string `json:"source"`
	Level  string `json:"matchLevel,omitempty"`
}

type batchFailedItem struct {
	Query  string `json:"query"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type batchUncertainItem struct {
	Query      string   `json:"query"`
	Picked     string   `json:"picked"`
	Singer     string   `json:"singer"`
	Source     string   `json:"source"`
	MatchLevel string   `json:"matchLevel,omitempty"`
	Issues     []string `json:"issues"`
}

type batchAddOutput struct {
	Playlist  playlistRef          `json:"playlist"`
	Summary   batchSummary         `json:"summary"`
	Added     []batchAddedItem     `json:"added"`
	Failed    []batchFailedItem    `json:"failed"`
	Uncertain []batchUncertainItem `json:"uncertain"`
	Note      string               `json:"note,omitempty"`
}

func registerBatchAdd(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_batch_add",
		Description: "批量按歌名找歌并加进歌单：一次完成「找/建歌单 → 并发搜索 → 过滤非原唱版本 → 分批入库」，最后返回汇总。" +
			"用户一次给出多首歌时用这个；不要在循环里反复调 lx_search，那样既慢又浪费上下文。" +
			"返回里的 uncertain 列表是「找到了但可能不是原唱」的歌，值得向用户提一句。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in batchAddInput) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(in.PlaylistName)
		if name == "" {
			return nil, nil, errors.New("playlistName 不能为空")
		}
		if len(in.Songs) == 0 {
			return nil, nil, errors.New("songs 不能为空")
		}

		// 每条输入内部还可能用逗号分隔多条，统一展开。
		queries := make([]batch.Query, 0, len(in.Songs))
		for _, raw := range in.Songs {
			queries = append(queries, batch.ParseList(raw)...)
		}
		if len(queries) == 0 {
			return nil, nil, errors.New("没有解析出任何有效歌曲")
		}

		// 1. 找到或新建歌单
		listID, created, err := d.ensurePlaylist(ctx, name)
		if err != nil {
			return nil, nil, err
		}

		// 2. 并发搜索（并发度与限流都在下层控制）
		runner := &batch.Runner{
			API:         d.API,
			Store:       d.Store,
			SearchLimit: in.MaxPerQuery,
			IncludeAll:  in.IncludeAll,
		}
		results := runner.Run(ctx, queries)

		// 3. 汇总搜索结果，分出「命中 / 失败 / 存疑」
		failed := make([]batchFailedItem, 0)
		uncertain := make([]batchUncertainItem, 0)
		pendingRaw := make([]json.RawMessage, 0, len(results))
		pendingItem := make([]batchAddedItem, 0, len(results))

		for _, r := range results {
			if !r.Found || r.Pick == nil {
				detail := ""
				if len(r.Issues) > 0 {
					detail = r.Issues[0]
				}
				failed = append(failed, batchFailedItem{
					Query:  r.Query,
					Reason: r.Reason,
					Detail: detail,
				})
				continue
			}
			pendingRaw = append(pendingRaw, r.Pick.Raw)
			pendingItem = append(pendingItem, batchAddedItem{
				Query:  r.Query,
				Ref:    r.Pick.ID,
				Name:   r.Pick.Name,
				Singer: r.Pick.Singer,
				Source: r.Pick.Source,
				Level:  r.MatchLevel,
			})
			if len(r.Issues) > 0 {
				uncertain = append(uncertain, batchUncertainItem{
					Query:      r.Query,
					Picked:     r.Pick.Name,
					Singer:     r.Pick.Singer,
					Source:     r.Pick.Source,
					MatchLevel: r.MatchLevel,
					Issues:     r.Issues,
				})
			}
		}

		// 4. 分批入库，一次 30 首
		added := make([]batchAddedItem, 0, len(pendingRaw))
		for start := 0; start < len(pendingRaw); start += batch.AddBatchSize {
			end := min(start+batch.AddBatchSize, len(pendingRaw))
			if _, err := d.API.PlaylistAdd(ctx, listID, pendingRaw[start:end]); err != nil {
				return nil, nil, fmt.Errorf("入库失败（此前已成功添加 %d 首）：%w", len(added), err)
			}
			added = append(added, pendingItem[start:end]...)
		}

		out := batchAddOutput{
			Playlist: playlistRef{ID: listID, Name: name, Created: created},
			Summary: batchSummary{
				Total:     len(queries),
				Found:     len(pendingRaw),
				Added:     len(added),
				Failed:    len(failed),
				Uncertain: len(uncertain),
			},
			Added:     added,
			Failed:    failed,
			Uncertain: uncertain,
		}
		if len(failed) > 0 {
			out.Note = "有歌曲没找到。可以改用更完整的『歌手 - 歌名』写法重试这几首。"
		}
		return jsonResult(out)
	})
}

// ensurePlaylist 按名字查找歌单，没有就新建。
func (d *Deps) ensurePlaylist(ctx context.Context, name string) (id string, created bool, err error) {
	raw, err := d.API.Playlists(ctx)
	if err != nil {
		return "", false, err
	}

	var lists []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &lists); err != nil {
		return "", false, fmt.Errorf("解析歌单列表失败：%w", err)
	}
	for _, l := range lists {
		if l.Name == name && l.ID != "" {
			return l.ID, false, nil
		}
	}

	body, err := d.API.PlaylistCreate(ctx, name)
	if err != nil {
		return "", false, err
	}
	var createdInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &createdInfo); err != nil || createdInfo.ID == "" {
		return "", false, fmt.Errorf("创建歌单失败，响应里没有 id：%s", string(body))
	}
	return createdInfo.ID, true, nil
}
