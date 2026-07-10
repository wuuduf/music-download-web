package studio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	lyricservice "github.com/liuran001/MusicBot-Go/webapp/lyrics"
	"github.com/liuran001/MusicBot-Go/webapp/playback"
)

type Service struct {
	repo      *db.Repository
	platforms platform.Manager
	playback  *playback.Service
	lyrics    *lyricservice.Service
}

type CreateRequest struct {
	Platform string `json:"platform"`
	TrackID  string `json:"track_id"`
	Quality  string `json:"quality"`
}

type Metadata struct {
	MusicName           string                   `json:"music_name"`
	MusicNames          []string                 `json:"music_names,omitempty"`
	Artists             []string                 `json:"artists"`
	Album               string                   `json:"album,omitempty"`
	Albums              []string                 `json:"albums,omitempty"`
	DurationMS          int64                    `json:"duration_ms,omitempty"`
	ISRC                string                   `json:"isrc,omitempty"`
	ISRCs               []string                 `json:"isrcs,omitempty"`
	CoverURL            string                   `json:"cover_url,omitempty"`
	ExternalIDs         map[string][]string      `json:"external_ids,omitempty"`
	Matches             map[string]MetadataMatch `json:"matches,omitempty"`
	UnresolvedPlatforms []string                 `json:"unresolved_platforms,omitempty"`
	ResolvedAt          time.Time                `json:"resolved_at,omitempty"`
}

type Project struct {
	ProjectID       string   `json:"project_id"`
	Platform        string   `json:"platform"`
	TrackID         string   `json:"track_id"`
	Quality         string   `json:"quality"`
	PlaybackSession string   `json:"playback_session"`
	Metadata        Metadata `json:"metadata"`
	CurrentRevision int      `json:"current_revision"`
	Status          string   `json:"status"`
}

type Bootstrap struct {
	Project      Project `json:"project"`
	AudioURL     string  `json:"audio_url"`
	SeedLyricURL string  `json:"seed_lyric_url"`
	Revision     int     `json:"revision"`
}

type SaveRequest struct {
	ExpectedRevision int             `json:"expected_revision"`
	Content          string          `json:"content"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

type MetadataCandidate struct {
	lyricservice.TrackIdentity
	Score                int      `json:"score"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Reasons              []string `json:"reasons"`
}

