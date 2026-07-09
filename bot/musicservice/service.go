package musicservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/app"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type PlatformInfo struct {
	Name         string                `json:"name"`
	DisplayName  string                `json:"display_name"`
	Emoji        string                `json:"emoji"`
	Aliases      []string              `json:"aliases,omitempty"`
	Capabilities platform.Capabilities `json:"capabilities"`
}

type SearchResult struct {
	TrackID         string            `json:"track_id"`
	Platform        string            `json:"platform"`
	Title           string            `json:"title"`
	Artists         []string          `json:"artists"`
	Album           string            `json:"album,omitempty"`
	DurationMS      int64             `json:"duration_ms"`
	CoverURL        string            `json:"cover_url,omitempty"`
	URL             string            `json:"url,omitempty"`
	ISRC            string            `json:"isrc,omitempty"`
	LyricsAvailable *bool             `json:"lyrics_available,omitempty"`
	Raw             *platform.Track   `json:"-"`
	Qualities       []QualityOption   `json:"qualities"`
	Extra           map[string]string `json:"extra,omitempty"`
}

type QualityOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type DownloadRequest struct {
	Platform string `json:"platform"`
	TrackID  string `json:"track_id"`
	Quality  string `json:"quality"`
}

type DownloadJob struct {
	JobID     string    `json:"job_id"`
	Platform  string    `json:"platform"`
	TrackID   string    `json:"track_id"`
	Quality   string    `json:"quality"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Error     string    `json:"error,omitempty"`
	Title     string    `json:"title,omitempty"`
	Artists   []string  `json:"artists,omitempty"`
	Album     string    `json:"album,omitempty"`
	FileName  string    `json:"file_name,omitempty"`
	FilePath  string    `json:"-"`
	FileSize  int64     `json:"file_size,omitempty"`
	Format    string    `json:"format,omitempty"`
	Bitrate   int       `json:"bitrate,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
	CacheKey  string    `json:"-"`
}

type Service struct {
	platforms  platform.Manager
	downloader *download.DownloadService
	tags       *id3.ID3Service
	taggers    map[string]id3.ID3TagProvider
	cacheDir   string
	ttl        time.Duration
	sem        chan struct{}
	logger     botpkg.Logger
	repo       *db.Repository

	mu    sync.RWMutex
	jobs  map[string]*DownloadJob
	byKey map[string]string
}

func New(core *app.Core) *Service {
	cacheDir := "./cache/web"
	ttl := 24 * time.Hour
	concurrency := 4
	cleanupInterval := 30 * time.Minute
	var dlTimeout time.Duration
	var proxy string
	var checkMD5 bool
	var retries int
	var multipart bool
	var multipartConcurrency int
	var multipartMinSize int64
	if core != nil && core.Config != nil {
		if v := strings.TrimSpace(core.Config.GetString("WebDownloadCacheDir")); v != "" {
			cacheDir = v
		}
		if hours := core.Config.GetInt("WebDownloadTTLHours"); hours > 0 {
			ttl = time.Duration(hours) * time.Hour
		}
		if v := core.Config.GetInt("WebMaxConcurrentDownloads"); v > 0 {
			concurrency = v
		}
		if minutes := core.Config.GetInt("WebDownloadCleanupIntervalMinutes"); minutes > 0 {
			cleanupInterval = time.Duration(minutes) * time.Minute
		}
		dlTimeout = time.Duration(core.Config.GetInt("DownloadTimeout")) * time.Second
		proxy = core.Config.GetString("DownloadProxy")
		checkMD5 = core.Config.GetBool("CheckMD5")
		retries = core.Config.GetInt("DownloadMaxRetries")
		multipart = core.Config.GetBool("EnableMultipartDownload")
		multipartConcurrency = core.Config.GetInt("MultipartConcurrency")
		multipartMinSize = int64(core.Config.GetInt("MultipartMinSizeMB")) * 1024 * 1024
	}
	if dlTimeout <= 0 {
		dlTimeout = 60 * time.Second
	}
	var platforms platform.Manager
	var taggers map[string]id3.ID3TagProvider
	var logger botpkg.Logger
	var repo *db.Repository
	if core != nil {
		platforms = core.PlatformManager
		taggers = core.TagProviders
		logger = core.Logger
		repo = core.DB
	}
	svc := &Service{
		platforms: platforms,
		downloader: download.NewDownloadService(download.DownloadServiceOptions{
			Timeout:              dlTimeout,
			Proxy:                proxy,
			CheckMD5:             checkMD5,
			MaxRetries:           retries,
			EnableMultipart:      multipart,
			MultipartConcurrency: multipartConcurrency,
			MultipartMinSize:     multipartMinSize,
		}),
		tags:     id3.NewID3Service(logger),
		taggers:  taggers,
		cacheDir: cacheDir,
		ttl:      ttl,
		sem:      make(chan struct{}, concurrency),
		logger:   logger,
		repo:     repo,
		jobs:     make(map[string]*DownloadJob),
		byKey:    make(map[string]string),
	}
	if repo != nil {
		if err := repo.MarkInterruptedWebDownloadJobs(context.Background()); err != nil && logger != nil {
			logger.Warn("failed to mark interrupted web download jobs", "error", err)
		}
	}
	if cleanupInterval > 0 {
		go svc.StartCleanupLoop(context.Background(), cleanupInterval)
	}
	return svc
}

