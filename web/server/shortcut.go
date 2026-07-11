package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/db"
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
	Files           int    `json:"files,omitempty"`
}

type shortcutLinks struct {
	Website           string `json:"website"`
	DownloadCreateAPI string `json:"download_create_api"`
	Status            string `json:"status,omitempty"`
	Events            string `json:"events,omitempty"`
}

type shortcutAsset struct {
	Kind     string `json:"kind"`
	FileName string `json:"file_name"`
	URL      string `json:"url"`
}

type shortcutQuota struct {
	KeyID     string `json:"key_id,omitempty"`
	Used      int64  `json:"used,omitempty"`
	Limit     int64  `json:"limit,omitempty"`
	Remaining int64  `json:"remaining,omitempty"`
	Unlimited bool   `json:"unlimited"`
}

type shortcutResolveResponse struct {
	OK        bool                       `json:"ok"`
	Version   string                     `json:"version"`
	Track     *musicservice.SearchResult `json:"track"`
	Download  *musicservice.DownloadJob  `json:"download,omitempty"`
	Links     shortcutLinks              `json:"links"`
	Directory string                     `json:"directory"`
	Assets    []shortcutAsset            `json:"assets,omitempty"`
	Quota     *shortcutQuota             `json:"quota,omitempty"`
}

type shortcutPrincipal struct {
	Key    *db.ShortcutAPIKeyModel
	Legacy bool
}

