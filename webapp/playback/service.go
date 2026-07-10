package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/musicservice"
)

type CreateRequest struct {
	Platform   string   `json:"platform"`
	TrackID    string   `json:"track_id"`
	Quality    string   `json:"quality"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	CoverURL   string   `json:"cover_url"`
	DurationMS int64    `json:"duration_ms"`
	ISRC       string   `json:"isrc"`
}

type Session struct {
	SessionID  string    `json:"session_id"`
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	Progress   int       `json:"progress"`
	Platform   string    `json:"platform"`
	TrackID    string    `json:"track_id"`
	Quality    string    `json:"quality"`
	Title      string    `json:"title"`
	Artists    []string  `json:"artists"`
	Album      string    `json:"album,omitempty"`
	CoverURL   string    `json:"cover_url,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	ISRC       string    `json:"isrc,omitempty"`
	AudioURL   string    `json:"audio_url"`
	LyricURL   string    `json:"lyric_url"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type Service struct {
	music *musicservice.Service
	ttl   time.Duration
	mu    sync.RWMutex
	items map[string]*Session
}

func New(music *musicservice.Service, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{music: music, ttl: ttl, items: make(map[string]*Session)}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Session, error) {
	if s == nil || s.music == nil {
		return nil, errors.New("playback service not configured")
	}
	if strings.TrimSpace(req.Platform) == "" || strings.TrimSpace(req.TrackID) == "" {
		return nil, errors.New("platform 和 track_id 必填")
	}
	job, err := s.music.CreateDownload(ctx, musicservice.DownloadRequest{Platform: req.Platform, TrackID: req.TrackID, Quality: req.Quality})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	session := &Session{SessionID: newID(), JobID: job.JobID, Platform: req.Platform, TrackID: req.TrackID, Quality: job.Quality, Title: req.Title, Artists: append([]string(nil), req.Artists...), Album: req.Album, CoverURL: req.CoverURL, DurationMS: req.DurationMS, ISRC: req.ISRC, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	s.decorate(session, job)
	s.mu.Lock()
	s.items[session.SessionID] = session
	s.mu.Unlock()
	clone := *session
	return &clone, nil
}

func (s *Service) Get(id string) (*Session, bool) {
	s.mu.RLock()
	session := s.items[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if session == nil || time.Now().After(session.ExpiresAt) {
		return nil, false
	}
	job, ok := s.music.GetJob(session.JobID)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	s.decorate(session, job)
	clone := *session
	clone.Artists = append([]string(nil), session.Artists...)
	s.mu.Unlock()
	return &clone, true
}

// Restore recreates an in-memory session after a process restart while reusing
// the persisted download/cache job.
func (s *Service) Restore(ctx context.Context, sessionID string, req CreateRequest) (*Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return s.Create(ctx, req)
	}
	job, err := s.music.CreateDownload(ctx, musicservice.DownloadRequest{Platform: req.Platform, TrackID: req.TrackID, Quality: req.Quality})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	session := &Session{SessionID: sessionID, JobID: job.JobID, Platform: req.Platform, TrackID: req.TrackID, Quality: job.Quality, Title: req.Title, Artists: append([]string(nil), req.Artists...), Album: req.Album, CoverURL: req.CoverURL, DurationMS: req.DurationMS, ISRC: req.ISRC, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	s.decorate(session, job)
	s.mu.Lock()
	s.items[sessionID] = session
	s.mu.Unlock()
	clone := *session
	return &clone, nil
}

func (s *Service) Job(id string) (*musicservice.DownloadJob, *Session, bool) {
	session, ok := s.Get(id)
	if !ok {
		return nil, nil, false
	}
	job, ok := s.music.GetJob(session.JobID)
	return job, session, ok
}

func (s *Service) decorate(session *Session, job *musicservice.DownloadJob) {
	if session == nil || job == nil {
		return
	}
	session.Status, session.Progress, session.Error = job.Status, job.Progress, job.Error
	if session.Title == "" {
		session.Title = job.Title
	}
	if len(session.Artists) == 0 {
		session.Artists = append([]string(nil), job.Artists...)
	}
	if session.Album == "" {
		session.Album = job.Album
	}
	session.AudioURL = "/api/v1/playback/sessions/" + session.SessionID + "/audio"
	session.LyricURL = "/api/v1/playback/sessions/" + session.SessionID + "/lyrics?format=ttml"
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