func (s *Service) Platforms() []PlatformInfo {
	if s == nil || s.platforms == nil {
		return nil
	}
	metas := s.platforms.ListMeta()
	out := make([]PlatformInfo, 0, len(metas))
	for _, meta := range metas {
		plat := s.platforms.Get(meta.Name)
		if plat == nil {
			continue
		}
		out = append(out, PlatformInfo{
			Name:         meta.Name,
			DisplayName:  fallback(meta.DisplayName, meta.Name),
			Emoji:        fallback(meta.Emoji, "🎵"),
			Aliases:      meta.Aliases,
			Capabilities: plat.Capabilities(),
		})
	}
	return out
}

func (s *Service) Search(ctx context.Context, platformName, query string, limit int) ([]SearchResult, error) {
	if s == nil || s.platforms == nil {
		return nil, errors.New("platform manager not configured")
	}
	platformName = strings.TrimSpace(platformName)
	query = strings.TrimSpace(query)
	if platformName == "" || query == "" {
		return nil, errors.New("platform and query are required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	plat := s.platforms.Get(platformName)
	if plat == nil {
		return nil, fmt.Errorf("unknown platform: %s", platformName)
	}
	if !plat.SupportsSearch() {
		return nil, platform.ErrUnsupported
	}
	tracks, err := plat.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, trackToResult(platformName, track))
	}
	return out, nil
}

func (s *Service) CreateDownload(ctx context.Context, req DownloadRequest) (*DownloadJob, error) {
	if s == nil || s.platforms == nil {
		return nil, errors.New("music service not configured")
	}
	req.Platform = strings.TrimSpace(req.Platform)
	req.TrackID = strings.TrimSpace(req.TrackID)
	if strings.TrimSpace(req.Quality) == "" {
		req.Quality = platform.QualityHigh.String()
	}
	quality, err := platform.ParseQuality(req.Quality)
	if err != nil {
		return nil, err
	}
	plat := s.platforms.Get(req.Platform)
	if plat == nil {
		return nil, fmt.Errorf("unknown platform: %s", req.Platform)
	}
	if req.TrackID == "" {
		return nil, errors.New("track_id is required")
	}
	key := cacheKey(req.Platform, req.TrackID, quality.String())

	s.mu.Lock()
	if existingID := s.byKey[key]; existingID != "" {
		if existing := s.jobs[existingID]; existing != nil {
			if existing.Status == "ready" && fileExists(existing.FilePath) && time.Now().Before(existing.ExpiresAt) {
				clone := *existing
				s.mu.Unlock()
				return &clone, nil
			}
			if existing.Status == "queued" || existing.Status == "running" || existing.Status == "tagging" {
				clone := *existing
				s.mu.Unlock()
				return &clone, nil
			}
		}
	}
	s.mu.Unlock()

	if s.repo != nil {
		if record, err := s.repo.FindReadyWebDownloadJobByCacheKey(ctx, key, time.Now()); err == nil {
			if job := jobFromRecord(record); fileExists(job.FilePath) {
				s.rememberJob(job)
				return job, nil
			}
		}
	}

	s.mu.Lock()
	job := &DownloadJob{
		JobID:     newJobID(),
		Platform:  req.Platform,
		TrackID:   req.TrackID,
		Quality:   quality.String(),
		Status:    "queued",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
		CacheKey:  key,
	}
	s.jobs[job.JobID] = job
	s.byKey[key] = job.JobID
	clone := *job
	s.mu.Unlock()
	s.persistJob(&clone)

	go s.runDownload(context.Background(), job.JobID)
	return &clone, nil
}