// handleShortcutResolve is the stable transport endpoint for iOS Shortcuts.
// It accepts copied/shared text (not only a bare URL), resolves one supported
// song, and can optionally enqueue a download in the same request.
func (s *Server) handleShortcutResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	principal, ok := s.authenticateShortcutAPIKey(w, r)
	if !ok {
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
	wantsDownload := r.Method == http.MethodPost && (req.PrepareDownload || strings.EqualFold(strings.TrimSpace(req.Action), "download"))
	quality := strings.ToLower(strings.TrimSpace(req.Quality))
	files := req.Files
	if wantsDownload {
		if quality == "" {
			quality = "hires"
		}
		switch quality {
		case "standard", "high", "lossless", "hires":
		default:
			writeError(w, http.StatusBadRequest, "quality 必须是 standard、high、lossless 或 hires")
			return
		}
		if files == 0 {
			files = 3
		}
		if files < 1 || files > 3 {
			writeError(w, http.StatusBadRequest, "files 必须是 1、2 或 3")
			return
		}
	}
	track, err := s.music.ParseLink(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	principal, ok = s.consumeShortcutParse(w, r, principal)
	if !ok {
		return
	}

	base := s.shortcutPublicBaseURL(r)
	response := shortcutResolveResponse{
		OK:        true,
		Version:   "v1",
		Track:     track,
		Directory: safeShortcutName(shortcutPlatformDisplayName(track.Platform)),
		Quota:     shortcutQuotaFromPrincipal(principal),
		Links: shortcutLinks{
			Website:           base + "/",
			DownloadCreateAPI: base + "/api/v1/downloads",
		},
	}

	if wantsDownload {
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
		assetPath := base + "/api/v1/shortcut/assets/" + url.PathEscape(job.JobID)
		artist := shortcutPrimaryArtist(track.Artists)
		musicBase := shortcutFileStem(artist, track.Title, track.Platform)
		response.Assets = append(response.Assets, shortcutAsset{Kind: "music", FileName: musicBase + shortcutAudioExtension(job), URL: assetPath + "/music"})
		if files >= 2 {
			response.Assets = append(response.Assets, shortcutAsset{Kind: "lyrics", FileName: musicBase + ".lrc", URL: assetPath + "/lyrics"})
		}
		if files >= 3 && strings.TrimSpace(track.CoverURL) != "" {
			coverName := shortcutFileStem(artist, firstNonEmpty(track.Album, track.Title), track.Platform) + shortcutCoverExtension(track.CoverURL)
			coverTarget := bestShortcutCoverURL(track.CoverURL)
			coverURL := base + "/api/v1/media/image?download=1&filename=" + url.QueryEscape(coverName) + "&url=" + url.QueryEscape(coverTarget)
			response.Assets = append(response.Assets, shortcutAsset{Kind: "cover", FileName: coverName, URL: coverURL})
		}
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
		if raw := strings.TrimSpace(r.Form.Get("files")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return req, fmt.Errorf("files 必须是整数")
			}
			req.Files = value
		}
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

func (s *Server) authenticateShortcutAPIKey(w http.ResponseWriter, r *http.Request) (*shortcutPrincipal, bool) {
	return s.authenticateShortcutAPIKeyAllowExhausted(w, r, false)
}

func (s *Server) authenticateShortcutAPIKeyAllowExhausted(w http.ResponseWriter, r *http.Request, allowExhausted bool) (*shortcutPrincipal, bool) {
	token := shortcutRequestAPIKey(r)
	if token == "" {
		shortcutAuthError(w, http.StatusUnauthorized, "缺少 API Key")
		return nil, false
	}
	if s != nil && s.core != nil && s.core.DB != nil {
		sum := sha256.Sum256([]byte(token))
		model, err := s.core.DB.FindShortcutAPIKeyByHash(r.Context(), hex.EncodeToString(sum[:]))
		if err == nil {
			if !model.Enabled {
				shortcutAuthError(w, http.StatusForbidden, "API Key 已停用")
				return nil, false
			}
			if !allowExhausted && model.UsageLimit > 0 && model.Used >= model.UsageLimit {
				writeError(w, http.StatusTooManyRequests, "API Key 解析次数已用完")
				return nil, false
			}
			return &shortcutPrincipal{Key: model}, true
		}
		if err != nil && !errors.Is(err, db.ErrShortcutAPIKeyNotFound) {
			writeError(w, http.StatusInternalServerError, "API Key 验证失败")
			return nil, false
		}
	}

	// Backwards compatibility for the original single-key config. New keys are
	// generated and quota-managed from /admin and stored only as hashes.
	want := ""
	if s != nil && s.core != nil && s.core.Config != nil {
		want = strings.TrimSpace(s.core.Config.GetString("WebShortcutAPIKey"))
	}
	if want != "" && len(token) == len(want) && subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1 {
		return &shortcutPrincipal{Legacy: true}, true
	}
	shortcutAuthError(w, http.StatusUnauthorized, "API Key 无效")
	return nil, false
}

func (s *Server) consumeShortcutParse(w http.ResponseWriter, r *http.Request, principal *shortcutPrincipal) (*shortcutPrincipal, bool) {
	if principal == nil || principal.Legacy || principal.Key == nil {
		return principal, true
	}
	model, err := s.core.DB.ConsumeShortcutAPIKey(r.Context(), principal.Key.KeyID)
	switch {
	case errors.Is(err, db.ErrShortcutAPIKeyExhausted):
		writeError(w, http.StatusTooManyRequests, "API Key 解析次数已用完")
		return nil, false
	case errors.Is(err, db.ErrShortcutAPIKeyDisabled):
		shortcutAuthError(w, http.StatusForbidden, "API Key 已停用")
		return nil, false
	case err != nil:
		writeError(w, http.StatusInternalServerError, "更新 API Key 使用次数失败")
		return nil, false
	default:
		principal.Key = model
		return principal, true
	}
}

func shortcutQuotaFromPrincipal(principal *shortcutPrincipal) *shortcutQuota {
	if principal == nil {
		return nil
	}
	if principal.Legacy || principal.Key == nil {
		return &shortcutQuota{Unlimited: true}
	}
	q := &shortcutQuota{KeyID: principal.Key.KeyID, Used: principal.Key.Used, Limit: principal.Key.UsageLimit, Unlimited: principal.Key.UsageLimit == 0}
	if principal.Key.UsageLimit > principal.Key.Used {
		q.Remaining = principal.Key.UsageLimit - principal.Key.Used
	}
	return q
}

func shortcutRequestAPIKey(r *http.Request) string {
	got := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		got = strings.TrimSpace(auth[len("bearer "):])
	}
	return got
}

func shortcutAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="musicweb-shortcut"`)
	writeError(w, status, message)
}

func (s *Server) handleShortcutAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	// A key that spent its final parse is still allowed to retrieve the assets
	// from that parse; asset reads do not consume additional quota.
	if _, ok := s.authenticateShortcutAPIKeyAllowExhausted(w, r, true); !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/shortcut/assets/"), "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	job, ok := s.music.GetJob(parts[0])
	if !ok {
		writeError(w, http.StatusNotFound, "下载任务不存在或已过期")
		return
	}
	artist := shortcutPrimaryArtist(job.Artists)
	stem := shortcutFileStem(artist, job.Title, job.Platform)
	switch parts[1] {
	case "music":
		if job.Status != "ready" {
			writeError(w, http.StatusConflict, "音乐文件尚未准备好")
			return
		}
		file, err := os.Open(job.FilePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "音乐文件不存在或已过期")
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || stat.IsDir() {
			writeError(w, http.StatusNotFound, "音乐文件不可读")
			return
		}
		name := stem + shortcutAudioExtension(job)
		w.Header().Set("Content-Disposition", musicservice.LyricsContentDisposition(name))
		http.ServeContent(w, r, name, stat.ModTime(), file)
	case "lyrics":
		doc, err := s.music.CreateLyrics(r.Context(), musicservice.LyricsRequest{Platform: job.Platform, TrackID: job.TrackID, Format: "lrc", IncludeTranslation: true, IncludeRoma: true})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		name := stem + ".lrc"
		w.Header().Set("Content-Type", doc.ContentType)
		w.Header().Set("Content-Disposition", musicservice.LyricsContentDisposition(name))
		w.Header().Set("Content-Length", strconv.Itoa(len(doc.Content)))
		_, _ = w.Write(doc.Content)
	default:
		http.NotFound(w, r)
	}
}

func shortcutPrimaryArtist(artists []string) string {
	values := make([]string, 0, len(artists))
	for _, artist := range artists {
		if artist = strings.TrimSpace(artist); artist != "" {
			values = append(values, artist)
		}
	}
	if len(values) == 0 {
		return "未知歌手"
	}
	return strings.Join(values, "、")
}

func shortcutFileStem(artist, title, platformName string) string {
	return safeShortcutName(artist) + "-" + safeShortcutName(firstNonEmpty(title, "未知歌曲")) + "-" + safeShortcutName(shortcutPlatformDisplayName(platformName))
}

func safeShortcutName(value string) string {
	value = strings.TrimSpace(value)
	value = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`).ReplaceAllString(value, "_")
	value = strings.Trim(value, ". ")
	if value == "" {
		return "unknown"
	}
	runes := []rune(value)
	if len(runes) > 120 {
		value = string(runes[:120])
	}
	return value
}

