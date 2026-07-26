// Package charts powers the public landing page: a curated set of per-platform
// "hot" playlists rendered as cover + title links.
//
// Every platform plugin already implements platform.GetPlaylist, and each
// platform's hot chart is just a playlist with a well-known ID, so this package
// needs no per-platform scraping — it only maps platform -> playlist ID and
// fans out.
//
// Reality check that shaped the design: not every platform can serve a chart.
// Probing the live plugins showed netease/applemusic/qqmusic working, spotify
// needing credentials, and bilibili/youtubemusic not supporting playlists at
// all. So a platform failing to load is a normal, expected state — it is
// dropped from the response instead of breaking the page.
package charts

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// Config is the subset of the app config this package reads.
type Config interface {
	GetString(string) string
	GetBool(string) bool
	GetInt(string) int
}

// defaultCharts are playlist IDs verified against the live plugins. Platforms
// whose plugin cannot serve playlists (bilibili, youtubemusic) are absent by
// design; operators can still point any platform at a playlist ID from /admin.
var defaultCharts = []Entry{
	{Platform: "netease", PlaylistID: "19723756", Title: "飙升榜", Enabled: true},
	{Platform: "netease", PlaylistID: "3778678", Title: "热歌榜", Enabled: true},
	{Platform: "applemusic", PlaylistID: "pl.f4d106fed2bd41149aaacabb233eb5eb", Title: "Today's Hits", Enabled: true},
	{Platform: "qqmusic", PlaylistID: "", Title: "QQ音乐歌单", Enabled: false},
	{Platform: "spotify", PlaylistID: "", Title: "Spotify 榜单", Enabled: false},
	{Platform: "kugou", PlaylistID: "", Title: "酷狗榜单", Enabled: false},
	{Platform: "soda", PlaylistID: "", Title: "汽水榜单", Enabled: false},
}

// Entry is one configured chart.
type Entry struct {
	Platform   string `json:"platform"`
	PlaylistID string `json:"playlist_id"`
	Title      string `json:"title"`
	Enabled    bool   `json:"enabled"`
}

// Track is a single rendered chart item: cover + title, linking somewhere.
type Track struct {
	TrackID  string   `json:"track_id"`
	Platform string   `json:"platform"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists,omitempty"`
	Album    string   `json:"album,omitempty"`
	CoverURL string   `json:"cover_url,omitempty"`
	URL      string   `json:"url,omitempty"`
}

// Chart is one platform's chart with its loaded tracks.
type Chart struct {
	Platform    string  `json:"platform"`
	DisplayName string  `json:"display_name"`
	PlaylistID  string  `json:"playlist_id"`
	Title       string  `json:"title"`
	CoverURL    string  `json:"cover_url,omitempty"`
	Tracks      []Track `json:"tracks"`
}

