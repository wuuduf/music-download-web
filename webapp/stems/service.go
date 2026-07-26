package stems

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config interface {
	GetString(string) string
	GetInt(string) int
	GetBool(string) bool
}

type Capabilities struct {
	Enabled            bool     `json:"enabled"`
	Ready              bool     `json:"ready"`
	Reason             string   `json:"reason,omitempty"`
	Engine             string   `json:"engine"`
	BaseModel          string   `json:"base_model"`
	MaxConcurrent      int      `json:"max_concurrent"`
	RefinementEnabled  bool     `json:"refinement_enabled"`
	RefinementReady    bool     `json:"refinement_ready"`
	RefinementReason   string   `json:"refinement_reason,omitempty"`
	ConfiguredRefiners []string `json:"configured_refiners,omitempty"`
}

type StartOptions struct {
	Refine bool `json:"refine"`
}

type Track struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Stem         string `json:"stem"`
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	RelativePath string `json:"relative_path,omitempty"`
	URL          string `json:"url,omitempty"`
	Preferred    bool   `json:"preferred"`
	Parent       string `json:"parent,omitempty"`
	SampleRate   int    `json:"sample_rate,omitempty"`
	Channels     int    `json:"channels,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
}

type Job struct {
	JobID               string    `json:"job_id"`
	ProjectID           string    `json:"project_id"`
	Status              string    `json:"status"`
	Stage               string    `json:"stage"`
	Progress            int       `json:"progress"`
	Message             string    `json:"message,omitempty"`
	Error               string    `json:"error,omitempty"`
	RefinementRequested bool      `json:"refinement_requested"`
	Warnings            []string  `json:"warnings,omitempty"`
	Tracks              []Track   `json:"tracks,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	workDir      string
	outputDir    string
	progressPath string
	manifestPath string
}

type progressFile struct {
	Stage    string   `json:"stage"`
	Progress int      `json:"progress"`
	Message  string   `json:"message"`
	Error    string   `json:"error"`
	Warnings []string `json:"warnings"`
}

type manifestFile struct {
	Version             int      `json:"version"`
	Status              string   `json:"status"`
	RefinementRequested bool     `json:"refinement_requested"`
	Warnings            []string `json:"warnings"`
	Tracks              []Track  `json:"tracks"`
}

type Service struct {
	enabled           bool
	python            string
	script            string
	workDir           string
	max               int
	model             string
	device            string
	shifts            int
	overlap           float64
	refinementEnabled bool
	separatorBinary   string
	vocalModel        string
	backingPreset     string
	pianoModel        string
	guitarModel       string
	ttl               time.Duration

	mu      sync.RWMutex
	jobs    map[string]*Job
	active  map[string]string
	workers chan struct{}
}

var safeTrackID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func New(cfg Config) *Service {
	service := &Service{
		python:          "/opt/musicweb/stem-venv/bin/python",
		script:          "./stem-worker/worker.py",
		workDir:         "./cache/stems",
		max:             1,
		model:           "htdemucs_6s",
		device:          "auto",
		shifts:          1,
		overlap:         0.25,
		separatorBinary: "audio-separator",
		vocalModel:      "model_mel_band_roformer_ep_3005_sdr_11.4360.ckpt",
		backingPreset:   "karaoke",
		ttl:             24 * time.Hour,
		jobs:            make(map[string]*Job),
		active:          make(map[string]string),
	}
	if cfg != nil {
		service.enabled = cfg.GetBool("WebStemEnabled")
		service.refinementEnabled = cfg.GetBool("WebStemRefinementEnabled")
		setString := func(key string, target *string) {
			if value := strings.TrimSpace(cfg.GetString(key)); value != "" {
				*target = value
			}
		}
		setString("WebStemPython", &service.python)
		setString("WebStemScript", &service.script)
		setString("WebStemWorkDir", &service.workDir)
		setString("WebStemDemucsModel", &service.model)
		setString("WebStemDevice", &service.device)
		setString("WebStemSeparatorBinary", &service.separatorBinary)
		setString("WebStemVocalModel", &service.vocalModel)
		setString("WebStemBackingPreset", &service.backingPreset)
		service.pianoModel = strings.TrimSpace(cfg.GetString("WebStemPianoModel"))
		service.guitarModel = strings.TrimSpace(cfg.GetString("WebStemGuitarModel"))
		if value := cfg.GetInt("WebStemMaxConcurrent"); value > 0 && value <= 4 {
			service.max = value
		}
		if value := cfg.GetInt("WebStemShifts"); value >= 0 && value <= 10 {
			service.shifts = value
		}
		if value := cfg.GetInt("WebStemTTLHours"); value > 0 && value <= 24*30 {
			service.ttl = time.Duration(value) * time.Hour
		}
		if value, err := strconv.ParseFloat(strings.TrimSpace(cfg.GetString("WebStemOverlap")), 64); err == nil && value >= 0 && value < 1 {
			service.overlap = value
		}
	}
	service.workers = make(chan struct{}, service.max)
	return service
}

