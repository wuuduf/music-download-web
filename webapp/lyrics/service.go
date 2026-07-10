package lyrics

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
	"github.com/liuran001/MusicBot-Go/bot/musicservice"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

const defaultBaseURL = "https://raw.githubusercontent.com/amll-dev/amll-ttml-db/refs/heads/main"

var dbFormats = map[string]bool{"ttml": true, "lrc": true, "yrc": true, "qrc": true, "lys": true, "eslrc": true}

type Config interface {
	GetString(string) string
	GetInt(string) int
}

type TrackIdentity struct {
	Platform    string              `json:"platform"`
	TrackID     string              `json:"track_id"`
	Title       string              `json:"title"`
	Artists     []string            `json:"artists"`
	Album       string              `json:"album,omitempty"`
	DurationMS  int64               `json:"duration_ms,omitempty"`
	ISRC        string              `json:"isrc,omitempty"`
	ExternalIDs map[string][]string `json:"external_ids,omitempty"`
}

type Asset struct {
	Source      string              `json:"source"`
	Format      string              `json:"format"`
	MatchType   string              `json:"match_type"`
	Confidence  int                 `json:"confidence"`
	Author      string              `json:"author,omitempty"`
	WordSynced  bool                `json:"word_synced"`
	Content     string              `json:"content"`
	SHA256      string              `json:"sha256,omitempty"`
	ExternalIDs map[string][]string `json:"external_ids,omitempty"`
	Metadata    map[string][]string `json:"metadata,omitempty"`
}

type ResolveOptions struct {
	Format             string
	IncludeTranslation bool
	IncludeRoma        bool
}

type indexRecord struct {
	RawFile  string
	Metadata map[string][]string
}

type indexLine struct {
	ID           string              `json:"id"`
	Metadata     [][]json.RawMessage `json:"metadata"`
	RawLyricFile string              `json:"rawLyricFile"`
}