func (s *Service) GetJob(jobID string) (*DownloadJob, bool) {
	s.mu.RLock()
	job := s.jobs[strings.TrimSpace(jobID)]
	if job != nil {
		clone := *job
		s.mu.RUnlock()
		return &clone, true
	}
	s.mu.RUnlock()
	if s.repo != nil {
		if record, err := s.repo.FindWebDownloadJob(context.Background(), strings.TrimSpace(jobID)); err == nil {
			job := jobFromRecord(record)
			if isActiveStatus(job.Status) {
				job.Status = "failed"
				job.Progress = 100
				job.Error = "任务因服务重启中断，请重新创建下载任务"
				job.UpdatedAt = time.Now()
				s.persistJob(job)
			}
			s.rememberJob(job)
			return job, true
		}
	}
	return nil, false
}

func (s *Service) runDownload(ctx context.Context, jobID string) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		s.failJob(jobID, ctx.Err())
		return
	}
	s.setJob(jobID, func(j *DownloadJob) {
		j.Status = "running"
		j.Progress = 1
	})

	job, ok := s.GetJob(jobID)
	if !ok {
		return
	}
	plat := s.platforms.Get(job.Platform)
	if plat == nil {
		s.failJob(jobID, fmt.Errorf("unknown platform: %s", job.Platform))
		return
	}
	quality, err := platform.ParseQuality(job.Quality)
	if err != nil {
		s.failJob(jobID, err)
		return
	}
	track, err := plat.GetTrack(ctx, job.TrackID)
	if err != nil {
		s.failJob(jobID, err)
		return
	}
	if track == nil {
		s.failJob(jobID, platform.ErrNotFound)
		return
	}
	s.setJob(jobID, func(j *DownloadJob) {
		j.Title = track.Title
		j.Artists = artistNames(track.Artists)
		if track.Album != nil {
			j.Album = track.Album.Title
		}
		j.Progress = 5
	})

	info, err := plat.GetDownloadInfo(ctx, job.TrackID, quality)
	if err != nil {
		s.failJob(jobID, err)
		return
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		s.failJob(jobID, errors.New("download url unavailable"))
		return
	}
	if info.Format == "" {
		info.Format = "mp3"
	}
	dir := filepath.Join(s.cacheDir, safePathPart(job.Platform), safePathPart(job.TrackID), quality.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.failJob(jobID, err)
		return
	}
	fileName := sanitizeFileName(fmt.Sprintf("%s - %s.%s", strings.Join(artistNames(track.Artists), ","), track.Title, info.Format))
	if strings.Trim(fileName, ". ") == "" {
		fileName = sanitizeFileName(fmt.Sprintf("%s.%s", job.TrackID, info.Format))
	}
	filePath := filepath.Join(dir, fileName)
	coverPath := ""
	progress := func(written, total int64) {
		if total <= 0 {
			return
		}
		pct := 5 + int(float64(written)*80/float64(total))
		if pct > 85 {
			pct = 85
		}
		s.setJob(jobID, func(j *DownloadJob) {
			if pct > j.Progress {
				j.Progress = pct
			}
		})
	}
	if info.Downloader != nil {
		_, err = info.Downloader(ctx, info, filePath, progress)
	} else {
		_, err = s.downloader.Download(ctx, info, filePath, progress)
	}
	if err != nil {
		_ = os.Remove(filePath)
		s.failJob(jobID, err)
		return
	}

	s.setJob(jobID, func(j *DownloadJob) {
		j.Status = "tagging"
		j.Progress = 88
	})
	coverPath = s.downloadCover(ctx, dir, track)
	s.embedTags(ctx, plat, track, info, filePath, coverPath)
	if coverPath != "" {
		_ = os.Remove(coverPath)
	}
	stat, _ := os.Stat(filePath)
	s.setJob(jobID, func(j *DownloadJob) {
		j.Status = "ready"
		j.Progress = 100
		j.FileName = fileName
		j.FilePath = filePath
		j.Format = info.Format
		j.Bitrate = info.Bitrate
		if stat != nil {
			j.FileSize = stat.Size()
		}
	})
}

func (s *Service) downloadCover(ctx context.Context, dir string, track *platform.Track) string {
	if s == nil || s.downloader == nil || track == nil {
		return ""
	}
	coverURL := strings.TrimSpace(track.CoverURL)
	if coverURL == "" && track.Album != nil {
		coverURL = strings.TrimSpace(track.Album.CoverURL)
	}
	if coverURL == "" {
		return ""
	}
	ext := ".jpg"
	if u, err := url.Parse(coverURL); err == nil {
		if got := path.Ext(u.Path); got != "" && len(got) <= 8 {
			ext = got
		}
	}
	dest := filepath.Join(dir, "cover"+ext)
	if _, err := s.downloader.Download(ctx, &platform.DownloadInfo{URL: coverURL}, dest, nil); err != nil {
		if s.logger != nil {
			s.logger.Warn("web cover download failed", "error", err)
		}
		return ""
	}
	return dest
}

