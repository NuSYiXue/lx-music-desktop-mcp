package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NuSYiXue/lx-music-desktop-mcp/internal/lxapi"
)

// 音量取值范围。与 LX Open API 的 0-100 分制一致；
// 下界取 1 而不是 0 —— “没声音”用 mute 表达，不用 0 音量。
const (
	minVolume = 1
	maxVolume = 100
)

// volume_up / volume_down 是本 server 自己实现的语义动作：读当前音量、
// 加减、再写回。它们不对应任何 LX 端点，所以不放进 lxapi.ControlAction。
const (
	actionVolumeUp   = "volume_up"
	actionVolumeDown = "volume_down"
)

// DefaultVolumeStep 是 volume_up / volume_down 未传 value 时的默认步长。
//
// 取 4 是为了与 LX 自己的音量快捷键一致：renderer 侧的
// handleSetVolumeUp / handleSetVolumeDown 用的 step 是 0.04，
// 换算到这个 0-100 分制就是 4。
const DefaultVolumeStep = 4

// volumeTrackerTTL 是本地音量记忆的有效期。
//
// 为什么需要记忆：音量在 renderer 侧生效、再由它回传状态，实测有 1~2 秒延迟
// （见 mcp/README.md 的「调用播放控制后，状态没有立刻变」）。相对增减若每次都
// 去读 LX，连续调用会读到旧值、把增量丢掉（“调大三次”只涨一次）。
// 所以在 TTL 内优先用本地记的值；超过 TTL 就回落到读 LX ——
// 这样用户中途手动拖了滑块，下一次相对增减依然基于真实值。
const volumeTrackerTTL = 5 * time.Second

// VolumeTracker 记住最近一次由本 server 设定的音量。
//
// 它只作为「相对增减的基准值」，不参与 lx_status 的返回：
// lx_status 始终透传 LX 的真实状态，不受这里影响。
type VolumeTracker struct {
	mu    sync.Mutex
	value int
	at    time.Time
}

// NewVolumeTracker 建一个空的音量记忆。
func NewVolumeTracker() *VolumeTracker { return &VolumeTracker{} }

// remember 记下刚设定的音量。
func (t *VolumeTracker) remember(v int) {
	t.mu.Lock()
	t.value = v
	t.at = time.Now()
	t.mu.Unlock()
}

// recent 返回仍在有效期内的记忆值；没有或已过期时第二个返回值为 false。
func (t *VolumeTracker) recent() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.at.IsZero() || time.Since(t.at) > volumeTrackerTTL {
		return 0, false
	}
	return t.value, true
}

// parseVolumeStep 解析 volume_up / volume_down 的步长。不传（0）时用默认值。
func parseVolumeStep(in controlInput) (int, error) {
	if in.Value == 0 {
		return DefaultVolumeStep, nil
	}
	step := int(in.Value)
	if step < 1 || step > maxVolume {
		return 0, fmt.Errorf("volume_up / volume_down 的 value 是步长，取值 1 到 %d（不传则默认 %d）", maxVolume, DefaultVolumeStep)
	}
	return step, nil
}

// currentVolume 取相对增减的基准音量：优先本地记忆，否则读 LX。
func currentVolume(ctx context.Context, d *Deps) (int, error) {
	if v, ok := d.Volume.recent(); ok {
		return v, nil
	}
	return d.API.CurrentVolume(ctx)
}

// adjustVolume 把当前音量按 step 加减、夹到合法范围后写回，返回实际生效的新音量。
//
// 到边界时是“夹住”而不是报错：让“调大三次”这种连续调用自然饱和，
// 而不是中途抛错打断模型。
func adjustVolume(ctx context.Context, d *Deps, up bool, step int) (int, error) {
	cur, err := currentVolume(ctx, d)
	if err != nil {
		return 0, err
	}

	next := cur - step
	if up {
		next = cur + step
	}
	if next < minVolume {
		next = minVolume
	}
	if next > maxVolume {
		next = maxVolume
	}

	if _, err := d.API.Control(ctx, lxapi.ActionVolume, float64(next), false); err != nil {
		return 0, err
	}
	d.Volume.remember(next)
	return next, nil
}