type IndexStatus struct {
	Ready       bool      `json:"ready"`
	Syncing     bool      `json:"syncing"`
	Entries     int       `json:"entries"`
	ExternalIDs int       `json:"external_ids"`
	LastSync    time.Time `json:"last_sync,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	CacheHits   int64     `json:"cache_hits"`
	CacheMisses int64     `json:"cache_misses"`
	AMLLMatches int64     `json:"amlldb_matches"`
	Fallbacks   int64     `json:"platform_fallbacks"`
	Failures    int64     `json:"failures"`
}

type Service struct {
	platforms platform.Manager
	baseURL   string
	cacheDir  string
	client    *http.Client

	mu         sync.RWMutex
	byExternal map[string]*indexRecord
	byISRC     map[string]*indexRecord
	records    map[string]*indexRecord
	status     IndexStatus
}

func New(cfg Config, platforms platform.Manager) *Service {
	baseURL := defaultBaseURL
	cacheDir := "./cache/amll-db"
	timeout := 60 * time.Second
	if cfg != nil {
		if value := strings.TrimSpace(cfg.GetString("AMLLDBBaseURL")); value != "" {
			baseURL = strings.TrimRight(value, "/")
		}
		if value := strings.TrimSpace(cfg.GetString("AMLLDBCacheDir")); value != "" {
			cacheDir = value
		}
		if seconds := cfg.GetInt("AMLLDBTimeoutSeconds"); seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	s := &Service{
		platforms:  platforms,
		baseURL:    strings.TrimRight(baseURL, "/"),
		cacheDir:   cacheDir,
		client:     &http.Client{Timeout: timeout},
		byExternal: make(map[string]*indexRecord),
		byISRC:     make(map[string]*indexRecord),
		records:    make(map[string]*indexRecord),
	}
	_ = os.MkdirAll(filepath.Join(cacheDir, "indexes"), 0o755)
	_ = os.MkdirAll(filepath.Join(cacheDir, "lyrics"), 0o755)
	s.loadCachedIndexes()
	return s
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		status := s.Status()
		if !status.Ready || status.LastSync.IsZero() || time.Since(status.LastSync) >= 24*time.Hour {
			_ = s.Sync(ctx)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Sync(ctx)
			}
		}
	}()
}

func (s *Service) Status() IndexStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Service) Sync(ctx context.Context) error {
	s.mu.Lock()
	if s.status.Syncing {
		s.mu.Unlock()
		return errors.New("AMLL DB index sync already running")
	}
	s.status.Syncing = true
	s.status.LastError = ""
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.status.Syncing = false
		s.mu.Unlock()
	}()
	indexesDir := filepath.Join(s.cacheDir, "indexes")
	lyricsDir := filepath.Join(s.cacheDir, "lyrics")
	if err := os.MkdirAll(indexesDir, 0o755); err != nil {
		err = fmt.Errorf("create AMLL DB index cache %q: %w", indexesDir, err)
		s.setSyncError(err)
		return err
	}
	if err := os.MkdirAll(lyricsDir, 0o755); err != nil {
		err = fmt.Errorf("create AMLL DB lyrics cache %q: %w", lyricsDir, err)
		s.setSyncError(err)
		return err
	}

	for _, source := range []struct{ Platform, Folder string }{
		{"netease", "ncm-lyrics"}, {"qqmusic", "qq-lyrics"}, {"spotify", "spotify-lyrics"}, {"applemusic", "am-lyrics"},
	} {
		url := s.baseURL + "/" + source.Folder + "/index.jsonl"
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := s.client.Do(req)
		if err != nil {
			s.setSyncError(err)
			return err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			err = fmt.Errorf("AMLL DB index %s: HTTP %d", source.Platform, resp.StatusCode)
			s.setSyncError(err)
			return err
		}
		path := filepath.Join(s.cacheDir, "indexes", source.Folder+".jsonl")
		tmp := path + ".tmp"
		file, err := os.Create(tmp)
		if err == nil {
			_, err = io.Copy(file, io.LimitReader(resp.Body, 32<<20))
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		_ = resp.Body.Close()
		if err != nil {
			_ = os.Remove(tmp)
			s.setSyncError(err)
			return err
		}
		if err = os.Rename(tmp, path); err != nil {
			s.setSyncError(err)
			return err
		}
	}
	if err := s.loadCachedIndexes(); err != nil {
		s.setSyncError(err)
		return err
	}
	s.mu.Lock()
	s.status.LastSync = time.Now()
	s.status.LastError = ""
	s.mu.Unlock()
	return nil
}

func (s *Service) setSyncError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
	}
}

func (s *Service) loadCachedIndexes() error {
	records := make(map[string]*indexRecord)
	byExternal := make(map[string]*indexRecord)
	byISRC := make(map[string]*indexRecord)
	var oldestIndex time.Time
	for _, source := range []struct{ Platform, Folder string }{
		{"netease", "ncm-lyrics"}, {"qqmusic", "qq-lyrics"}, {"spotify", "spotify-lyrics"}, {"applemusic", "am-lyrics"},
	} {
		path := filepath.Join(s.cacheDir, "indexes", source.Folder+".jsonl")
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info, statErr := file.Stat(); statErr == nil && (oldestIndex.IsZero() || info.ModTime().Before(oldestIndex)) {
			oldestIndex = info.ModTime()
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2<<20)
		for scanner.Scan() {
			var line indexLine
			if json.Unmarshal(scanner.Bytes(), &line) != nil || line.ID == "" {
				continue
			}
			metadata := decodeMetadata(line.Metadata)
			key := line.RawLyricFile
			if key == "" {
				key = source.Platform + ":" + line.ID
			}
			record := records[key]
			if record == nil {
				record = &indexRecord{RawFile: line.RawLyricFile, Metadata: metadata}
				records[key] = record
			}
			byExternal[externalKey(source.Platform, line.ID)] = record
			for platformName, metaKey := range map[string]string{"netease": "ncmMusicId", "qqmusic": "qqMusicId", "spotify": "spotifyId", "applemusic": "appleMusicId"} {
				for _, id := range metadata[metaKey] {
					byExternal[externalKey(platformName, id)] = record
				}
			}
			for _, isrc := range metadata["isrc"] {
				byISRC[strings.ToUpper(strings.TrimSpace(isrc))] = record
			}
		}
		scanErr := scanner.Err()
		_ = file.Close()
		if scanErr != nil {
			return scanErr
		}
	}
	s.mu.Lock()
	s.records = records
	s.byExternal = byExternal
	s.byISRC = byISRC
	s.status.Ready = len(records) > 0
	s.status.Entries = len(records)
	s.status.ExternalIDs = len(byExternal)
	if !oldestIndex.IsZero() {
		s.status.LastSync = oldestIndex
	}
	s.mu.Unlock()
	return nil
}

func decodeMetadata(raw [][]json.RawMessage) map[string][]string {
	out := make(map[string][]string)
	for _, pair := range raw {
		if len(pair) != 2 {
			continue
		}
		var key string
		var values []string
		if json.Unmarshal(pair[0], &key) == nil && json.Unmarshal(pair[1], &values) == nil {
			out[key] = values
		}
	}
	return out
}

func externalKey(platformName, id string) string {
	return strings.ToLower(strings.TrimSpace(platformName)) + "\x00" + strings.TrimSpace(id)
}

func (s *Service) Resolve(ctx context.Context, platformName, trackID string, opts ResolveOptions) (*Asset, *TrackIdentity, error) {
	if s.platforms == nil {
		return nil, nil, errors.New("platform manager not configured")
	}
	plat := s.platforms.Get(strings.TrimSpace(platformName))
	if plat == nil {
		return nil, nil, fmt.Errorf("unknown platform: %s", platformName)
	}
	track, err := plat.GetTrack(ctx, strings.TrimSpace(trackID))
	if err != nil {
		return nil, nil, err
	}
	identity := identityFromTrack(platformName, track)
	asset, err := s.resolveAMLLDB(ctx, *identity, opts)
	if err == nil && asset != nil {
		s.bump(func(status *IndexStatus) { status.AMLLMatches++ })
		return asset, identity, nil
	}
	asset, err = s.resolvePlatform(ctx, plat, *identity, opts)
	if err == nil {
		s.bump(func(status *IndexStatus) { status.Fallbacks++ })
	} else {
		s.bump(func(status *IndexStatus) { status.Failures++ })
	}
	return asset, identity, err
}

func (s *Service) ResolveDocument(ctx context.Context, req musicservice.LyricsRequest) (*musicservice.LyricsDocument, error) {
	asset, identity, err := s.Resolve(ctx, req.Platform, req.TrackID, ResolveOptions{
		Format: req.Format, IncludeTranslation: req.IncludeTranslation, IncludeRoma: req.IncludeRoma,
	})
	if err != nil {
		return nil, err
	}
	name := req.TrackID
	if identity != nil && strings.TrimSpace(identity.Title) != "" {
		name = identity.Title
	}
	ext := lyricpkg.FileExtension(asset.Format)
	name = safeFileName(name) + "." + ext
	contentType := mime.TypeByExtension("." + ext)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	return &musicservice.LyricsDocument{FileName: name, ContentType: contentType, Content: []byte(asset.Content)}, nil
}

func identityFromTrack(platformName string, track *platform.Track) *TrackIdentity {
	identity := &TrackIdentity{Platform: platformName, ExternalIDs: make(map[string][]string)}
	if track == nil {
		return identity
	}
	identity.ExternalIDs[platformName] = []string{track.ID}
	identity.TrackID = track.ID
	identity.Title = track.Title
	identity.DurationMS = track.Duration.Milliseconds()
	identity.ISRC = track.ISRC
	identity.Artists = make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		if strings.TrimSpace(artist.Name) != "" {
			identity.Artists = append(identity.Artists, artist.Name)
		}
	}
	if track.Album != nil {
		identity.Album = track.Album.Title
	}
	return identity
}

func (s *Service) resolveAMLLDB(ctx context.Context, identity TrackIdentity, opts ResolveOptions) (*Asset, error) {
	format := lyricpkg.NormalizeFormat(opts.Format)
	fetchFormat := format
	if !dbFormats[fetchFormat] {
		fetchFormat = "ttml"
	}
	record, matchType, confidence := s.matchRecord(identity)
	platformName := identity.Platform
	id := identity.TrackID
	if record != nil {
		platformName, id = preferredExternalID(record.Metadata, identity.Platform, identity.TrackID)
	}
	folder := platformFolder(platformName)
	if folder == "" || id == "" {
		return nil, platform.ErrNotFound
	}
	content, err := s.fetchDBLyric(ctx, folder, id, fetchFormat)
	if err != nil {
		return nil, err
	}
	wordSynced := fetchFormat != "lrc" && (fetchFormat != "ttml" || strings.Contains(content, "<span"))
	metadata := map[string][]string{}
	if record != nil {
		metadata = cloneMetadata(record.Metadata)
	}
	if format != fetchFormat {
		converted := lyricpkg.Convert(lyricpkg.Payload{RawTTML: content, Lyric: content, MusicName: identity.Title, Artist: strings.Join(identity.Artists, ", "), Album: identity.Album, Source: identity.Platform}, format, lyricpkg.Options{IncludeTranslation: boolPtr(opts.IncludeTranslation), IncludeRoma: opts.IncludeRoma})
		if strings.TrimSpace(converted) == "" {
			return nil, platform.ErrUnavailable
		}
		content = converted
	}
	if matchType == "" {
		matchType, confidence = "exact_platform_id", 100
	}
	asset := &Asset{Source: "amlldb", Format: format, MatchType: matchType, Confidence: confidence, Content: content, Metadata: metadata, ExternalIDs: externalIDsFromMetadata(metadata)}
	asset.Author = first(metadata["ttmlAuthorGithubLogin"])
	asset.WordSynced = wordSynced
	asset.SHA256 = contentHash(content)
	return asset, nil
}

func (s *Service) matchRecord(identity TrackIdentity) (*indexRecord, string, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if record := s.byExternal[externalKey(identity.Platform, identity.TrackID)]; record != nil {
		return record, "exact_platform_id", 100
	}
	if identity.ISRC != "" {
		if record := s.byISRC[strings.ToUpper(strings.TrimSpace(identity.ISRC))]; record != nil {
			return record, "exact_isrc", 95
		}
	}
	return nil, "", 0
}

func preferredExternalID(metadata map[string][]string, currentPlatform, currentID string) (string, string) {
	keys := []struct{ Platform, Meta string }{{currentPlatform, platformMetaKey(currentPlatform)}, {"netease", "ncmMusicId"}, {"qqmusic", "qqMusicId"}, {"applemusic", "appleMusicId"}, {"spotify", "spotifyId"}}
	seen := make(map[string]bool)
	for _, item := range keys {
		if item.Meta == "" || seen[item.Platform] {
			continue
		}
		seen[item.Platform] = true
		values := metadata[item.Meta]
		if item.Platform == currentPlatform && currentID != "" {
			for _, value := range values {
				if value == currentID {
					return item.Platform, value
				}
			}
		}
		if len(values) > 0 {
			return item.Platform, values[0]
		}
	}
	return currentPlatform, currentID
}

func platformFolder(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "netease":
		return "ncm-lyrics"
	case "qqmusic":
		return "qq-lyrics"
	case "spotify":
		return "spotify-lyrics"
	case "applemusic":
		return "am-lyrics"
	default:
		return ""
	}
}

func platformMetaKey(name string) string {
	switch name {
	case "netease":
		return "ncmMusicId"
	case "qqmusic":
		return "qqMusicId"
	case "spotify":
		return "spotifyId"
	case "applemusic":
		return "appleMusicId"
	default:
		return ""
	}
}

func (s *Service) fetchDBLyric(ctx context.Context, folder, id, format string) (string, error) {
	safeID := strings.NewReplacer("/", "_", "\\", "_").Replace(id)
	cachePath := filepath.Join(s.cacheDir, "lyrics", folder, safeID+"."+format)
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		s.bump(func(status *IndexStatus) { status.CacheHits++ })
		return string(data), nil
	}
	s.bump(func(status *IndexStatus) { status.CacheMisses++ })
	url := fmt.Sprintf("%s/%s/%s.%s", s.baseURL, folder, id, format)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", platform.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AMLL DB lyric: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return "", errors.New("AMLL DB lyric exceeds size limit")
	}
	content := string(data)
	if format == "ttml" && !strings.Contains(content, "<tt") {
		return "", errors.New("AMLL DB returned invalid TTML")
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
	return content, nil
}

func (s *Service) resolvePlatform(ctx context.Context, plat platform.Platform, identity TrackIdentity, opts ResolveOptions) (*Asset, error) {
	if !plat.SupportsLyrics() {
		return nil, platform.ErrUnsupported
	}
	lyrics, err := plat.GetLyrics(ctx, identity.TrackID)
	if err != nil {
		return nil, err
	}
	format := lyricpkg.NormalizeFormat(opts.Format)
	payload := payloadFromPlatform(lyrics, identity)
	content := lyricpkg.Convert(payload, format, lyricpkg.Options{IncludeTranslation: boolPtr(opts.IncludeTranslation), IncludeRoma: opts.IncludeRoma})
	if strings.TrimSpace(content) == "" {
		return nil, platform.ErrUnavailable
	}
	return &Asset{Source: "platform", Format: format, MatchType: "native_platform", Confidence: 80, WordSynced: lyricpkg.HasWordTiming(payload), Content: content, SHA256: contentHash(content), ExternalIDs: identity.ExternalIDs}, nil
}

func payloadFromPlatform(lyrics *platform.Lyrics, identity TrackIdentity) lyricpkg.Payload {
	if lyrics == nil {
		return lyricpkg.Payload{}
	}
	plain := strings.TrimSpace(lyrics.Plain)
	if plain == "" && len(lyrics.Timestamped) > 0 {
		parts := make([]string, 0, len(lyrics.Timestamped))
		for _, line := range lyrics.Timestamped {
			ms := line.Time.Milliseconds()
			parts = append(parts, fmt.Sprintf("[%02d:%02d.%02d]%s", ms/60000, (ms/1000)%60, (ms/10)%100, line.Text))
		}
		plain = strings.Join(parts, "\n")
	}
	source := identity.Platform
	if source == "qqmusic" {
		source = "tencent"
	}
	return lyricpkg.Payload{Lyric: plain, Translation: lyrics.Translation, Roma: lyrics.Roma, RawYRC: lyrics.RawYRC, RawQRC: lyrics.RawQRC, RawLYS: lyrics.RawLYS, RawTTML: lyrics.RawTTML, MusicName: identity.Title, Artist: strings.Join(identity.Artists, ", "), Album: identity.Album, Source: source, NcmMusicID: first(identity.ExternalIDs["netease"]), QqMusicID: first(identity.ExternalIDs["qqmusic"])}
}

func (s *Service) SearchMetadata(query string, limit int) []TrackIdentity {
	query = normalize(query)
	if query == "" {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	s.mu.RLock()
	records := make([]*indexRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	s.mu.RUnlock()
	out := make([]TrackIdentity, 0, limit)
	for _, record := range records {
		haystack := normalize(strings.Join(append(append([]string{}, record.Metadata["musicName"]...), append(record.Metadata["artists"], record.Metadata["album"]...)...), " "))
		if !strings.Contains(haystack, query) {
			continue
		}
		identity := TrackIdentity{Title: first(record.Metadata["musicName"]), Artists: record.Metadata["artists"], Album: first(record.Metadata["album"]), ISRC: first(record.Metadata["isrc"]), ExternalIDs: externalIDsFromMetadata(record.Metadata)}
		out = append(out, identity)
		if len(out) >= limit {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func externalIDsFromMetadata(metadata map[string][]string) map[string][]string {
	out := make(map[string][]string)
	for platformName, key := range map[string]string{"netease": "ncmMusicId", "qqmusic": "qqMusicId", "spotify": "spotifyId", "applemusic": "appleMusicId"} {
		if len(metadata[key]) > 0 {
			out[platformName] = append([]string(nil), metadata[key]...)
		}
	}
	return out
}

func cloneMetadata(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
func first(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}
func boolPtr(value bool) *bool { return &value }
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
func (s *Service) bump(update func(*IndexStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(&s.status)
}
func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, value)
	if value == "" {
		return "lyrics"
	}
	return value
}
