package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/db"
	lyricservice "github.com/liuran001/MusicBot-Go/webapp/lyrics"
	"github.com/liuran001/MusicBot-Go/webapp/playback"
	"github.com/liuran001/MusicBot-Go/webapp/studio"
	"gorm.io/gorm"
)

type rateWindow struct {
	Start time.Time
	Count int
}

func (s *Server) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, bucket := 0, ""
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/shortcut/"):
			limit = 30
			if s.core != nil && s.core.Config != nil && s.core.Config.GetInt("WebShortcutRateLimitPerMinute") > 0 {
				limit = s.core.Config.GetInt("WebShortcutRateLimitPerMinute")
			}
			bucket = "shortcut-api"
		case strings.HasPrefix(r.URL.Path, "/api/v1/studio/"):
			limit, bucket = 120, "studio"
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/playback/"):
			limit, bucket = 20, "playback-create"
		case strings.HasPrefix(r.URL.Path, "/api/v1/playback/"):
			limit, bucket = 300, "playback-read"
		}
		if limit > 0 && !s.allowRequest(clientIP(r)+"|"+bucket, limit) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowRequest(key string, limit int) bool {
	now := time.Now()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	window := s.rates[key]
	if window == nil || now.Sub(window.Start) >= time.Minute {
		window = &rateWindow{Start: now}
		s.rates[key] = window
	}
	window.Count++
	if len(s.rates) > 2048 {
		for item, value := range s.rates {
			if now.Sub(value.Start) > 2*time.Minute {
				delete(s.rates, item)
			}
		}
	}
	return window.Count <= limit
}

func clientIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); value != "" {
		return value
	}
	value := r.RemoteAddr
	if i := strings.LastIndex(value, ":"); i > 0 {
		return strings.Trim(value[:i], "[]")
	}
	return value
}

func (s *Server) handlePlaybackSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req playback.CreateRequest
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	session, err := s.playback.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, session)
}