func shortcutPlatformDisplayName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "netease":
		return "网易云音乐"
	case "qqmusic":
		return "QQ音乐"
	case "applemusic":
		return "Apple Music"
	case "spotify":
		return "Spotify"
	case "youtubemusic":
		return "YouTube Music"
	case "kugou":
		return "酷狗音乐"
	case "bilibili":
		return "哔哩哔哩"
	case "soda":
		return "汽水音乐"
	default:
		return firstNonEmpty(name, "未知平台")
	}
}

func shortcutAudioExtension(job *musicservice.DownloadJob) string {
	if job == nil {
		return ""
	}
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(job.Format)), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(filepath.Ext(job.FilePath)), ".")
	}
	if !regexp.MustCompile(`^[a-z0-9]{1,8}$`).MatchString(ext) {
		return ""
	}
	return "." + ext
}

func shortcutCoverExtension(raw string) string {
	if parsed, err := url.Parse(raw); err == nil {
		ext := strings.ToLower(filepath.Ext(parsed.Path))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".avif":
			return ext
		}
	}
	return ".jpg"
}

var appleCoverSizePattern = regexp.MustCompile(`/\d+x\d+(?:bb|cc)?\.`)
var qqCoverSizePattern = regexp.MustCompile(`R\d+x\d+M`)

func bestShortcutCoverURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.HasSuffix(host, "music.126.net") || strings.HasSuffix(host, "music.163.com"):
		query := parsed.Query()
		query.Set("param", "3000y3000")
		parsed.RawQuery = query.Encode()
	case strings.HasSuffix(host, "mzstatic.com"):
		parsed.Path = appleCoverSizePattern.ReplaceAllString(parsed.Path, "/3000x3000bb.")
	case strings.HasSuffix(host, "y.qq.com") || strings.HasSuffix(host, "gtimg.cn"):
		parsed.Path = qqCoverSizePattern.ReplaceAllString(parsed.Path, "R1500x1500M")
	case strings.HasSuffix(host, "googleusercontent.com"):
		parsed.Path = regexp.MustCompile(`=w\d+-h\d+.*$`).ReplaceAllString(parsed.Path, "=w2000-h2000")
	}
	return parsed.String()
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