func (s *Service) embedTags(ctx context.Context, plat platform.Platform, track *platform.Track, info *platform.DownloadInfo, filePath, coverPath string) {
	if s == nil || s.tags == nil || plat == nil || track == nil {
		return
	}
	var tag *id3.TagData
	if s.taggers != nil {
		if provider := s.taggers[plat.Name()]; provider != nil {
			if got, err := provider.GetTagData(ctx, track, info); err == nil {
				tag = got
			}
		}
	}
	if tag == nil {
		tag = fallbackTag(track)
	}
	if err := s.tags.EmbedTags(filePath, tag, coverPath); err != nil && s.logger != nil {
		s.logger.Warn("web tag embedding skipped", "platform", plat.Name(), "track", track.ID, "error", err)
	}
}

func (s *Service) setJob(jobID string, update func(*DownloadJob)) {
	s.mu.Lock()
	var clone *DownloadJob
	if job := s.jobs[jobID]; job != nil {
		update(job)
		job.UpdatedAt = time.Now()
		copied := *job
		clone = &copied
	}
	s.mu.Unlock()
	if clone != nil {
		s.persistJob(clone)
	}
}

func (s *Service) failJob(jobID string, err error) {
	s.setJob(jobID, func(j *DownloadJob) {
		j.Status = "failed"
		j.Progress = 100
		if err != nil {
			j.Error = err.Error()
		}
	})
}

func (s *Service) rememberJob(job *DownloadJob) {
	if s == nil || job == nil {
		return
	}
	s.mu.Lock()
	s.jobs[job.JobID] = job
	if strings.TrimSpace(job.CacheKey) != "" {
		s.byKey[job.CacheKey] = job.JobID
	}
	s.mu.Unlock()
}

func (s *Service) persistJob(job *DownloadJob) {
	if s == nil || s.repo == nil || job == nil {
		return
	}
	if err := s.repo.SaveWebDownloadJob(context.Background(), recordFromJob(job)); err != nil && s.logger != nil {
		s.logger.Warn("failed to persist web download job", "job_id", job.JobID, "error", err)
	}
}

func recordFromJob(job *DownloadJob) *db.WebDownloadJobModel {
	if job == nil {
		return nil
	}
	artists, _ := json.Marshal(job.Artists)
	record := &db.WebDownloadJobModel{
		JobID:     job.JobID,
		CacheKey:  job.CacheKey,
		Platform:  job.Platform,
		TrackID:   job.TrackID,
		Quality:   job.Quality,
		Status:    job.Status,
		Progress:  job.Progress,
		Error:     job.Error,
		Title:     job.Title,
		Artists:   string(artists),
		Album:     job.Album,
		FilePath:  job.FilePath,
		FileName:  job.FileName,
		FileSize:  job.FileSize,
		Format:    job.Format,
		Bitrate:   job.Bitrate,
		ExpiresAt: job.ExpiresAt,
	}
	if !job.CreatedAt.IsZero() {
		record.CreatedAt = job.CreatedAt
	}
	if !job.UpdatedAt.IsZero() {
		record.UpdatedAt = job.UpdatedAt
	}
	return record
}

func jobFromRecord(record *db.WebDownloadJobModel) *DownloadJob {
	if record == nil {
		return nil
	}
	var artists []string
	_ = json.Unmarshal([]byte(record.Artists), &artists)
	return &DownloadJob{
		JobID:     record.JobID,
		Platform:  record.Platform,
		TrackID:   record.TrackID,
		Quality:   record.Quality,
		Status:    record.Status,
		Progress:  record.Progress,
		Error:     record.Error,
		Title:     record.Title,
		Artists:   artists,
		Album:     record.Album,
		FileName:  record.FileName,
		FilePath:  record.FilePath,
		FileSize:  record.FileSize,
		Format:    record.Format,
		Bitrate:   record.Bitrate,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
		ExpiresAt: record.ExpiresAt,
		CacheKey:  record.CacheKey,
	}
}

func isActiveStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "queued", "running", "tagging":
		return true
	default:
		return false
	}
}