func New(repo *db.Repository, platforms platform.Manager, playbackService *playback.Service, lyricService *lyricservice.Service) *Service {
	return &Service{repo: repo, platforms: platforms, playback: playbackService, lyrics: lyricService}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Project, error) {
	if s == nil || s.repo == nil || s.platforms == nil || s.playback == nil || s.lyrics == nil {
		return nil, errors.New("studio service not configured")
	}
	plat := s.platforms.Get(strings.TrimSpace(req.Platform))
	if plat == nil {
		return nil, fmt.Errorf("unknown platform: %s", req.Platform)
	}
	track, err := plat.GetTrack(ctx, strings.TrimSpace(req.TrackID))
	if err != nil || track == nil {
		if err == nil {
			err = platform.ErrNotFound
		}
		return nil, err
	}
	artists := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artists = append(artists, artist.Name)
	}
	album := ""
	if track.Album != nil {
		album = track.Album.Title
	}
	session, err := s.playback.Create(ctx, playback.CreateRequest{Platform: req.Platform, TrackID: track.ID, Quality: req.Quality, Title: track.Title, Artists: artists, Album: album, CoverURL: track.CoverURL, DurationMS: track.Duration.Milliseconds(), ISRC: track.ISRC})
	if err != nil {
		return nil, err
	}
	asset, identity, lyricErr := s.lyrics.Resolve(ctx, req.Platform, track.ID, lyricservice.ResolveOptions{Format: "ttml", IncludeTranslation: true, IncludeRoma: true})
	content := emptyTTML(track.Title, artists, album)
	if lyricErr == nil && asset != nil && strings.TrimSpace(asset.Content) != "" {
		content = asset.Content
	}
	metadata := Metadata{MusicName: track.Title, MusicNames: []string{track.Title}, Artists: artists, Album: album, Albums: []string{album}, DurationMS: track.Duration.Milliseconds(), ISRC: track.ISRC, CoverURL: track.CoverURL, ExternalIDs: map[string][]string{req.Platform: {track.ID}}}
	mergeLyricAssetMetadata(&metadata, identity, asset)
	metadata = s.enrichMetadata(ctx, track, metadata)
	metadataJSON, _ := json.Marshal(metadata)
	project := Project{ProjectID: newID(), Platform: req.Platform, TrackID: track.ID, Quality: session.Quality, PlaybackSession: session.SessionID, Metadata: metadata, CurrentRevision: 1, Status: "active"}
	err = s.repo.CreateStudioProject(ctx, &db.StudioProjectModel{ProjectID: project.ProjectID, Platform: project.Platform, TrackID: project.TrackID, Quality: project.Quality, PlaybackSession: project.PlaybackSession, MetadataJSON: string(metadataJSON), CurrentRevision: 1, Status: "active"}, &db.StudioRevisionModel{Content: content, MetadataJSON: string(metadataJSON)})
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Service) Get(ctx context.Context, projectID string) (*Project, error) {
	model, err := s.repo.GetStudioProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var metadata Metadata
	_ = json.Unmarshal([]byte(model.MetadataJSON), &metadata)
	return &Project{ProjectID: model.ProjectID, Platform: model.Platform, TrackID: model.TrackID, Quality: model.Quality, PlaybackSession: model.PlaybackSession, Metadata: metadata, CurrentRevision: model.CurrentRevision, Status: model.Status}, nil
}

func (s *Service) Bootstrap(ctx context.Context, projectID string) (*Bootstrap, error) {
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if metadataNeedsRefresh(project.Metadata) {
		if refreshed, refreshErr := s.RefreshMetadata(ctx, projectID); refreshErr == nil {
			project = refreshed
		}
	}
	if _, ok := s.playback.Get(project.PlaybackSession); !ok {
		session, restoreErr := s.playback.Restore(ctx, project.PlaybackSession, playback.CreateRequest{Platform: project.Platform, TrackID: project.TrackID, Quality: project.Quality, Title: project.Metadata.MusicName, Artists: project.Metadata.Artists, Album: project.Metadata.Album, CoverURL: project.Metadata.CoverURL, DurationMS: project.Metadata.DurationMS, ISRC: project.Metadata.ISRC})
		if restoreErr != nil {
			return nil, restoreErr
		}
		project.PlaybackSession = session.SessionID
	}
	return &Bootstrap{Project: *project, AudioURL: "/api/v1/playback/sessions/" + project.PlaybackSession + "/audio", SeedLyricURL: "/api/v1/studio/projects/" + project.ProjectID + "/export", Revision: project.CurrentRevision}, nil
}

// RefreshMetadata re-runs the cross-platform resolver for an existing Studio
// project. It is used automatically for projects created before this resolver
// existed and can also be called explicitly after account credentials change.
func (s *Service) RefreshMetadata(ctx context.Context, projectID string) (*Project, error) {
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	plat := s.platforms.Get(project.Platform)
	if plat == nil {
		return nil, fmt.Errorf("unknown platform: %s", project.Platform)
	}
	track, err := plat.GetTrack(ctx, project.TrackID)
	if err != nil || track == nil {
		if err == nil {
			err = platform.ErrNotFound
		}
		return nil, err
	}
	project.Metadata = s.enrichMetadata(ctx, track, project.Metadata)
	metadataJSON, err := json.Marshal(project.Metadata)
	if err != nil {
		return nil, err
	}
	if err = s.repo.UpdateStudioProjectMetadata(ctx, projectID, string(metadataJSON)); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *Service) Save(ctx context.Context, projectID string, req SaveRequest) (int, error) {
	if len(req.Content) == 0 || len(req.Content) > 4<<20 {
		return 0, errors.New("TTML 内容为空或超过 4 MiB")
	}
	if !strings.Contains(req.Content, "<tt") {
		return 0, errors.New("不是有效的 TTML 文档")
	}
	return s.repo.SaveStudioRevision(ctx, projectID, req.ExpectedRevision, &db.StudioRevisionModel{Content: req.Content, MetadataJSON: string(req.Metadata)})
}

