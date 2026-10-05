package musicstore

import (
	"encoding/json"
	"testing"
)

// kgSample 是酷狗源的 MusicInfo，字段里有 kg 特有的 hash。
const kgSample = `{
  "id": "abc_123",
  "name": "夜曲",
  "singer": "周杰伦",
  "source": "kg",
  "interval": "03:45",
  "meta": {
    "songId": "abc",
    "albumName": "十一月的萧邦",
    "qualitys": [
      {"type": "128k", "size": "3.5M", "hash": "h1"},
      {"type": "flac", "size": "25.0M", "hash": "h2"}
    ],
    "_qualitys": {
      "128k": {"size": "3.5M", "hash": "h1"},
      "flac": {"size": "25.0M", "hash": "h2"}
    },
    "hash": "123"
  }
}`

func TestPutRawParsesDisplayFields(t *testing.T) {
	s := New(10)
	item, err := s.PutRaw(json.RawMessage(kgSample))
	if err != nil {
		t.Fatalf("PutRaw 失败：%v", err)
	}
	if item.ID != "abc_123" || item.Name != "夜曲" || item.Singer != "周杰伦" {
		t.Fatalf("展示字段不对：%+v", item)
	}
	if item.Source != "kg" || item.Interval != "03:45" {
		t.Fatalf("source/interval 不对：%+v", item)
	}
	if len(item.Quality) != 2 || item.Quality[0] != "flac" || item.Quality[1] != "128k" {
		t.Fatalf("音质应按高到低排序，实际 %v", item.Quality)
	}
}

// 这是本设计的核心保证：原始字节必须一字不差地留着，
// 否则 meta 里的源特有字段会在回传时丢失，导致播放静默失败。
func TestRawIsByteIdentical(t *testing.T) {
	s := New(10)
	item, err := s.PutRaw(json.RawMessage(kgSample))
	if err != nil {
		t.Fatalf("PutRaw 失败：%v", err)
	}
	if string(item.Raw) != kgSample {
		t.Fatalf("Raw 被改写了\n期望：%s\n实际：%s", kgSample, item.Raw)
	}

	// 关键字段必须仍然存在
	var back map[string]any
	if err := json.Unmarshal(item.Raw, &back); err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	meta, _ := back["meta"].(map[string]any)
	if meta == nil {
		t.Fatal("meta 丢了")
	}
	if _, ok := meta["_qualitys"]; !ok {
		t.Fatal("meta._qualitys 丢了——这正是会导致播放静默失败的字段")
	}
	if meta["hash"] != "123" {
		t.Fatalf("meta.hash 丢了或被改写：%v", meta["hash"])
	}
}

func TestPutRawRejectsInvalid(t *testing.T) {
	s := New(10)
	if _, err := s.PutRaw(json.RawMessage(`{}`)); err == nil {
		t.Fatal("缺少 id 的 MusicInfo 应该报错")
	}
	if _, err := s.PutRaw(nil); err == nil {
		t.Fatal("空输入应该报错")
	}
}

func TestGetManyReportsMissing(t *testing.T) {
	s := New(10)
	if _, err := s.PutRaw(json.RawMessage(kgSample)); err != nil {
		t.Fatal(err)
	}

	items, missing := s.GetMany([]string{"abc_123", "nope_1", "nope_2"})
	if len(items) != 1 || items[0].ID != "abc_123" {
		t.Fatalf("命中项不对：%+v", items)
	}
	if len(missing) != 2 || missing[0] != "nope_1" || missing[1] != "nope_2" {
		t.Fatalf("缺失项应原样报出，实际 %v", missing)
	}
}

func TestEvictsOldest(t *testing.T) {
	s := New(2)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.PutRaw(json.RawMessage(`{"id":"` + id + `","name":"n"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != 2 {
		t.Fatalf("上限 2，实际 %d", s.Len())
	}
	if _, ok := s.Get("a"); ok {
		t.Fatal("最老的条目应该被淘汰")
	}
	if _, ok := s.Get("c"); !ok {
		t.Fatal("最新条目应该还在")
	}
}

func TestQualityHelpers(t *testing.T) {
	if got := BestQuality([]string{"128k", "flac", "320k"}); got != "flac" {
		t.Fatalf("BestQuality = %q，期望 flac", got)
	}
	if got := BestQuality([]string{"128k"}); got != "128k" {
		t.Fatalf("BestQuality = %q，期望 128k", got)
	}
	if got := BestQuality(nil); got != "" {
		t.Fatalf("BestQuality(nil) = %q，期望空", got)
	}

	if MeetsQuality([]string{"128k"}, "flac") {
		t.Fatal("128k 不该满足 flac 下限")
	}
	if !MeetsQuality([]string{"flac24bit"}, "flac") {
		t.Fatal("flac24bit 应满足 flac 下限")
	}
	if !MeetsQuality([]string{"128k"}, "") {
		t.Fatal("未设下限时应一律通过")
	}

	qs := []string{"320k", "flac24bit", "128k", "flac"}
	SortQuality(qs)
	want := []string{"flac24bit", "flac", "320k", "128k"}
	for i := range want {
		if qs[i] != want[i] {
			t.Fatalf("SortQuality = %v，期望 %v", qs, want)
		}
	}
}