// Result is the payload the landing page renders.
type Result struct {
	Charts    []Chart   `json:"charts"`
	LinkMode  string    `json:"link_mode"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Link modes decide where a chart item navigates to.
const (
	LinkModePlayer   = "player"   // in-site AMLL player
	LinkModeSearch   = "search"   // in-site search page, prefilled
	LinkModePlatform = "platform" // original platform page
)

const (
	defaultTTL       = 30 * time.Minute
	defaultPerChart  = 12
	maxPerChart      = 50
	perPlatformLimit = 25 * time.Second
)

// Service loads and caches chart data.
type Service struct {
	platforms platform.Manager
	config    Config

	mu       sync.RWMutex
	cached   *Result
	cachedAt time.Time
	inflight sync.Mutex // collapses concurrent refreshes into one
}

func New(platforms platform.Manager, config Config) *Service {
	return &Service{platforms: platforms, config: config}
}

func (s *Service) ttl() time.Duration {
	if s.config != nil {
		if m := s.config.GetInt("WebChartsCacheMinutes"); m > 0 {
			return time.Duration(m) * time.Minute
		}
	}
	return defaultTTL
}

func (s *Service) perChart() int {
	if s.config != nil {
		if n := s.config.GetInt("WebChartsTracksPerChart"); n > 0 {
			if n > maxPerChart {
				return maxPerChart
			}
			return n
		}
	}
	return defaultPerChart
}

// LinkMode returns the configured navigation target for chart items.
func (s *Service) LinkMode() string {
	if s.config != nil {
		switch strings.TrimSpace(s.config.GetString("WebChartsLinkMode")) {
		case LinkModeSearch:
			return LinkModeSearch
		case LinkModePlatform:
			return LinkModePlatform
		case LinkModePlayer:
			return LinkModePlayer
		}
	}
	return LinkModePlayer
}

// Entries returns the configured charts, merging operator overrides from
// config over the built-in defaults.
func (s *Service) Entries() []Entry {
	out := make([]Entry, 0, len(defaultCharts))
	for _, def := range defaultCharts {
		entry := def
		if s.config != nil {
			key := "WebChart_" + entry.Platform + "_" + sanitizeKey(entry.Title)
			if raw := strings.TrimSpace(s.config.GetString(key)); raw != "" {
				// Stored as "enabled|playlistID|title"
				parts := strings.SplitN(raw, "|", 3)
				if len(parts) >= 1 {
					entry.Enabled = parts[0] == "1"
				}
				if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
					entry.PlaylistID = strings.TrimSpace(parts[1])
				}
				if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
					entry.Title = strings.TrimSpace(parts[2])
				}
			}
		}
		out = append(out, entry)
	}
	return out
}

// ConfigKey is the config.ini key used to persist one entry's overrides.
func ConfigKey(platformName, title string) string {
	return "WebChart_" + platformName + "_" + sanitizeKey(title)
}

// EncodeEntry serialises an entry into its config value form.
func EncodeEntry(enabled bool, playlistID, title string) string {
	flag := "0"
	if enabled {
		flag = "1"
	}
	return flag + "|" + strings.TrimSpace(playlistID) + "|" + strings.TrimSpace(title)
}

// sanitizeKey keeps config keys INI-safe (letters/digits/underscore only).
//
// Titles are often pure CJK ("飙升榜", "热歌榜"), which a naive
// replace-with-underscore would collapse into the *same* key, so two charts on
// one platform would overwrite each other. A short hash of the original title
// is appended to keep distinct titles distinct.
func sanitizeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	sum := sha1.Sum([]byte(s))
	return b.String() + hex.EncodeToString(sum[:3])
}

// Get returns cached charts, refreshing when stale.
func (s *Service) Get(ctx context.Context) *Result {
	s.mu.RLock()
	cached, at := s.cached, s.cachedAt
	s.mu.RUnlock()
	if cached != nil && time.Since(at) < s.ttl() {
		return cached
	}
	return s.Refresh(ctx)
}

// Refresh reloads every enabled chart concurrently. A platform that errors or
// returns nothing is omitted rather than failing the whole page.
func (s *Service) Refresh(ctx context.Context) *Result {
	// Collapse concurrent refreshes; the loser reuses the winner's result.
	s.inflight.Lock()
	defer s.inflight.Unlock()
	s.mu.RLock()
	cached, at := s.cached, s.cachedAt
	s.mu.RUnlock()
	if cached != nil && time.Since(at) < s.ttl() {
		return cached
	}

	entries := s.Entries()
	type slot struct {
		index int
		chart *Chart
	}
	results := make(chan slot, len(entries))
	var wg sync.WaitGroup
	limit := s.perChart()

	for i, entry := range entries {
		if !entry.Enabled || strings.TrimSpace(entry.PlaylistID) == "" {
			continue
		}
		plat := s.platforms.Get(entry.Platform)
		if plat == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, e Entry, p platform.Platform) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, perPlatformLimit)
			defer cancel()
			pl, err := p.GetPlaylist(c, e.PlaylistID)
			if err != nil || pl == nil || len(pl.Tracks) == 0 {
				return
			}
			chart := &Chart{
				Platform:    e.Platform,
				DisplayName: s.displayName(e.Platform),
				PlaylistID:  e.PlaylistID,
				Title:       firstNonEmpty(e.Title, pl.Title),
				CoverURL:    pl.CoverURL,
			}
			for _, t := range pl.Tracks {
				if len(chart.Tracks) >= limit {
					break
				}
				artists := make([]string, 0, len(t.Artists))
				for _, a := range t.Artists {
					artists = append(artists, a.Name)
				}
				album := ""
				if t.Album != nil {
					album = t.Album.Title
				}
				chart.Tracks = append(chart.Tracks, Track{
					TrackID:  t.ID,
					Platform: e.Platform,
					Title:    t.Title,
					Artists:  artists,
					Album:    album,
					CoverURL: t.CoverURL,
					URL:      t.URL,
				})
			}
			if len(chart.Tracks) > 0 {
				results <- slot{index: idx, chart: chart}
			}
		}(i, entry, plat)
	}
	go func() { wg.Wait(); close(results) }()

	collected := make([]slot, 0, len(entries))
	for r := range results {
		collected = append(collected, r)
	}
	// Preserve the configured order regardless of completion order.
	sort.Slice(collected, func(i, j int) bool { return collected[i].index < collected[j].index })
	out := &Result{Charts: make([]Chart, 0, len(collected)), LinkMode: s.LinkMode(), UpdatedAt: time.Now()}
	for _, c := range collected {
		out.Charts = append(out.Charts, *c.chart)
	}

	s.mu.Lock()
	s.cached, s.cachedAt = out, time.Now()
	s.mu.Unlock()
	return out
}

// Invalidate drops the cache so the next Get refetches.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.cached, s.cachedAt = nil, time.Time{}
	s.mu.Unlock()
}

func (s *Service) displayName(name string) string {
	if s.platforms != nil {
		if meta, ok := s.platforms.Meta(name); ok && strings.TrimSpace(meta.DisplayName) != "" {
			return meta.DisplayName
		}
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
