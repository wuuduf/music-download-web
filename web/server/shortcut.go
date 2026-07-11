package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/musicservice"
)

const maxShortcutInputBytes = 16 << 10

type shortcutResolveRequest struct {
	Input           string `json:"input"`
	URL             string `json:"url,omitempty"`
	Action          string `json:"action,omitempty"`
	Quality         string `json:"quality,omitempty"`
	PrepareDownload bool   `json:"prepare_download,omitempty"`
	WaitSeconds     *int   `json:"wait_seconds,omitempty"`
}

type shortcutLinks struct {
	Website           string `json:"website"`
	Lyrics            string `json:"lyrics"`
	DownloadCreateAPI string `json:"download_create_api"`
	Status            string `json:"status,omitempty"`
	Events            string `json:"events,omitempty"`
	File              string `json:"file,omitempty"`
}

type shortcutResolveResponse struct {
	OK       bool                       `json:"ok"`
	Version  string                     `json:"version"`
	Track    *musicservice.SearchResult `json:"track"`
	Download *musicservice.DownloadJob  `json:"download,omitempty"`
	Links    shortcutLinks              `json:"links"`
}

// handleShortcutResolve is the stable transport endpoint for iOS Shortcuts.
// It accepts copied/shared text (not only a bare URL), resolves one supported
// song, and can optionally enqueue a download in the same request.
func (s *Server) handleShortcutResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireShortcutAPIKey(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	}
	req, err := decodeShortcutResolveRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input := strings.TrimSpace(req.Input)
	if input == "" {
		input = strings.TrimSpace(req.URL)
	}
	if input == "" {
		writeError(w, http.StatusBadRequest, "input 或 url 必填")
		return
	}
	track, err := s.music.ParseLink(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	base := s.shortcutPublicBaseURL(r)
	trackPath := url.PathEscape(track.Platform) + "/" + url.PathEscape(track.TrackID)
	response := shortcutResolveResponse{
		OK:      true,
		Version: "v1",
		Track:   track,
		Links: shortcutLinks{
			Website:           base + "/",
			Lyrics:            base + "/api/v1/lyrics/" + trackPath + "?format=ttml&translation=1&roma=1",
			DownloadCreateAPI: base + "/api/v1/downloads",
		},
	}

	wantsDownload := r.Method == http.MethodPost && (req.PrepareDownload || strings.EqualFold(strings.TrimSpace(req.Action), "download"))
	if wantsDownload {
		quality := strings.TrimSpace(req.Quality)
		if quality == "" {
			quality = "high"
		}
		job, createErr := s.music.CreateDownload(r.Context(), musicservice.DownloadRequest{
			Platform: track.Platform,
			TrackID:  track.TrackID,
			Quality:  quality,
		})
		if createErr != nil {
			writeError(w, http.StatusBadRequest, createErr.Error())
			return
		}
		waitSeconds := 15
		if req.WaitSeconds != nil {
			waitSeconds = *req.WaitSeconds
		}
		if waitSeconds < 0 {
			waitSeconds = 0
		}
		if waitSeconds > 25 {
			waitSeconds = 25
		}
		job = s.waitShortcutDownload(r, job, time.Duration(waitSeconds)*time.Second)
		response.Download = job
		jobPath := "/api/v1/downloads/" + url.PathEscape(job.JobID)
		response.Links.Status = base + jobPath
		response.Links.Events = base + jobPath + "/events"
		response.Links.File = base + jobPath + "/file"
	}

	status := http.StatusOK
	if response.Download != nil && !isFinalDownloadStatus(response.Download.Status) {
		status = http.StatusAccepted
	}
	writeJSON(w, status, response)
}

func decodeShortcutResolveRequest(r *http.Request) (shortcutResolveRequest, error) {
	var req shortcutResolveRequest
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		req.Input = firstNonEmpty(q.Get("input"), q.Get("text"), q.Get("url"))
		return req, nil
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	switch {
	case strings.Contains(contentType, "application/json") || contentType == "":
		decoder := json.NewDecoder(io.LimitReader(r.Body, maxShortcutInputBytes))
		if err := decoder.Decode(&req); err != nil {
			return req, fmt.Errorf("JSON 格式错误: %w", err)
		}
	case strings.Contains(contentType, "text/plain"):
		body, err := io.ReadAll(io.LimitReader(r.Body, maxShortcutInputBytes))
		if err != nil {
			return req, fmt.Errorf("读取输入失败: %w", err)
		}
		req.Input = string(body)
	case strings.Contains(contentType, "application/x-www-form-urlencoded") || strings.Contains(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(maxShortcutInputBytes); err != nil && !strings.Contains(contentType, "application/x-www-form-urlencoded") {
			return req, fmt.Errorf("表单格式错误: %w", err)
		}
		if strings.Contains(contentType, "application/x-www-form-urlencoded") {
			if err := r.ParseForm(); err != nil {
				return req, fmt.Errorf("表单格式错误: %w", err)
			}
		}
		req.Input = firstNonEmpty(r.Form.Get("input"), r.Form.Get("text"), r.Form.Get("url"))
		req.Action = r.Form.Get("action")
		req.Quality = r.Form.Get("quality")
		req.PrepareDownload, _ = strconv.ParseBool(r.Form.Get("prepare_download"))
		if raw := strings.TrimSpace(r.Form.Get("wait_seconds")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return req, fmt.Errorf("wait_seconds 必须是整数")
			}
			req.WaitSeconds = &value
		}
	default:
		return req, fmt.Errorf("不支持的 Content-Type")
	}
	return req, nil
}

func (s *Server) waitShortcutDownload(r *http.Request, initial *musicservice.DownloadJob, timeout time.Duration) *musicservice.DownloadJob {
	if initial == nil || timeout <= 0 || isFinalDownloadStatus(initial.Status) {
		return initial
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	latest := initial
	for {
		select {
		case <-r.Context().Done():
			return latest
		case <-deadline.C:
			return latest
		case <-ticker.C:
			if job, ok := s.music.GetJob(initial.JobID); ok {
				latest = job
				if isFinalDownloadStatus(job.Status) {
					return job
				}
			}
		}
	}
}

func (s *Server) requireShortcutAPIKey(w http.ResponseWriter, r *http.Request) bool {
	want := ""
	if s != nil && s.core != nil && s.core.Config != nil {
		want = strings.TrimSpace(s.core.Config.GetString("WebShortcutAPIKey"))
	}
	if want == "" {
		writeError(w, http.StatusServiceUnavailable, "快捷指令 API 未启用")
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		got = strings.TrimSpace(auth[len("bearer "):])
	}
	if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="musicweb-shortcut"`)
		writeError(w, http.StatusUnauthorized, "API Key 无效")
		return false
	}
	return true
}

func (s *Server) shortcutPublicBaseURL(r *http.Request) string {
	if s != nil && s.core != nil && s.core.Config != nil {
		if configured := strings.TrimRight(strings.TrimSpace(s.core.Config.GetString("WebPublicBaseURL")), "/"); strings.HasPrefix(configured, "https://") || strings.HasPrefix(configured, "http://") {
			return configured
		}
	}
	scheme := firstNonEmpty(r.Header.Get("X-Forwarded-Proto"), "http")
	host := firstNonEmpty(r.Header.Get("X-Forwarded-Host"), r.Host)
	return strings.TrimRight(scheme+"://"+host, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