func trackToResult(platformName string, track platform.Track) SearchResult {
	album := ""
	coverURL := track.CoverURL
	if track.Album != nil {
		album = track.Album.Title
		if strings.TrimSpace(coverURL) == "" {
			coverURL = track.Album.CoverURL
		}
	}
	return SearchResult{
		TrackID:         track.ID,
		Platform:        fallback(track.Platform, platformName),
		Title:           track.Title,
		Artists:         artistNames(track.Artists),
		Album:           album,
		DurationMS:      track.Duration.Milliseconds(),
		CoverURL:        coverURL,
		URL:             track.URL,
		ISRC:            track.ISRC,
		LyricsAvailable: track.LyricsAvailable,
		Raw:             &track,
		Qualities:       DefaultQualities(),
	}
}

func DefaultQualities() []QualityOption {
	return []QualityOption{
		{Value: "standard", Label: "标准"},
		{Value: "high", Label: "高音质"},
		{Value: "lossless", Label: "无损"},
		{Value: "hires", Label: "Hi-Res"},
	}
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func artistNames(artists []platform.Artist) []string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if strings.TrimSpace(artist.Name) != "" {
			names = append(names, artist.Name)
		}
	}
	return names
}

func fallbackTag(track *platform.Track) *id3.TagData {
	if track == nil {
		return &id3.TagData{}
	}
	album := ""
	if track.Album != nil {
		album = track.Album.Title
	}
	return &id3.TagData{
		Title:       track.Title,
		Artist:      strings.Join(artistNames(track.Artists), "/"),
		Album:       album,
		AlbumArtist: strings.Join(artistNames(track.Artists), "/"),
		Year:        yearString(track.Year),
		TrackNumber: track.TrackNumber,
		DiscNumber:  track.DiscNumber,
		CoverURL:    track.CoverURL,
	}
}

func yearString(year int) string {
	if year <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", year)
}

func cacheKey(platformName, trackID, quality string) string {
	return strings.TrimSpace(platformName) + ":" + strings.TrimSpace(trackID) + ":" + strings.TrimSpace(quality)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}

func newJobID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "dl_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("dl_%d", time.Now().UnixNano())
}

func safePathPart(value string) string {
	value = sanitizeFileName(value)
	if value == "" {
		return "_"
	}
	return value
}

func sanitizeFileName(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ")
	value = strings.TrimSpace(replacer.Replace(value))
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	if len([]rune(value)) > 180 {
		r := []rune(value)
		value = string(r[:180])
	}
	return value
}

// ListJobs returns persisted web download jobs, newest first.
func (s *Service) ListJobs(ctx context.Context, limit, offset int) ([]*DownloadJob, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("repository not configured")
	}
	records, err := s.repo.ListWebDownloadJobs(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	jobs := make([]*DownloadJob, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, jobFromRecord(record))
	}
	return jobs, nil
}

// CleanupExpired removes expired web download jobs and their local files.
func (s *Service) CleanupExpired(ctx context.Context) (int, int, error) {
	if s == nil || s.repo == nil {
		return 0, 0, errors.New("repository not configured")
	}
	records, err := s.repo.FindExpiredWebDownloadJobs(ctx, time.Now(), 500)
	if err != nil {
		return 0, 0, err
	}
	if len(records) == 0 {
		return 0, 0, nil
	}
	jobIDs := make([]string, 0, len(records))
	filesRemoved := 0
	for _, record := range records {
		jobIDs = append(jobIDs, record.JobID)
		if strings.TrimSpace(record.FilePath) != "" {
			if err := os.Remove(record.FilePath); err == nil {
				filesRemoved++
			}
			cleanupEmptyParents(record.FilePath, s.cacheDir)
		}
		s.mu.Lock()
		delete(s.jobs, record.JobID)
		if strings.TrimSpace(record.CacheKey) != "" && s.byKey[record.CacheKey] == record.JobID {
			delete(s.byKey, record.CacheKey)
		}
		s.mu.Unlock()
	}
	if err := s.repo.DeleteWebDownloadJobs(ctx, jobIDs); err != nil {
		return filesRemoved, 0, err
	}
	return filesRemoved, len(jobIDs), nil
}

// StartCleanupLoop periodically removes expired web downloads.
func (s *Service) StartCleanupLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	_, _, _ = s.CleanupExpired(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			files, jobs, err := s.CleanupExpired(ctx)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("web download cleanup failed", "error", err)
				}
				continue
			}
			if (files > 0 || jobs > 0) && s.logger != nil {
				s.logger.Info("web download cleanup finished", "files", files, "jobs", jobs)
			}
		}
	}
}

func cleanupEmptyParents(filePath, stopDir string) {
	stopDir, _ = filepath.Abs(stopDir)
	dir := filepath.Dir(filePath)
	for i := 0; i < 4; i++ {
		abs, err := filepath.Abs(dir)
		if err != nil || abs == stopDir || !strings.HasPrefix(abs, stopDir) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
