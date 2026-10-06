// Package tools 把 LX Open API 封装成一组 MCP 工具。
//
// 工具分两层：
//
//   - 原语工具：状态、播放控制、歌词、队列、搜索、歌单增删改查
//   - 高阶工具：lx_batch_add，一次完成"建歌单 + 批量搜索 + 过滤 + 入库"
//
// 高阶工具的存在是刻意的：让模型自己循环调用几十次搜索，
// 既会撑爆上下文，也会失去"最多 5 并发"这类保护。
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/musicstore"
)

// Deps 是所有工具共享的依赖。
type Deps struct {
	API    *lxapi.Client
	Store  *musicstore.Store
	Volume *VolumeTracker
}

// jsonResult 把值序列化成紧凑 JSON 文本返回。
//
// 用紧凑 JSON 而非缩进：工具返回值会整体进入模型上下文，省 token 优先。
// 只放文本、不依赖 structuredContent，是为了在所有 MCP host 上表现一致。
func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化工具返回值失败：%w", err)
	}
	return textResult(string(b))
}

// textResult 返回纯文本结果。
func textResult(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}, nil, nil
}

// songRef 是对模型暴露的精简歌曲信息。
//
// 只有 ref 是"可操作"的：播放和入库都传它，完整 MusicInfo 留在服务端。
type songRef struct {
	Ref      string   `json:"ref"`
	Name     string   `json:"name"`
	Singer   string   `json:"singer"`
	Source   string   `json:"source"`
	Interval string   `json:"interval,omitempty"`
	Quality  []string `json:"quality,omitempty"`
	// MatchLevel 仅在搜索结果带 matchSinger 时出现：exact / partial / none。
	MatchLevel string `json:"matchLevel,omitempty"`
}

// toSongRef 把缓存条目转成对模型暴露的精简结构。
func toSongRef(item *musicstore.Item) songRef {
	return songRef{
		Ref:      item.ID,
		Name:     item.Name,
		Singer:   item.Singer,
		Source:   item.Source,
		Interval: item.Interval,
		Quality:  item.Quality,
	}
}

// toSongRefs 批量转换。
func toSongRefs(items []*musicstore.Item) []songRef {
	out := make([]songRef, 0, len(items))
	for _, item := range items {
		out = append(out, toSongRef(item))
	}
	return out
}
