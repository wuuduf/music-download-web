package server

import (
	"net/http"
	"strconv"
	"time"
)

// handleAdminCacheStats reports current cache usage and the configured limits.
func (s *Server) handleAdminCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	stats, err := s.music.CacheStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_bytes": stats.TotalBytes,
		"file_count":  stats.FileCount,
		"job_count":   stats.JobCount,
		"max_bytes":   s.music.CacheMaxBytes(),
		"ttl_hours":   int(s.music.TTL() / time.Hour),
	})
}

// handleAdminCacheSettings persists the retention TTL and cache size budget,
// applying them to the running service immediately.
func (s *Server) handleAdminCacheSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	var body struct {
		TTLHours int `json:"ttl_hours"`
		MaxMB    int `json:"max_mb"`
	}
	if !decodeJSON(w, r, 16<<10, &body) {
		return
	}
	if body.TTLHours < 1 || body.TTLHours > 24*365 {
		writeError(w, http.StatusBadRequest, "缓存保留时长必须在 1 小时到 1 年之间")
		return
	}
	if body.MaxMB < 0 || body.MaxMB > 1024*1024 {
		writeError(w, http.StatusBadRequest, "缓存上限必须在 0（不限制）到 1048576 MB 之间")
		return
	}
	if err := s.core.Config.PersistAdminConfig(map[string]string{
		"WebDownloadTTLHours":   strconv.Itoa(body.TTLHours),
		"WebDownloadCacheMaxMB": strconv.Itoa(body.MaxMB),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存设置失败: "+err.Error())
		return
	}
	s.music.SetCacheLimits(time.Duration(body.TTLHours)*time.Hour, int64(body.MaxMB)*1024*1024)
	// Apply the new budget right away so shrinking the limit frees disk now.
	files, jobs, err := s.music.EnforceCacheBudget(r.Context(), s.music.CacheMaxBytes())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "设置已保存，但清理失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "files_removed": files, "jobs_removed": jobs,
		"message": "设置已保存并立即生效",
	})
}

// handleAdminDownloadDelete removes specific jobs (or every cached job when
// "all" is set) regardless of expiry.
func (s *Server) handleAdminDownloadDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		JobIDs []string `json:"job_ids"`
		All    bool     `json:"all"`
	}
	if !decodeJSON(w, r, 256<<10, &body) {
		return
	}
	if body.All {
		files, jobs, err := s.music.DeleteAllJobs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"files_removed": files, "jobs_removed": jobs})
		return
	}
	if len(body.JobIDs) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要删除的任务")
		return
	}
	files, jobs, err := s.music.DeleteJobs(r.Context(), body.JobIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files_removed": files, "jobs_removed": jobs})
}