func (s *Service) Capabilities() Capabilities {
	capability := Capabilities{
		Enabled: s != nil && s.enabled,
		Engine:  "Demucs six-stem + optional audio-separator refiners",
	}
	if s == nil {
		capability.Reason = "分轨服务未配置"
		return capability
	}
	capability.BaseModel = s.model
	capability.MaxConcurrent = s.max
	capability.RefinementEnabled = s.refinementEnabled
	capability.ConfiguredRefiners = s.configuredRefiners()
	if !s.enabled {
		capability.Reason = "管理员尚未启用多阶段分轨服务"
		return capability
	}
	if !commandExists(s.python) {
		capability.Reason = "找不到分轨 Python 环境：" + s.python
		return capability
	}
	if info, err := os.Stat(s.script); err != nil || info.IsDir() {
		capability.Reason = "找不到分轨 Worker：" + s.script
		return capability
	}
	capability.Ready = true
	if !s.refinementEnabled {
		capability.RefinementReason = "第二阶段精修未启用"
		return capability
	}
	if !commandExists(s.separatorBinary) {
		capability.RefinementReason = "找不到 audio-separator：" + s.separatorBinary
		return capability
	}
	capability.RefinementReady = true
	return capability
}

func (s *Service) configuredRefiners() []string {
	values := make([]string, 0, 4)
	if strings.TrimSpace(s.vocalModel) != "" {
		values = append(values, "vocals:"+s.vocalModel)
	}
	if strings.TrimSpace(s.backingPreset) != "" {
		values = append(values, "backing:preset:"+s.backingPreset)
	}
	if strings.TrimSpace(s.pianoModel) != "" {
		values = append(values, "piano:"+s.pianoModel)
	}
	if strings.TrimSpace(s.guitarModel) != "" {
		values = append(values, "guitar:"+s.guitarModel)
	}
	return values
}

