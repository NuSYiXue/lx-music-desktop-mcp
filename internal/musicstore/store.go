// Package musicstore 在进程内缓存完整的 MusicInfo。
//
// 为什么需要它：
//
// 搜索结果是完整 MusicInfo 才能可靠播放，但把整个对象回传给模型很浪费上下文
// （一首歌约 0.5KB，50 首就 25KB）。而且模型一旦手动裁剪或重构对象，
// 缺了 meta.qualitys / meta._qualitys 就会导致"歌单里有歌但永远播不出来"。
//
// 所以对模型只暴露精简列表 + 一个稳定 ref（就是 MusicInfo 的 id），
// 完整对象留在本进程，播放或入库时按 ref 取回**原样字节**。
package musicstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// DefaultLimit 是缓存条目上限。
const DefaultLimit = 5000

// Item 是一首歌的缓存条目。
type Item struct {
	// Raw 是原始 MusicInfo JSON，原样透传，不做任何重组。
	Raw json.RawMessage
	// ID 同时充当对模型暴露的 ref。
	ID       string
	Name     string
	Singer   string
	Source   string
	Interval string
	// Quality 是可用音质等级，已按从高到低排序。
	Quality []string
}

// Store 是有上限的进程内缓存，并发安全。
type Store struct {
	mu    sync.RWMutex
	items map[string]*Item
	order []string
	limit int
}

// New 创建缓存。limit <= 0 时使用 DefaultLimit。
func New(limit int) *Store {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Store{
		items: make(map[string]*Item),
		limit: limit,
	}
}

// wireMusicInfo 只用于读出展示字段，绝不用于回写。
type wireMusicInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Singer   string `json:"singer"`
	Source   string `json:"source"`
	Interval string `json:"interval"`
	Meta     struct {
		Qualitys  []struct {
			Type string `json:"type"`
		} `json:"qualitys"`
		Qualitys2 map[string]json.RawMessage `json:"_qualitys"`
	} `json:"meta"`
}

// PutRaw 存入一条 MusicInfo 原始 JSON，并解析出展示字段。
func (s *Store) PutRaw(raw json.RawMessage) (*Item, error) {
	if len(raw) == 0 {
		return nil, errors.New("空的 MusicInfo")
	}
	var w wireMusicInfo
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("解析 MusicInfo 失败：%w", err)
	}
	if w.ID == "" {
		return nil, errors.New("MusicInfo 缺少 id 字段")
	}

	quality := make([]string, 0, 4)
	if len(w.Meta.Qualitys2) > 0 {
		for k := range w.Meta.Qualitys2 {
			quality = append(quality, k)
		}
	} else {
		for _, q := range w.Meta.Qualitys {
			if q.Type != "" {
				quality = append(quality, q.Type)
			}
		}
	}
	SortQuality(quality)

	item := &Item{
		Raw:      append(json.RawMessage(nil), raw...), // 拷贝，避免调用方复用底层数组
		ID:       w.ID,
		Name:     w.Name,
		Singer:   w.Singer,
		Source:   w.Source,
		Interval: w.Interval,
		Quality:  quality,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[item.ID]; !exists {
		s.order = append(s.order, item.ID)
	}
	s.items[item.ID] = item
	s.evictLocked()
	return item, nil
}

// PutAll 批量存入，返回成功加入的条目（顺序与输入一致，失败的跳过）。
func (s *Store) PutAll(raws []json.RawMessage) []*Item {
	items := make([]*Item, 0, len(raws))
	for _, raw := range raws {
		item, err := s.PutRaw(raw)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items
}

// Get 按 ref 取回条目。
func (s *Store) Get(ref string) (*Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[ref]
	return item, ok
}

// GetRaw 按 ref 取回原始 JSON。
func (s *Store) GetRaw(ref string) (json.RawMessage, bool) {
	item, ok := s.Get(ref)
	if !ok {
		return nil, false
	}
	return item.Raw, true
}

// GetMany 按 refs 批量取回。
//
// 返回命中的条目和未命中的 ref 列表；调用方应把 missing 明确报给模型，
// 而不是静默丢弃——静默丢弃会让"添加了 50 首但只进了 48 首"无从察觉。
func (s *Store) GetMany(refs []string) (items []*Item, missing []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items = make([]*Item, 0, len(refs))
	for _, ref := range refs {
		if item, ok := s.items[ref]; ok {
			items = append(items, item)
			continue
		}
		missing = append(missing, ref)
	}
	return items, missing
}

// Len 返回当前缓存条数。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// evictLocked 按插入顺序淘汰最老的条目。调用方需持有写锁。
func (s *Store) evictLocked() {
	for len(s.order) > s.limit {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
}
