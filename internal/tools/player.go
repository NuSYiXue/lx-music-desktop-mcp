package tools

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
)

// 注意：以下所有 jsonschema 标签的描述里都不能出现英文逗号或等号。
// google/jsonschema-go 会把它们解析成选项对，AddTool 会直接 panic。

// ---------------------------------------------------------------------------
// lx_status
// ---------------------------------------------------------------------------

type statusInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"可选。逗号分隔的字段过滤。默认返回 status、name、singer、albumName、lyricLineText、duration、progress、playbackRate。还可选 picUrl、collect、volume、mute、lyric、tlyric、rlyric、lxlyric"`
}

func registerStatus(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_status",
		Description: "获取 LX Music 当前播放状态：正在播放的歌曲、播放/暂停、进度、音量等。" +
			"返回 JSON。想判断\"在放什么\"优先用这个，而不是 lx_queue。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in statusInput) (*mcp.CallToolResult, any, error) {
		raw, err := d.API.Status(ctx, in.Filter)
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(raw))
	})
}

// ---------------------------------------------------------------------------
// lx_control
// ---------------------------------------------------------------------------

type controlInput struct {
	Action string  `json:"action" jsonschema:"要执行的动作。play 播放或继续、pause 暂停、next 下一首、prev 上一首、seek 跳转到指定秒数、volume 设置音量、mute 静音开关、collect 收藏当前歌曲、uncollect 取消收藏"`
	Value  float64 `json:"value,omitempty" jsonschema:"仅 seek 和 volume 需要。seek 传秒数，volume 传 1 到 100"`
	Mute   bool    `json:"mute,omitempty" jsonschema:"仅 action 取 mute 时使用。true 静音、false 取消静音"`
}

func registerControl(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_control",
		Description: "控制播放：播放、暂停、切歌、跳转进度、调音量、静音、收藏/取消收藏。" +
			"跳转和调音量通过 value 传参，静音开关通过 mute 传参。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in controlInput) (*mcp.CallToolResult, any, error) {
		action := lxapi.ControlAction(in.Action)
		var value float64
		switch action {
		case lxapi.ActionSeek:
			if in.Value <= 0 {
				return nil, nil, errors.New("seek 需要 value，表示跳转到的秒数（大于 0）")
			}
			value = in.Value
		case lxapi.ActionVolume:
			if in.Value < 1 || in.Value > 100 {
				return nil, nil, errors.New("volume 需要 value，取值 1 到 100")
			}
			value = in.Value
		}

		if _, err := d.API.Control(ctx, action, value, in.Mute); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"ok": true, "action": in.Action})
	})
}

// ---------------------------------------------------------------------------
// lx_lyric
// ---------------------------------------------------------------------------

type lyricInput struct {
	Type string `json:"type,omitempty" jsonschema:"current 返回当前 LRC 歌词纯文本（默认）；all 返回全部歌词类型 lyric、tlyric、rlyric、lxlyric"`
}

func registerLyric(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "lx_lyric",
		Description: "获取当前歌曲的歌词。默认返回 LRC 纯文本，type 取 all 时返回全部歌词类型。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in lyricInput) (*mcp.CallToolResult, any, error) {
		if in.Type == "all" {
			raw, err := d.API.LyricAll(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(string(raw))
		}
		body, err := d.API.Lyric(ctx)
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(body))
	})
}

// ---------------------------------------------------------------------------
// lx_queue
// ---------------------------------------------------------------------------

func registerQueue(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "lx_queue",
		Description: "获取当前播放队列。返回 listId、currentIndex、count、list；" +
			"list 里 currentIndex 位置是当前歌曲，之后的是待播。每首歌都带 ref，可用于 lx_play。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		raw, err := d.API.PlayerQueue(ctx)
		if err != nil {
			return nil, nil, err
		}
		return textResult(string(raw))
	})
}
