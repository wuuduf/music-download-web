package charts

import "testing"

// fakeConfig lets the tests drive Entries()/LinkMode() without a real app config.
type fakeConfig map[string]string

func (f fakeConfig) GetString(k string) string { return f[k] }
func (f fakeConfig) GetBool(k string) bool     { return f[k] == "true" }
func (f fakeConfig) GetInt(k string) int {
	switch f[k] {
	case "5":
		return 5
	case "999":
		return 999
	}
	return 0
}

func TestEntriesUsesDefaultsWithoutConfig(t *testing.T) {
	s := New(nil, nil)
	entries := s.Entries()
	if len(entries) != len(defaultCharts) {
		t.Fatalf("got %d entries, want %d", len(entries), len(defaultCharts))
	}
	// The verified netease chart should be enabled out of the box.
	var found bool
	for _, e := range entries {
		if e.Platform == "netease" && e.PlaylistID == "19723756" {
			found = true
			if !e.Enabled {
				t.Error("netease 飙升榜 should be enabled by default")
			}
		}
	}
	if !found {
		t.Error("built-in netease chart missing")
	}
}

func TestEntriesConfigOverride(t *testing.T) {
	key := ConfigKey("netease", "飙升榜")
	cfg := fakeConfig{key: EncodeEntry(false, "12345", "我的榜单")}
	s := New(nil, cfg)
	for _, e := range s.Entries() {
		if e.Platform != "netease" || e.Title != "我的榜单" {
			continue
		}
		if e.Enabled {
			t.Error("override should have disabled the entry")
		}
		if e.PlaylistID != "12345" {
			t.Errorf("playlist ID = %q, want 12345", e.PlaylistID)
		}
		return
	}
	t.Error("overridden entry not found")
}

func TestEntriesOverrideEnableOnly(t *testing.T) {
	// Enabling a shipped-but-empty platform requires supplying an ID too;
	// the encoded form must round-trip both.
	key := ConfigKey("qqmusic", "QQ音乐歌单")
	cfg := fakeConfig{key: EncodeEntry(true, "7364258222", "QQ音乐歌单")}
	s := New(nil, cfg)
	for _, e := range s.Entries() {
		if e.Platform == "qqmusic" {
			if !e.Enabled || e.PlaylistID != "7364258222" {
				t.Errorf("got enabled=%v id=%q", e.Enabled, e.PlaylistID)
			}
			return
		}
	}
	t.Error("qqmusic entry missing")
}

func TestConfigKeyIsINISafe(t *testing.T) {
	// Titles carry CJK, spaces and punctuation; keys must stay [A-Za-z0-9_].
	got := ConfigKey("netease", "飙升榜 Top/100")
	for _, r := range got {
		ok := r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			t.Fatalf("key %q contains unsafe rune %q", got, r)
		}
	}
}

func TestLinkModeDefaultsAndValidates(t *testing.T) {
	if got := New(nil, nil).LinkMode(); got != LinkModePlayer {
		t.Errorf("default link mode = %q, want %q", got, LinkModePlayer)
	}
	if got := New(nil, fakeConfig{"WebChartsLinkMode": "search"}).LinkMode(); got != LinkModeSearch {
		t.Errorf("link mode = %q, want search", got)
	}
	// Unknown values must fall back rather than reach the frontend.
	if got := New(nil, fakeConfig{"WebChartsLinkMode": "bogus"}).LinkMode(); got != LinkModePlayer {
		t.Errorf("invalid link mode = %q, want fallback player", got)
	}
}

func TestPerChartClamped(t *testing.T) {
	if got := New(nil, fakeConfig{"WebChartsTracksPerChart": "5"}).perChart(); got != 5 {
		t.Errorf("perChart = %d, want 5", got)
	}
	if got := New(nil, fakeConfig{"WebChartsTracksPerChart": "999"}).perChart(); got != maxPerChart {
		t.Errorf("perChart = %d, want clamp to %d", got, maxPerChart)
	}
}

func TestConfigKeyDistinguishesCJKTitles(t *testing.T) {
	// Regression: pure-CJK titles used to sanitise to the same key, so two
	// charts on one platform silently overwrote each other's config.
	a := ConfigKey("netease", "飙升榜")
	b := ConfigKey("netease", "热歌榜")
	if a == b {
		t.Fatalf("distinct titles collided on key %q", a)
	}
	if ConfigKey("netease", "飙升榜") != a {
		t.Error("ConfigKey must be stable for the same input")
	}
}
