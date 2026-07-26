package server

import (
	"net/http"
	"strings"

	"github.com/liuran001/MusicBot-Go/webapp/charts"
)

// handleCharts serves the public landing page data. Cached in the service, so
// this stays cheap even as the site's default page.
func (s *Server) handleCharts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.charts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"charts": []any{}, "link_mode": charts.LinkModePlayer})
		return
	}
	writeJSON(w, http.StatusOK, s.charts.Get(r.Context()))
}

// handleAdminCharts lists every configured chart (including disabled ones) so
// the admin panel can render the full set.
func (s *Server) handleAdminCharts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.charts == nil {
		writeError(w, http.StatusServiceUnavailable, "榜单服务未初始化")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":   s.charts.Entries(),
		"link_mode": s.charts.LinkMode(),
	})
}

// handleAdminChartsSave persists chart overrides (enable/ID/title) and the
// link mode, then drops the cache so the change shows up immediately.
func (s *Server) handleAdminChartsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.charts == nil || s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "榜单服务未初始化")
		return
	}
	var body struct {
		Entries []struct {
			Platform   string `json:"platform"`
			Title      string `json:"title"`
			PlaylistID string `json:"playlist_id"`
			Enabled    bool   `json:"enabled"`
		} `json:"entries"`
		LinkMode string `json:"link_mode"`
	}
	if !decodeJSON(w, r, 64<<10, &body) {
		return
	}

	pairs := make(map[string]string)
	for _, e := range body.Entries {
		platformName := strings.TrimSpace(e.Platform)
		title := strings.TrimSpace(e.Title)
		if platformName == "" || title == "" {
			continue
		}
		// Enabling without an ID would just fail silently at load time.
		if e.Enabled && strings.TrimSpace(e.PlaylistID) == "" {
			writeError(w, http.StatusBadRequest, "启用「"+title+"」前请先填写歌单 ID")
			return
		}
		pairs[charts.ConfigKey(platformName, title)] = charts.EncodeEntry(e.Enabled, e.PlaylistID, title)
	}
	switch strings.TrimSpace(body.LinkMode) {
	case charts.LinkModePlayer, charts.LinkModeSearch, charts.LinkModePlatform:
		pairs["WebChartsLinkMode"] = strings.TrimSpace(body.LinkMode)
	case "":
		// leave unchanged
	default:
		writeError(w, http.StatusBadRequest, "跳转方式无效")
		return
	}
	if len(pairs) == 0 {
		writeError(w, http.StatusBadRequest, "没有可保存的内容")
		return
	}
	if err := s.core.Config.PersistAdminConfig(pairs); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.charts.Invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "已保存，正在重新拉取榜单"})
}

// handleAdminChartsRefresh forces a reload, bypassing the cache TTL.
func (s *Server) handleAdminChartsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.charts == nil {
		writeError(w, http.StatusServiceUnavailable, "榜单服务未初始化")
		return
	}
	s.charts.Invalidate()
	result := s.charts.Refresh(r.Context())
	loaded := 0
	for _, c := range result.Charts {
		loaded += len(c.Tracks)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "charts": len(result.Charts), "tracks": loaded,
	})
}
