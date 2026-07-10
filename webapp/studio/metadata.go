package studio

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	lyricservice "github.com/liuran001/MusicBot-Go/webapp/lyrics"
)

var metadataPlatforms = []string{"netease", "qqmusic", "spotify", "applemusic"}

const (
	metadataSearchLimit = 10
	metadataTimeout     = 15 * time.Second
	metadataRetryAfter  = 24 * time.Hour
)

// MetadataMatch records why a platform ID was (or was not) accepted. Fuzzy
// candidates are kept for observability, but only high-confidence matches are
// copied into ExternalIDs automatically.
type MetadataMatch struct {
	TrackID              string   `json:"track_id,omitempty"`
	Score                int      `json:"score"`
	MatchType            string   `json:"match_type,omitempty"`
	Reasons              []string `json:"reasons,omitempty"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Error                string   `json:"error,omitempty"`
}

type scoredMetadataTrack struct {
	track   platform.Track
	score   int
	reasons []string
	kind    string
}

func mergeLyricAssetMetadata(metadata *Metadata, identity *lyricservice.TrackIdentity, asset *lyricservice.Asset) {
	if metadata == nil {
		return
	}
	if identity != nil {
		addUnique(&metadata.MusicNames, identity.Title)
		addUnique(&metadata.Artists, identity.Artists...)
		addUnique(&metadata.Albums, identity.Album)
		addISRC(metadata, identity.ISRC)
		mergeExternalIDs(metadata, identity.ExternalIDs)
	}
	if asset != nil {
		mergeExternalIDs(metadata, asset.ExternalIDs)
		addUnique(&metadata.MusicNames, asset.Metadata["musicName"]...)
		addUnique(&metadata.Artists, asset.Metadata["artists"]...)
		addUnique(&metadata.Albums, asset.Metadata["album"]...)
		for _, value := range asset.Metadata["isrc"] {
			addISRC(metadata, value)
		}
	}
	normalizeMetadata(metadata)
}

func (s *Service) enrichMetadata(ctx context.Context, source *platform.Track, metadata Metadata) Metadata {
	normalizeMetadata(&metadata)
	if source != nil {
		mergeTrackMetadata(&metadata, *source)
		if metadata.ExternalIDs == nil {
			metadata.ExternalIDs = make(map[string][]string)
		}
		addExternalID(&metadata, source.Platform, source.ID)
	}
	if metadata.Matches == nil {
		metadata.Matches = make(map[string]MetadataMatch)
	}
	for platformName, ids := range metadata.ExternalIDs {
		if len(ids) > 0 {
			metadata.Matches[platformName] = MetadataMatch{TrackID: ids[0], Score: 100, MatchType: "existing_id", Reasons: []string{"existing_id"}}
		}
	}

	resolveCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	// Apple Music and Spotify are the preferred ISRC authorities. Hydrate IDs
	// already supplied by AMLL DB before searching missing platforms.
	type hydratedTrack struct {
		platformName string
		track        *platform.Track
	}
	hydrated := make(chan hydratedTrack, 2)
	var hydrateWG sync.WaitGroup
	for _, platformName := range []string{"applemusic", "spotify"} {
		ids := metadata.ExternalIDs[platformName]
		plat := s.platforms.Get(platformName)
		if len(ids) == 0 || plat == nil {
			continue
		}
		hydrateWG.Add(1)
		go func(name, id string, p platform.Platform) {
			defer hydrateWG.Done()
			track, _ := p.GetTrack(resolveCtx, id)
			if track != nil {
				hydrated <- hydratedTrack{platformName: name, track: track}
			}
		}(platformName, ids[0], plat)
	}
	go func() {
		hydrateWG.Wait()
		close(hydrated)
	}()
	for item := range hydrated {
		mergeTrackMetadata(&metadata, *item.track)
		match := metadata.Matches[item.platformName]
		if item.track.ISRC != "" {
			match.Reasons = appendUnique(match.Reasons, "authoritative_isrc")
			metadata.Matches[item.platformName] = match
		}
	}

	queries := metadataQueries(metadata)
	type searchResult struct {
		platformName string
		tracks       []platform.Track
		err          error
	}
	results := make(chan searchResult, len(metadataPlatforms))
	var searchWG sync.WaitGroup
	for _, platformName := range metadataPlatforms {
		if len(metadata.ExternalIDs[platformName]) > 0 {
			continue
		}
		plat := s.platforms.Get(platformName)
		if plat == nil || !plat.SupportsSearch() {
			metadata.Matches[platformName] = MetadataMatch{RequiresConfirmation: true, Error: "platform search unavailable"}
			continue
		}
		searchWG.Add(1)
		go func(name string, p platform.Platform) {
			defer searchWG.Done()
			tracks, err := searchMetadataCandidates(resolveCtx, p, queries)
			results <- searchResult{platformName: name, tracks: tracks, err: err}
		}(platformName, plat)
	}
	go func() {
		searchWG.Wait()
		close(results)
	}()

	candidates := make(map[string][]platform.Track)
	for result := range results {
		if result.err != nil {
			metadata.Matches[result.platformName] = MetadataMatch{RequiresConfirmation: true, Error: result.err.Error()}
			continue
		}
		candidates[result.platformName] = result.tracks
	}

	// Resolve authoritative catalogs first so their ISRC can make the second
	// pass deterministic for the other catalog.
	for _, platformName := range []string{"applemusic", "spotify"} {
		acceptMetadataCandidate(&metadata, platformName, candidates[platformName])
	}
	for _, platformName := range metadataPlatforms {
		if platformName == "applemusic" || platformName == "spotify" {
			continue
		}
		acceptMetadataCandidate(&metadata, platformName, candidates[platformName])
	}

	metadata.UnresolvedPlatforms = metadata.UnresolvedPlatforms[:0]
	for _, platformName := range metadataPlatforms {
		if len(metadata.ExternalIDs[platformName]) == 0 {
			metadata.UnresolvedPlatforms = append(metadata.UnresolvedPlatforms, platformName)
		}
	}
	metadata.ResolvedAt = time.Now().UTC()
	normalizeMetadata(&metadata)
	return metadata
}

func searchMetadataCandidates(ctx context.Context, plat platform.Platform, queries []string) ([]platform.Track, error) {
	seen := make(map[string]bool)
	result := make([]platform.Track, 0, metadataSearchLimit)
	var lastErr error
	for _, query := range queries {
		tracks, err := plat.Search(ctx, query, metadataSearchLimit)
		if err != nil {
			lastErr = err
			continue
		}
		for _, track := range tracks {
			if strings.TrimSpace(track.ID) == "" || seen[track.ID] {
				continue
			}
			seen[track.ID] = true
			result = append(result, track)
		}
		if len(result) >= metadataSearchLimit {
			break
		}
	}
	if len(result) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return result, nil
}

func acceptMetadataCandidate(metadata *Metadata, platformName string, tracks []platform.Track) {
	if metadata == nil || len(metadata.ExternalIDs[platformName]) > 0 || len(tracks) == 0 {
		return
	}
	scored := make([]scoredMetadataTrack, 0, len(tracks))
	for _, track := range tracks {
		score, reasons, kind := scoreMetadataTrack(*metadata, track)
		scored = append(scored, scoredMetadataTrack{track: track, score: score, reasons: reasons, kind: kind})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	best := scored[0]
	secondScore := -1
	if len(scored) > 1 {
		secondScore = scored[1].score
	}
	accepted := best.score == 100 || (best.score >= 85 && (best.score >= 95 || best.score-secondScore >= 5))
	metadata.Matches[platformName] = MetadataMatch{
		TrackID:              best.track.ID,
		Score:                best.score,
		MatchType:            best.kind,
		Reasons:              best.reasons,
		RequiresConfirmation: !accepted,
	}
	if !accepted {
		return
	}
	addExternalID(metadata, platformName, best.track.ID)
	mergeTrackMetadata(metadata, best.track)
}

func scoreMetadataTrack(metadata Metadata, track platform.Track) (int, []string, string) {
	if isKnownISRC(metadata, track.ISRC) {
		return 100, []string{"exact_isrc"}, "exact_isrc"
	}
	score := 0
	reasons := make([]string, 0, 4)
	if anyNormalizedEqual(append([]string{metadata.MusicName}, metadata.MusicNames...), track.Title) {
		score += 40
		reasons = append(reasons, "title")
	}
	candidateArtists := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		candidateArtists = append(candidateArtists, artist.Name)
	}
	if anyPairNormalizedEqual(metadata.Artists, candidateArtists) {
		score += 25
		reasons = append(reasons, "artist")
	}
	if metadata.DurationMS > 0 && track.Duration > 0 {
		delta := abs(metadata.DurationMS - track.Duration.Milliseconds())
		switch {
		case delta <= 1500:
			score += 20
			reasons = append(reasons, "duration_within_1_5s")
		case delta <= 3000:
			score += 15
			reasons = append(reasons, "duration_within_3s")
		case delta <= 5000:
			score += 8
			reasons = append(reasons, "duration_within_5s")
		}
	}
	if track.Album != nil && anyNormalizedEqual(append([]string{metadata.Album}, metadata.Albums...), track.Album.Title) {
		score += 10
		reasons = append(reasons, "album")
	}
	if score > 100 {
		score = 100
	}
	return score, reasons, "metadata"
}

func metadataQueries(metadata Metadata) []string {
	titles := append([]string{metadata.MusicName}, metadata.MusicNames...)
	queries := make([]string, 0, 3)
	artist := firstString(metadata.Artists)
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		query := strings.TrimSpace(title + " " + artist)
		queries = appendUnique(queries, query)
		if len(queries) >= 3 {
			break
		}
	}
	if len(queries) == 0 && metadata.MusicName != "" {
		queries = append(queries, metadata.MusicName)
	}
	return queries
}

func mergeTrackMetadata(metadata *Metadata, track platform.Track) {
	if metadata == nil {
		return
	}
	addUnique(&metadata.MusicNames, track.Title)
	for _, artist := range track.Artists {
		addUnique(&metadata.Artists, artist.Name)
	}
	if track.Album != nil {
		addUnique(&metadata.Albums, track.Album.Title)
	}
	addISRC(metadata, track.ISRC)
	if metadata.DurationMS == 0 {
		metadata.DurationMS = track.Duration.Milliseconds()
	}
	if metadata.CoverURL == "" {
		metadata.CoverURL = track.CoverURL
	}
	normalizeMetadata(metadata)
}

func normalizeMetadata(metadata *Metadata) {
	if metadata == nil {
		return
	}
	addUnique(&metadata.MusicNames, metadata.MusicName)
	addUnique(&metadata.Albums, metadata.Album)
	addISRC(metadata, metadata.ISRC)
	if metadata.MusicName == "" {
		metadata.MusicName = firstString(metadata.MusicNames)
	}
	if metadata.Album == "" {
		metadata.Album = firstString(metadata.Albums)
	}
	if metadata.ISRC == "" {
		metadata.ISRC = firstString(metadata.ISRCs)
	}
	if metadata.ExternalIDs == nil {
		metadata.ExternalIDs = make(map[string][]string)
	}
}

func mergeExternalIDs(metadata *Metadata, values map[string][]string) {
	if metadata == nil || len(values) == 0 {
		return
	}
	if metadata.ExternalIDs == nil {
		metadata.ExternalIDs = make(map[string][]string)
	}
	for platformName, ids := range values {
		for _, id := range ids {
			addExternalID(metadata, platformName, id)
		}
	}
}

func addISRC(metadata *Metadata, value string) {
	value = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if value == "" {
		return
	}
	addUniqueFold(&metadata.ISRCs, value)
}

func addExternalID(metadata *Metadata, platformName, id string) {
	if metadata == nil {
		return
	}
	if metadata.ExternalIDs == nil {
		metadata.ExternalIDs = make(map[string][]string)
	}
	values := metadata.ExternalIDs[platformName]
	addUnique(&values, id)
	metadata.ExternalIDs[platformName] = values
}

func isKnownISRC(metadata Metadata, value string) bool {
	value = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if value == "" {
		return false
	}
	for _, known := range append([]string{metadata.ISRC}, metadata.ISRCs...) {
		if strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(known), "-", ""), value) {
			return true
		}
	}
	return false
}

func addUnique(target *[]string, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, current := range *target {
			if current == value {
				found = true
				break
			}
		}
		if !found {
			*target = append(*target, value)
		}
	}
}

func addUniqueFold(target *[]string, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, current := range *target {
			if strings.EqualFold(current, value) {
				found = true
				break
			}
		}
		if !found {
			*target = append(*target, value)
		}
	}
}

func appendUnique(values []string, value string) []string {
	addUnique(&values, value)
	return values
}

func normalizeMatchText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func anyNormalizedEqual(values []string, candidate string) bool {
	candidate = normalizeMatchText(candidate)
	if candidate == "" {
		return false
	}
	for _, value := range values {
		if normalizeMatchText(value) == candidate {
			return true
		}
	}
	return false
}

func anyPairNormalizedEqual(left, right []string) bool {
	for _, value := range right {
		if anyNormalizedEqual(left, value) {
			return true
		}
	}
	return false
}

func metadataNeedsRefresh(metadata Metadata) bool {
	if metadata.ResolvedAt.IsZero() {
		return true
	}
	return len(metadata.UnresolvedPlatforms) > 0 && time.Since(metadata.ResolvedAt) >= metadataRetryAfter
}
