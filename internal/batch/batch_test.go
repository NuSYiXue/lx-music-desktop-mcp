package batch

import "testing"

func TestParseQuery(t *testing.T) {
	tests := []struct {
		in     string
		song   string
		artist string
		ok     bool
	}{
		{in: "周杰伦 - 夜曲", song: "夜曲", artist: "周杰伦", ok: true},
		{in: "周杰伦 – 夜曲", song: "夜曲", artist: "周杰伦", ok: true},
		{in: "周杰伦 — 夜曲", song: "夜曲", artist: "周杰伦", ok: true},
		{in: "Simple Plan-夜曲", song: "夜曲", artist: "Simple Plan", ok: true},
		{in: "周杰伦 夜曲", song: "夜曲", artist: "周杰伦", ok: true},
		{in: "夜曲", song: "夜曲", artist: "", ok: true},
		{in: "  黄昏  ", song: "黄昏", artist: "", ok: true},
		{in: "   ", ok: false},
		{in: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := ParseQuery(tt.in)
		if ok != tt.ok {
			t.Errorf("ParseQuery(%q) ok = %v, 期望 %v", tt.in, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Song != tt.song || got.Artist != tt.artist {
			t.Errorf("ParseQuery(%q) = {song:%q artist:%q}, 期望 {song:%q artist:%q}",
				tt.in, got.Song, got.Artist, tt.song, tt.artist)
		}
	}
}

func TestParseList(t *testing.T) {
	qs := ParseList("周杰伦 - 夜曲, 光良 童话，黄昏\n简单爱")
	if len(qs) != 4 {
		t.Fatalf("ParseList 得到 %d 条，期望 4 条：%+v", len(qs), qs)
	}

	want := []Query{
		{Raw: "周杰伦 - 夜曲", Song: "夜曲", Artist: "周杰伦"},
		{Raw: " 光良 童话", Song: "童话", Artist: "光良"},
		{Raw: "黄昏", Song: "黄昏"},
		{Raw: "简单爱", Song: "简单爱"},
	}
	for i, w := range want {
		if qs[i].Song != w.Song || qs[i].Artist != w.Artist {
			t.Errorf("第 %d 条 = {song:%q artist:%q}，期望 {song:%q artist:%q}",
				i, qs[i].Song, qs[i].Artist, w.Song, w.Artist)
		}
	}
}

func TestParseListDropsEmpty(t *testing.T) {
	if got := ParseList(" , ,, \n "); len(got) != 0 {
		t.Fatalf("空输入应解析出 0 条，实际 %d 条", len(got))
	}
}

func TestIsSuspect(t *testing.T) {
	cases := []struct {
		name, singer string
		want         bool
	}{
		{"夜曲", "周杰伦", false},
		{"夜曲 (Live)", "周杰伦", true},
		{"童话", "光良", false},
		{"童话", "纯音乐", true},
		{"晴天 (翻唱)", "", true},
		{"", "DJ版", true},
		{"夜曲 Remix", "", true},
		{"告白气球", "周杰伦", false},
	}
	for _, c := range cases {
		if got := IsSuspect(c.name, c.singer); got != c.want {
			t.Errorf("IsSuspect(%q, %q) = %v，期望 %v", c.name, c.singer, got, c.want)
		}
	}
}
