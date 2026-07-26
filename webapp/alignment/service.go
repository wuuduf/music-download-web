package alignment

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
	Enabled       bool   `json:"enabled"`
	Ready         bool   `json:"ready"`
	Reason        string `json:"reason,omitempty"`
	Engine        string `json:"engine"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type Job struct {
	JobID               string    `json:"job_id"`
	ProjectID           string    `json:"project_id"`
	Status              string    `json:"status"`
	Stage               string    `json:"stage"`
	Progress            int       `json:"progress"`
	Message             string    `json:"message,omitempty"`
	Error               string    `json:"error,omitempty"`
	Tokens              int       `json:"tokens,omitempty"`
	LowConfidenceTokens int       `json:"low_confidence_tokens,omitempty"`
	ResultURL           string    `json:"result_url,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	workDir      string
	progressPath string
	outputPath   string
}

type progressFile struct {
	Stage               string `json:"stage"`
	Progress            int    `json:"progress"`
	Message             string `json:"message"`
	Error               string `json:"error"`
	Tokens              int    `json:"tokens"`
	LowConfidenceTokens int    `json:"low_confidence_tokens"`
}

type Service struct {
	enabled bool
	python  string
	script  string
	workDir string
	max     int

	mu      sync.RWMutex
	jobs    map[string]*Job
	active  map[string]string
	workers chan struct{}
}

func New(cfg Config) *Service {
	service := &Service{
		python:  "/opt/musicweb/alignment-venv/bin/python",
		script:  "./alignment-worker/worker.py",
		workDir: "./cache/alignment",
		max:     1,
		jobs:    make(map[string]*Job),
		active:  make(map[string]string),
	}
	if cfg != nil {
		service.enabled = cfg.GetBool("WebAlignmentEnabled")
		if value := strings.TrimSpace(cfg.GetString("WebAlignmentPython")); value != "" {
			service.python = value
		}
		if value := strings.TrimSpace(cfg.GetString("WebAlignmentScript")); value != "" {
			service.script = value
		}
		if value := strings.TrimSpace(cfg.GetString("WebAlignmentWorkDir")); value != "" {
			service.workDir = value
		}
		if value := cfg.GetInt("WebAlignmentMaxConcurrent"); value > 0 && value <= 4 {
			service.max = value
		}
	}
	service.workers = make(chan struct{}, service.max)
	return service
}

func (s *Service) Capabilities() Capabilities {
	capability := Capabilities{Enabled: s != nil && s.enabled, Engine: "demucs-htdemucs_ft + qwen3-forced-aligner-0.6b"}
	if s == nil {
		capability.Reason = "自动打轴服务未配置"
		return capability
	}
	capability.MaxConcurrent = s.max
	if !s.enabled {
		capability.Reason = "管理员尚未启用自动打轴服务"
		return capability
	}
	if _, err := os.Stat(s.python); err != nil {
		if _, lookErr := exec.LookPath(s.python); lookErr != nil {
			capability.Reason = "找不到自动打轴 Python 环境：" + s.python
			return capability
		}
	}
	if info, err := os.Stat(s.script); err != nil || info.IsDir() {
		capability.Reason = "找不到自动打轴 Worker：" + s.script
		return capability
	}
	capability.Ready = true
	return capability
}