func (s *Server) handlePlaybackSession(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/playback/sessions/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		session, ok := s.playback.Get(parts[0])
		if !ok {
			writeError(w, http.StatusNotFound, "播放会话不存在或已过期")
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if len(parts) == 2 && parts[1] == "audio" && r.Method == http.MethodGet {
		s.servePlaybackAudio(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "lyrics" && r.Method == http.MethodGet {
		s.servePlaybackLyrics(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) servePlaybackAudio(w http.ResponseWriter, r *http.Request, sessionID string) {
	job, _, ok := s.playback.Job(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "播放会话不存在")
		return
	}
	if job.Status != "ready" {
		writeError(w, http.StatusConflict, "音频尚未准备好")
		return
	}
	file, err := os.Open(job.FilePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "音频缓存不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "音频缓存不可读")
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(job.FileName, `"`, `_`)+`"`)
	if contentType := mime.TypeByExtension(filepath.Ext(job.FilePath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, job.FileName, info.ModTime(), file)
}

func (s *Server) servePlaybackLyrics(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, ok := s.playback.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "播放会话不存在")
		return
	}
	asset, _, err := s.lyrics.Resolve(r.Context(), session.Platform, session.TrackID, lyricservice.ResolveOptions{Format: valueOr(r.URL.Query().Get("format"), "ttml"), IncludeTranslation: r.URL.Query().Get("translation") != "0", IncludeRoma: r.URL.Query().Get("roma") != "0"})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleResolvedLyrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/lyrics/"), "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	asset, identity, err := s.lyrics.Resolve(r.Context(), parts[0], parts[1], lyricservice.ResolveOptions{Format: valueOr(r.URL.Query().Get("format"), "ttml"), IncludeTranslation: r.URL.Query().Get("translation") != "0", IncludeRoma: r.URL.Query().Get("roma") != "0"})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": asset, "track": identity})
}

func (s *Server) handleStudioProjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req studio.CreateRequest
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	project, err := s.studio.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) handleStudioProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/studio/projects/"), "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "bootstrap" && r.Method == http.MethodGet {
		value, err := s.studio.Bootstrap(r.Context(), id)
		writeStudioResult(w, value, err)
		return
	}
	if len(parts) == 3 && parts[1] == "metadata" && parts[2] == "resolve" && r.Method == http.MethodPost {
		value, err := s.studio.RefreshMetadata(r.Context(), id)
		writeStudioResult(w, value, err)
		return
	}
	if len(parts) == 2 && parts[1] == "export" && r.Method == http.MethodGet {
		rev, err := s.studio.Revision(r.Context(), id, 0)
		if err != nil {
			writeStudioError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/ttml+xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.ttml"`)
		_, _ = io.WriteString(w, rev.Content)
		return
	}
	if len(parts) == 2 && parts[1] == "revisions" && r.Method == http.MethodGet {
		value, err := s.studio.Revisions(r.Context(), id)
		writeStudioResult(w, value, err)
		return
	}
	if len(parts) == 2 && parts[1] == "revisions" && r.Method == http.MethodPost {
		var req studio.SaveRequest
		if json.NewDecoder(io.LimitReader(r.Body, 5<<20)).Decode(&req) != nil {
			writeError(w, 400, "JSON 格式错误")
			return
		}
		next, err := s.studio.Save(r.Context(), id, req)
		writeStudioResult(w, map[string]int{"revision": next}, err)
		return
	}
	if len(parts) == 2 && parts[1] == "restore" && r.Method == http.MethodPost {
		var req struct {
			ExpectedRevision int `json:"expected_revision"`
			SourceRevision   int `json:"source_revision"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeError(w, 400, "JSON 格式错误")
			return
		}
		next, err := s.studio.Restore(r.Context(), id, req.ExpectedRevision, req.SourceRevision)
		writeStudioResult(w, map[string]int{"revision": next}, err)
		return
	}
	http.NotFound(w, r)
}

func writeStudioResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeStudioError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func writeStudioError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrStudioRevisionConflict) {
		writeError(w, http.StatusConflict, "修订冲突，请重新加载最新版本")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func (s *Server) handleStudioMetadataSearch(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	platformName, trackID := r.URL.Query().Get("platform"), r.URL.Query().Get("track_id")
	if platformName != "" && trackID != "" {
		results, err := s.studio.MatchMetadata(r.Context(), platformName, trackID, r.URL.Query().Get("q"), limit)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"results": results})
		return
	}
	writeJSON(w, 200, map[string]any{"results": s.studio.SearchMetadata(r.URL.Query().Get("q"), limit)})
}
func (s *Server) handleAMLLDBStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, 200, s.lyrics.Status())
}
func (s *Server) handleAMLLDBSync(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	go func() { _ = s.lyrics.Sync(context.Background()) }()
	writeJSON(w, http.StatusAccepted, map[string]bool{"syncing": true})
}

func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	target, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || target.Scheme != "https" || !allowedImageHost(target.Hostname()) {
		writeError(w, http.StatusBadRequest, "不允许的封面地址")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 MusicWeb/1.0")
	req.Header.Set("Referer", target.Scheme+"://"+target.Hostname()+"/")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(next *http.Request, _ []*http.Request) error {
		if next.URL.Scheme != "https" || !allowedImageHost(next.URL.Hostname()) {
			return errors.New("封面重定向目标不在允许列表")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("封面源 HTTP %d", resp.StatusCode))
		return
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		writeError(w, http.StatusBadGateway, "封面源返回的不是图片")
		return
	} else {
		w.Header().Set("Content-Type", ct)
	}
	if r.URL.Query().Get("download") == "1" {
		name := strings.TrimSpace(r.URL.Query().Get("filename"))
		if name == "" {
			name = "cover.jpg"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 10<<20))
}

func allowedImageHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range []string{"music.126.net", "music.163.com", "y.qq.com", "gtimg.cn", "mzstatic.com", "scdn.co", "spotifycdn.com", "googleusercontent.com", "ytimg.com", "hdslb.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Server) serveWebAsset(w http.ResponseWriter, r *http.Request) bool {
	isStudio := strings.HasPrefix(r.URL.Path, "/studio/")
	dir := "./webui/dist/site"
	if s.core != nil && s.core.Config != nil {
		if v := strings.TrimSpace(s.core.Config.GetString("WebStaticDir")); v != "" {
			dir = v
		}
		if isStudio {
			if v := strings.TrimSpace(s.core.Config.GetString("WebStudioStaticDir")); v != "" {
				dir = v
			}
		}
	}
	if isStudio {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	path := r.URL.Path
	if isStudio {
		path = strings.TrimPrefix(path, "/studio")
	}
	if path == "/" || strings.HasPrefix(path, "/player/") || (isStudio && !strings.Contains(filepath.Base(path), ".")) {
		path = "/index.html"
	}
	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return false
	}
	full := filepath.Join(dir, clean)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		if strings.HasPrefix(r.URL.Path, "/player/") || isStudio {
			full = filepath.Join(dir, "index.html")
			if _, err = os.Stat(full); err != nil {
				return false
			}
		} else {
			return false
		}
	}
	http.ServeFile(w, r, full)
	return true
}
