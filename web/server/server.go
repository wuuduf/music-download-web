package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/app"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/musicservice"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	lyricservice "github.com/liuran001/MusicBot-Go/webapp/lyrics"
	"github.com/liuran001/MusicBot-Go/webapp/playback"
	"github.com/liuran001/MusicBot-Go/webapp/studio"
)

type Server struct {
	core     *app.Core
	music    *musicservice.Service
	lyrics   *lyricservice.Service
	playback *playback.Service
	studio   *studio.Service
	mux      *http.ServeMux
	qrMu     sync.RWMutex
	qr       map[string]*qrSessionState
	rateMu   sync.Mutex
	rates    map[string]*rateWindow
}

func New(core *app.Core, music *musicservice.Service) *Server {
	var cfg lyricservice.Config
	var platforms platform.Manager
	var repo *db.Repository
	ttl := 24 * time.Hour
	if core != nil {
		cfg, platforms, repo = core.Config, core.PlatformManager, core.DB
		if core.Config != nil && core.Config.GetInt("WebPlaybackTTLHours") > 0 {
			ttl = time.Duration(core.Config.GetInt("WebPlaybackTTLHours")) * time.Hour
		}
	}
	lyricResolver := lyricservice.New(cfg, platforms)
	lyricResolver.Start(context.Background())
	music.SetLyricsResolver(lyricResolver)
	playbackService := playback.New(music, ttl)
	s := &Server{core: core, music: music, lyrics: lyricResolver, playback: playbackService, studio: studio.New(repo, platforms, playbackService, lyricResolver), mux: http.NewServeMux(), qr: make(map[string]*qrSessionState), rates: make(map[string]*rateWindow)}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withRateLimit(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/admin", s.handleAdmin)
	s.mux.HandleFunc("/admin/", s.handleAdmin)
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/platforms", s.handlePlatforms)
	s.mux.HandleFunc("/api/search", s.handleSearch)
	s.mux.HandleFunc("/api/parse", s.handleParseLink)
	s.mux.HandleFunc("/api/lyrics/file", s.handleLyricsFile)
	s.mux.HandleFunc("/api/downloads", s.handleDownloads)
	s.mux.HandleFunc("/api/downloads/", s.handleDownloadByID)
	s.mux.HandleFunc("/api/v1/platforms", s.handlePlatforms)
	s.mux.HandleFunc("/api/v1/search", s.handleSearch)
	s.mux.HandleFunc("/api/v1/parse", s.handleParseLink)
	s.mux.HandleFunc("/api/v1/shortcut/resolve", s.handleShortcutResolve)
	s.mux.HandleFunc("/api/v1/shortcut/bundle", s.handleShortcutBundle)
	s.mux.HandleFunc("/api/v1/shortcut/assets/", s.handleShortcutAsset)
	s.mux.HandleFunc("/api/v1/lyrics/file", s.handleLyricsFile)
	s.mux.HandleFunc("/api/v1/downloads", s.handleDownloads)
	s.mux.HandleFunc("/api/v1/downloads/", s.handleDownloadByID)
	s.mux.HandleFunc("/api/v1/playback/sessions", s.handlePlaybackSessions)
	s.mux.HandleFunc("/api/v1/playback/sessions/", s.handlePlaybackSession)
	s.mux.HandleFunc("/api/v1/lyrics/", s.handleResolvedLyrics)
	s.mux.HandleFunc("/api/v1/media/image", s.handleImageProxy)
	s.mux.HandleFunc("/api/v1/studio/projects", s.handleStudioProjects)
	s.mux.HandleFunc("/api/v1/studio/projects/", s.handleStudioProject)
	s.mux.HandleFunc("/api/v1/studio/metadata/search", s.handleStudioMetadataSearch)
	s.mux.HandleFunc("/api/v1/admin/amlldb/status", s.handleAMLLDBStatus)
	s.mux.HandleFunc("/api/v1/admin/amlldb/sync", s.handleAMLLDBSync)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/studio/") && !s.requireAdmin(w, r) {
		return
	}
	if s.serveWebAsset(w, r) {
		return
	}
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/player/") && !strings.HasPrefix(r.URL.Path, "/studio/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handlePlatforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": s.music.Platforms()})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	platformName := strings.TrimSpace(r.URL.Query().Get("platform"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if q == "" || platformName == "" {
		writeError(w, http.StatusBadRequest, "platform 和 q 必填")
		return
	}
	results, err := s.music.Search(r.Context(), platformName, q, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"platform": platformName,
		"query":    q,
		"results":  results,
	})
}