func (s *Service) Start(projectID, audioPath, seedTTML string) (*Job, error) {
	capability := s.Capabilities()
	if !capability.Ready {
		return nil, errors.New(capability.Reason)
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(audioPath) == "" {
		return nil, errors.New("project_id 和音频文件必填")
	}
	if len(seedTTML) == 0 || len(seedTTML) > 4<<20 || !strings.Contains(seedTTML, "<tt") {
		return nil, errors.New("TTML 内容为空、无效或超过 4 MiB")
	}
	if info, err := os.Stat(audioPath); err != nil || info.IsDir() {
		return nil, errors.New("项目音频尚未准备好")
	}

	s.mu.Lock()
	if existingID := s.active[projectID]; existingID != "" {
		if existing := s.jobs[existingID]; existing != nil {
			clone := cloneJob(existing)
			s.mu.Unlock()
			return clone, nil
		}
	}
	jobID := newID()
	workDir := filepath.Join(s.workDir, jobID)
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("创建自动打轴目录: %w", err)
	}
	seedPath := filepath.Join(workDir, "seed.ttml")
	if err := os.WriteFile(seedPath, []byte(seedTTML), 0o600); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("写入自动打轴歌词: %w", err)
	}
	now := time.Now()
	job := &Job{
		JobID: jobID, ProjectID: projectID, Status: "queued", Stage: "queued",
		Progress: 0, Message: "等待自动打轴 Worker", CreatedAt: now, UpdatedAt: now,
		workDir: workDir, progressPath: filepath.Join(workDir, "progress.json"),
		outputPath: filepath.Join(workDir, "auto-draft.ttml"),
	}
	s.jobs[jobID] = job
	s.active[projectID] = jobID
	s.mu.Unlock()

	go s.run(jobID, audioPath, seedPath)
	return cloneJob(job), nil
}

func (s *Service) Get(jobID string) (*Job, bool) {
	s.refresh(jobID)
	s.mu.RLock()
	job := s.jobs[strings.TrimSpace(jobID)]
	clone := cloneJob(job)
	s.mu.RUnlock()
	return clone, clone != nil
}

func (s *Service) Result(jobID string) (string, error) {
	job, ok := s.Get(jobID)
	if !ok {
		return "", errors.New("自动打轴任务不存在")
	}
	if job.Status != "ready" {
		return "", errors.New("自动打轴任务尚未完成")
	}
	data, err := os.ReadFile(job.outputPath)
	if err != nil {
		return "", fmt.Errorf("读取自动打轴结果: %w", err)
	}
	return string(data), nil
}

func (s *Service) run(jobID, audioPath, seedPath string) {
	s.workers <- struct{}{}
	defer func() { <-s.workers }()

	s.mu.Lock()
	job := s.jobs[jobID]
	if job == nil {
		s.mu.Unlock()
		return
	}
	job.Status, job.Stage, job.Progress = "running", "preparing", 1
	job.Message, job.UpdatedAt = "正在启动自动打轴 Worker", time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	logPath := filepath.Join(job.workDir, "process.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.finishFailed(jobID, err)
		return
	}
	defer logFile.Close()
	command := exec.CommandContext(ctx, s.python, s.script,
		"--audio", audioPath,
		"--seed-ttml", seedPath,
		"--output", job.outputPath,
		"--work-dir", job.workDir,
		"--progress", job.progressPath,
	)
	command.Stdout, command.Stderr = logFile, logFile
	err = command.Run()
	s.refresh(jobID)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errors.New("自动打轴超过 45 分钟，任务已终止")
		}
		s.finishFailed(jobID, err)
		return
	}
	if _, err = os.Stat(job.outputPath); err != nil {
		s.finishFailed(jobID, errors.New("Worker 未生成 TTML 结果"))
		return
	}
	s.mu.Lock()
	if current := s.jobs[jobID]; current != nil {
		current.Status, current.Stage, current.Progress = "ready", "ready", 100
		current.Message = "自动打轴完成"
		current.ResultURL = "/api/v1/studio/projects/" + current.ProjectID + "/alignments/" + current.JobID + "/result"
		current.UpdatedAt = time.Now()
		delete(s.active, current.ProjectID)
	}
	s.mu.Unlock()
}

func (s *Service) refresh(jobID string) {
	s.mu.RLock()
	job := s.jobs[strings.TrimSpace(jobID)]
	if job == nil || job.progressPath == "" {
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
		current.Tokens, current.LowConfidenceTokens = progress.Tokens, progress.LowConfidenceTokens
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
		if strings.TrimSpace(job.Message) == "" || job.Message == "正在启动自动打轴 Worker" {
			job.Message = job.Error
		}
		job.UpdatedAt = time.Now()
		delete(s.active, job.ProjectID)
	}
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	clone := *job
	return &clone
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "自动打轴失败"
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
