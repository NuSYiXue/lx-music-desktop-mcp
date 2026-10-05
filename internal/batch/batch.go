// Package batch 实现"按歌名批量找歌并入库"的高阶流程。
//
// 这段逻辑迁移自原 skill 方案里的 batch_add.js，行为刻意保持一致：
// 每条查询并发度受限、过滤疑似非原唱版本、分批入库、输出结构化汇总。
//
// 放在独立包而不是 MCP 工具里，是因为它是纯逻辑，可以脱离 MCP 单测。
package batch

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/musicstore"
)

// 默认参数，与 batch_add.js 对齐。
const (
	DefaultConcurrency = 5
	DefaultSearchLimit = 10
	// AddBatchSize 是一次入库的歌曲条数。
	AddBatchSize = 30
)

// badPatterns 是疑似非原唱版本的过滤词表，取自原脚本，区分大小写以保持一致。
var badPatterns = regexp.MustCompile(`Live|DJ|R&B版|cover|翻唱|新版|合唱|空灵鼓|练习曲|Remix|Mix|片段|现场|串烧|DJ版|慢摇|演唱会|伴奏|纯音乐`)

// Query 是一条待搜索的歌曲描述。
type Query struct {
	Raw    string // 原始输入，如 "周杰伦 - 夜曲"
	Song   string // 歌名
	Artist string // 歌手，可能为空
}

// IsSuspect 判断歌名或歌手是否命中"疑似非原唱"词表。
func IsSuspect(name, singer string) bool {
	return badPatterns.MatchString(name) || badPatterns.MatchString(singer)
}

// ParseQuery 解析单条输入，支持三种写法：
//
//	"周杰伦 - 夜曲" → artist=周杰伦, song=夜曲
//	"周杰伦 夜曲"   → artist=周杰伦, song=夜曲
//	"夜曲"          → artist="",     song=夜曲
//
// 无法解析出内容时返回 ok=false。
func ParseQuery(raw string) (Query, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Query{}, false
	}
	q := Query{Raw: s}

	// "歌手 - 歌名"（支持 -, –, —）
	if m := splitArtistSong.FindStringSubmatch(s); m != nil {
		q.Artist = strings.TrimSpace(m[1])
		q.Song = strings.TrimSpace(m[2])
		if q.Song != "" {
			return q, true
		}
	}

	// "歌手 歌名"：恰好两段，且第二段有一定长度
	if parts := strings.Fields(s); len(parts) == 2 && len([]rune(parts[1])) >= 2 {
		q.Artist = parts[0]
		q.Song = parts[1]
		return q, true
	}

	q.Song = s
	return q, true
}

var splitArtistSong = regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+)$`)

// ParseList 按逗号或换行拆分输入，逐条解析，丢弃空项。
func ParseList(raw string) []Query {
	sep := regexp.MustCompile(`[，,\n]`)
	queries := make([]Query, 0, 16)
	for _, part := range sep.Split(raw, -1) {
		if q, ok := ParseQuery(part); ok {
			queries = append(queries, q)
		}
	}
	return queries
}

// Result 是单条查询的结果。
type Result struct {
	Query      string
	Found      bool
	Reason     string // 未命中时的原因：no_results / error
	Pick       *musicstore.Item
	MatchLevel string   // exact / partial / none / 空（未指定歌手时）
	Issues     []string // 例如 singer_none，提示可能不是原唱
}

// Runner 执行批量搜索。
type Runner struct {
	API         *lxapi.Client
	Store       *musicstore.Store
	Concurrency int
	SearchLimit int
	// IncludeAll 为 true 时不过滤疑似非原唱版本。
	IncludeAll bool
}

func (r *Runner) concurrency() int {
	if r.Concurrency > 0 {
		return r.Concurrency
	}
	return DefaultConcurrency
}

func (r *Runner) searchLimit() int {
	if r.SearchLimit > 0 {
		return r.SearchLimit
	}
	return DefaultSearchLimit
}

// Run 并发搜索全部查询，返回结果（顺序与输入一致）。
//
// 这里只控制"同时有多少条查询在跑"；对服务端的连接限流由 lxapi.Client 统一负责。
func (r *Runner) Run(ctx context.Context, queries []Query) []Result {
	results := make([]Result, len(queries))
	sem := make(chan struct{}, r.concurrency())
	var wg sync.WaitGroup

	for i, q := range queries {
		wg.Add(1)
		go func(i int, q Query) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = Result{Query: q.Raw, Reason: "error", Issues: []string{ctx.Err().Error()}}
				return
			}
			defer func() { <-sem }()
			results[i] = r.searchOne(ctx, q)
		}(i, q)
	}

	wg.Wait()
	return results
}

// searchOne 搜索一条查询并挑出最合适的一首。
func (r *Runner) searchOne(ctx context.Context, q Query) Result {
	res := Result{Query: q.Raw}

	raw, err := r.API.Search(ctx, lxapi.SearchParams{
		Keyword:     q.Song,
		Limit:       r.searchLimit(),
		Dedup:       true,
		MatchSinger: q.Artist,
	})
	if err != nil {
		res.Reason = "error"
		res.Issues = []string{err.Error()}
		return res
	}

	var resp struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		res.Reason = "error"
		res.Issues = []string{"解析搜索结果失败：" + err.Error()}
		return res
	}
	if len(resp.List) == 0 {
		res.Reason = "no_results"
		return res
	}

	// 全部入库，同时按原脚本的顺序挑第一首合格的候选。
	items := r.Store.PutAll(resp.List)
	if len(items) == 0 {
		res.Reason = "no_results"
		return res
	}

	levels := extractMatchLevels(resp.List)

	var pick *musicstore.Item
	if !r.IncludeAll {
		for _, item := range items {
			if IsSuspect(item.Name, item.Singer) {
				continue
			}
			pick = item
			break
		}
	}
	if pick == nil {
		// 全被过滤掉（或未启用过滤）时退回第一条，与原脚本一致。
		pick = items[0]
	}

	res.Found = true
	res.Pick = pick
	res.MatchLevel = levels[pick.ID]
	if q.Artist != "" && res.MatchLevel != "" && res.MatchLevel != "exact" {
		res.Issues = append(res.Issues, "singer_"+res.MatchLevel)
	}
	return res
}

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

// SearchDelay 是批次之间等待的时间，用于进一步降低瞬时并发压力。
const SearchDelay = 100 * time.Millisecond