func (s *Server) handleParseLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	link := strings.TrimSpace(r.URL.Query().Get("url"))
	if link == "" {
		writeError(w, http.StatusBadRequest, "url 必填")
		return
	}
	result, err := s.music.ParseLink(r.Context(), link)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) handleLyricsFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query := r.URL.Query()
	doc, err := s.music.CreateLyrics(r.Context(), musicservice.LyricsRequest{
		Platform:           strings.TrimSpace(query.Get("platform")),
		TrackID:            strings.TrimSpace(query.Get("track_id")),
		Format:             strings.TrimSpace(query.Get("format")),
		IncludeTranslation: query.Get("translation") == "1",
		IncludeRoma:        query.Get("roma") == "1",
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", musicservice.LyricsContentDisposition(doc.FileName))
	w.Header().Set("Content-Length", strconv.Itoa(len(doc.Content)))
	_, _ = w.Write(doc.Content)
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/downloads" && r.URL.Path != "/api/v1/downloads" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req musicservice.DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	job, err := s.music.CreateDownload(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleDownloadByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/downloads/")
	if rest == r.URL.Path {
		rest = strings.TrimPrefix(r.URL.Path, "/api/v1/downloads/")
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	jobID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		job, ok := s.music.GetJob(jobID)
		if !ok {
			writeError(w, http.StatusNotFound, "下载任务不存在")
			return
		}
		writeJSON(w, http.StatusOK, job)
		return
	}
	if len(parts) == 2 && parts[1] == "file" {
		s.handleDownloadFile(w, r, jobID)
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		s.handleDownloadEvents(w, r, jobID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleDownloadEvents(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server-sent events unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() bool {
		job, ok := s.music.GetJob(jobID)
		if !ok {
			payload, _ := json.Marshal(map[string]any{"error": "下载任务不存在"})
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
			flusher.Flush()
			return true
		}
		payload, _ := json.Marshal(job)
		_, _ = fmt.Fprintf(w, "event: job\ndata: %s\n\n", payload)
		flusher.Flush()
		return isFinalDownloadStatus(job.Status)
	}
	if send() {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if send() {
				return
			}
		}
	}
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	job, ok := s.music.GetJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "下载任务不存在")
		return
	}
	if job.Status != "ready" {
		writeError(w, http.StatusConflict, "文件尚未准备好")
		return
	}
	if strings.TrimSpace(job.FilePath) == "" {
		writeError(w, http.StatusNotFound, "文件路径不存在")
		return
	}
	f, err := os.Open(job.FilePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在或已过期")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, "文件不可读")
		return
	}
	name := job.FileName
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(job.FilePath)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, `_`)+`"`)
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func isFinalDownloadStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "ready", "failed", "expired":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}
	writeJSON(w, status, map[string]any{"error": message})
}

func listenAddr(core *app.Core) string {
	if core != nil && core.Config != nil {
		if addr := strings.TrimSpace(core.Config.GetString("WebListenAddr")); addr != "" {
			return addr
		}
	}
	return "127.0.0.1:8080"
}

func ListenAndServe(core *app.Core, music *musicservice.Service) error {
	s := New(core, music)
	srv := &http.Server{
		Addr:              listenAddr(core),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if core != nil && core.Logger != nil {
		core.Logger.Info("music web server listening", "addr", srv.Addr)
	}
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