func commandExists(command string) bool {
	if info, err := os.Stat(command); err == nil && !info.IsDir() {
		return true
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func (s *Service) Start(projectID, audioPath string, options StartOptions) (*Job, error) {
	capability := s.Capabilities()
	if !capability.Ready {
		return nil, errors.New(capability.Reason)
	}
	projectID = strings.TrimSpace(projectID)
	audioPath = strings.TrimSpace(audioPath)
	if projectID == "" || audioPath == "" {
		return nil, errors.New("project_id 和音频文件必填")
	}
	if info, err := os.Stat(audioPath); err != nil || info.IsDir() {
		return nil, errors.New("项目音频尚未准备好")
	}

	s.mu.Lock()
	if existingID := s.active[projectID]; existingID != "" {
		if existing := s.jobs[existingID]; existing != nil {
			clone := s.decorateJob(existing)
			s.mu.Unlock()
			return clone, nil
		}
	}
	jobID := newID()
	workDir := filepath.Join(s.workDir, jobID)
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("创建分轨目录: %w", err)
	}
	now := time.Now()
	job := &Job{
		JobID: jobID, ProjectID: projectID, Status: "queued", Stage: "queued",
		Progress: 0, Message: "等待分轨 Worker", RefinementRequested: options.Refine,
		CreatedAt: now, UpdatedAt: now, workDir: workDir, outputDir: outputDir,
		progressPath: filepath.Join(workDir, "progress.json"),
		manifestPath: filepath.Join(workDir, "manifest.json"),
	}
	s.jobs[jobID] = job
	s.active[projectID] = jobID
	s.mu.Unlock()

	go s.cleanupExpired()
	go s.run(jobID, audioPath, options.Refine && capability.RefinementReady)
	created, _ := s.Get(jobID)
	return created, nil
}

func (s *Service) cleanupExpired() {
	entries, err := os.ReadDir(s.workDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-s.ttl)
	s.mu.RLock()
	active := make(map[string]struct{}, len(s.active))
	for _, jobID := range s.active {
		active[jobID] = struct{}{}
	}
	s.mu.RUnlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := active[entry.Name()]; ok {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			if os.RemoveAll(filepath.Join(s.workDir, entry.Name())) == nil {
				s.mu.Lock()
				delete(s.jobs, entry.Name())
				s.mu.Unlock()
			}
		}
	}
}

func (s *Service) Get(jobID string) (*Job, bool) {
	s.refresh(jobID)
	s.mu.RLock()
	job := s.decorateJob(s.jobs[strings.TrimSpace(jobID)])
	s.mu.RUnlock()
	return job, job != nil
}

func (s *Service) Asset(jobID, trackID string) (string, *Track, error) {
	job, ok := s.Get(jobID)
	if !ok {
		return "", nil, errors.New("分轨任务不存在")
	}
	if job.Status != "ready" {
		return "", nil, errors.New("分轨任务尚未完成")
	}
	for _, track := range job.Tracks {
		if track.ID != trackID {
			continue
		}
		path, err := safeAssetPath(job.outputDir, track.RelativePath)
		if err != nil {
			return "", nil, err
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return "", nil, errors.New("分轨文件不存在")
		}
		clone := track
		return path, &clone, nil
	}
	return "", nil, errors.New("分轨不存在")
}

func (s *Service) run(jobID, audioPath string, refine bool) {
	s.workers <- struct{}{}
	defer func() { <-s.workers }()

	s.mu.Lock()
	job := s.jobs[jobID]
	if job == nil {
		s.mu.Unlock()
		return
	}
	job.Status, job.Stage, job.Progress = "running", "preparing", 1
	job.Message, job.UpdatedAt = "正在启动分轨 Worker", time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	logPath := filepath.Join(job.workDir, "process.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.finishFailed(jobID, err)
		return
	}
	defer logFile.Close()
	args := []string{
		s.script,
		"--audio", audioPath,
		"--output-dir", job.outputDir,
		"--manifest", job.manifestPath,
		"--work-dir", job.workDir,
		"--progress", job.progressPath,
		"--demucs-model", s.model,
		"--device", s.device,
		"--shifts", strconv.Itoa(s.shifts),
		"--overlap", strconv.FormatFloat(s.overlap, 'f', 2, 64),
		"--audio-separator-bin", s.separatorBinary,
		"--vocal-model", s.vocalModel,
		"--backing-preset", s.backingPreset,
		"--piano-model", s.pianoModel,
		"--guitar-model", s.guitarModel,
	}
	if refine {
		args = append(args, "--refine")
	}
	command := exec.CommandContext(ctx, s.python, args...)
	command.Stdout, command.Stderr = logFile, logFile
	err = command.Run()
	s.refresh(jobID)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errors.New("分轨超过 90 分钟，任务已终止")
		}
		s.finishFailed(jobID, err)
		return
	}
	manifest, err := s.readManifest(job)
	if err != nil {
		s.finishFailed(jobID, err)
		return
	}
	s.mu.Lock()
	if current := s.jobs[jobID]; current != nil {
		current.Status, current.Stage, current.Progress = "ready", "ready", 100
		current.Message = fmt.Sprintf("分轨完成，共生成 %d 条轨道", len(manifest.Tracks))
		current.RefinementRequested = manifest.RefinementRequested
		current.Warnings = append([]string(nil), manifest.Warnings...)
		current.Tracks = append([]Track(nil), manifest.Tracks...)
		current.UpdatedAt = time.Now()
		delete(s.active, current.ProjectID)
	}
	s.mu.Unlock()
}