func (s *Service) Revision(ctx context.Context, projectID string, revision int) (*db.StudioRevisionModel, error) {
	return s.repo.GetStudioRevision(ctx, projectID, revision)
}

func (s *Service) Revisions(ctx context.Context, projectID string) ([]db.StudioRevisionModel, error) {
	return s.repo.ListStudioRevisions(ctx, projectID)
}

func (s *Service) Restore(ctx context.Context, projectID string, expectedRevision, sourceRevision int) (int, error) {
	return s.repo.RestoreStudioRevision(ctx, projectID, expectedRevision, sourceRevision)
}

func (s *Service) SearchMetadata(query string, limit int) []lyricservice.TrackIdentity {
	return s.lyrics.SearchMetadata(query, limit)
}

// MatchMetadata scores AMLL DB candidates. Anything below an exact ID/ISRC
// match remains explicitly confirm-only; the editor must not silently apply a
// fuzzy title match.
func (s *Service) MatchMetadata(ctx context.Context, platformName, trackID, query string, limit int) ([]MetadataCandidate, error) {
	plat := s.platforms.Get(platformName)
	if plat == nil {
		return nil, fmt.Errorf("unknown platform: %s", platformName)
	}
	track, err := plat.GetTrack(ctx, trackID)
	if err != nil || track == nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		query = track.Title + " " + firstArtist(track.Artists)
	}
	candidates := s.lyrics.SearchMetadata(query, limit)
	out := make([]MetadataCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score, reasons := 0, make([]string, 0, 4)
		for _, id := range candidate.ExternalIDs[platformName] {
			if id == track.ID {
				score, reasons = 100, append(reasons, "exact_platform_id")
				break
			}
		}
		if score < 100 && track.ISRC != "" && strings.EqualFold(track.ISRC, candidate.ISRC) {
			score, reasons = 95, append(reasons, "exact_isrc")
		}
		if strings.EqualFold(strings.TrimSpace(track.Title), strings.TrimSpace(candidate.Title)) {
			score += 35
			reasons = append(reasons, "title")
		}
		if equalArtist(firstArtist(track.Artists), firstString(candidate.Artists)) {
			score += 25
			reasons = append(reasons, "artist")
		}
		if track.Album != nil && strings.EqualFold(strings.TrimSpace(track.Album.Title), strings.TrimSpace(candidate.Album)) {
			score += 15
			reasons = append(reasons, "album")
		}
		if track.Duration > 0 && candidate.DurationMS > 0 && abs(track.Duration.Milliseconds()-candidate.DurationMS) <= 2000 {
			score += 15
			reasons = append(reasons, "duration_within_2s")
		}
		if score > 100 {
			score = 100
		}
		out = append(out, MetadataCandidate{TrackIdentity: candidate, Score: score, RequiresConfirmation: score < 95, Reasons: reasons})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func emptyTTML(title string, artists []string, album string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><tt xmlns="http://www.w3.org/ns/ttml" xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><head><metadata><ttm:title>` + html.EscapeString(title) + `</ttm:title><ttm:agent type="person">` + html.EscapeString(strings.Join(artists, ", ")) + `</ttm:agent><ttm:desc>` + html.EscapeString(album) + `</ttm:desc></metadata></head><body><div><p begin="00:00.000" end="00:05.000">在这里开始制作逐字歌词</p></div></body></tt>`
}

func newID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
func firstArtist(values []platform.Artist) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Name
}
func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func equalArtist(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) && strings.TrimSpace(a) != ""
}
func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