func (s *Service) readManifest(job *Job) (*manifestFile, error) {
	data, err := os.ReadFile(job.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("Worker 未生成分轨清单: %w", err)
	}
	var manifest manifestFile
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析分轨清单: %w", err)
	}
	if manifest.Version != 1 || manifest.Status != "ready" || len(manifest.Tracks) < 6 {
		return nil, errors.New("分轨清单不完整")
	}
	seen := make(map[string]struct{}, len(manifest.Tracks))
	for index := range manifest.Tracks {
		track := &manifest.Tracks[index]
		if !safeTrackID.MatchString(track.ID) {
			return nil, fmt.Errorf("分轨 ID 非法: %s", track.ID)
		}
		if _, exists := seen[track.ID]; exists {
			return nil, fmt.Errorf("分轨 ID 重复: %s", track.ID)
		}
		seen[track.ID] = struct{}{}
		if _, err = safeAssetPath(job.outputDir, track.RelativePath); err != nil {
			return nil, err
		}
	}
	return &manifest, nil
}

func safeAssetPath(outputDir, relative string) (string, error) {
	relative = filepath.Clean(strings.TrimSpace(relative))
	if relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", errors.New("分轨文件路径非法")
	}
	base, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(base, relative))
	if err != nil {
		return "", err
	}
	if path != base && !strings.HasPrefix(path, base+string(filepath.Separator)) {
		return "", errors.New("分轨文件越过任务目录")
	}
	return path, nil
}

func (s *Service) refresh(jobID string) {
	s.mu.RLock()
	job := s.jobs[strings.TrimSpace(jobID)]
	if job == nil || job.progressPath == "" || job.Status == "ready" {
		s.mu.RUnlock()
		return
	}
	path := job.progressPath
	s.mu.RUnlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var progress progressFile
	if json.Unmarshal(data, &progress) != nil {
		return
	}
	s.mu.Lock()
	if current := s.jobs[jobID]; current != nil && current.Status != "ready" {
		current.Stage, current.Progress, current.Message = progress.Stage, progress.Progress, progress.Message
		if len(progress.Warnings) > 0 {
			current.Warnings = append([]string(nil), progress.Warnings...)
		}
		if progress.Stage == "failed" {
			current.Status, current.Error = "failed", firstNonEmpty(progress.Error, progress.Message)
			delete(s.active, current.ProjectID)
		} else if current.Status == "queued" {
			current.Status = "running"
		}
		current.UpdatedAt = time.Now()
	}
	s.mu.Unlock()
}

func (s *Service) finishFailed(jobID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.jobs[jobID]; job != nil {
		job.Status, job.Stage, job.Progress = "failed", "failed", 100
		if strings.TrimSpace(job.Error) == "" {
			job.Error = err.Error()
		}
		if strings.TrimSpace(job.Message) == "" || job.Message == "正在启动分轨 Worker" {
			job.Message = job.Error
		}
		job.UpdatedAt = time.Now()
		delete(s.active, job.ProjectID)
	}
}

func (s *Service) decorateJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	clone := *job
	clone.Warnings = append([]string(nil), job.Warnings...)
	clone.Tracks = append([]Track(nil), job.Tracks...)
	for index := range clone.Tracks {
		clone.Tracks[index].URL = "/api/v1/studio/projects/" + clone.ProjectID + "/stems/" + clone.JobID + "/assets/" + clone.Tracks[index].ID
	}
	return &clone
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "分轨失败"
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
