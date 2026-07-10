package studio

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	lyricservice "github.com/liuran001/MusicBot-Go/webapp/lyrics"
)

type metadataTestPlatform struct {
	name   string
	search []platform.Track
	tracks map[string]platform.Track
}

func (p *metadataTestPlatform) Name() string              { return p.name }
func (p *metadataTestPlatform) SupportsDownload() bool    { return false }
func (p *metadataTestPlatform) SupportsSearch() bool      { return true }
func (p *metadataTestPlatform) SupportsLyrics() bool      { return false }
func (p *metadataTestPlatform) SupportsRecognition() bool { return false }
func (p *metadataTestPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{Search: true}
}
func (p *metadataTestPlatform) GetDownloadInfo(context.Context, string, platform.Quality) (*platform.DownloadInfo, error) {
	return nil, platform.ErrUnsupported
}
func (p *metadataTestPlatform) Search(context.Context, string, int) ([]platform.Track, error) {
	return append([]platform.Track(nil), p.search...), nil
}
func (p *metadataTestPlatform) GetLyrics(context.Context, string) (*platform.Lyrics, error) {
	return nil, platform.ErrUnsupported
}
func (p *metadataTestPlatform) RecognizeAudio(context.Context, io.Reader) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}
func (p *metadataTestPlatform) GetTrack(_ context.Context, id string) (*platform.Track, error) {
	track, ok := p.tracks[id]
	if !ok {
		return nil, platform.ErrNotFound
	}
	return &track, nil
}
func (p *metadataTestPlatform) GetArtist(context.Context, string) (*platform.Artist, error) {
	return nil, platform.ErrUnsupported
}
func (p *metadataTestPlatform) GetAlbum(context.Context, string) (*platform.Album, error) {
	return nil, platform.ErrUnsupported
}
func (p *metadataTestPlatform) GetPlaylist(context.Context, string) (*platform.Playlist, error) {
	return nil, platform.ErrUnsupported
}

func metadataTrack(platformName, id, title, artist, album, isrc string, duration time.Duration) platform.Track {
	return platform.Track{
		ID: id, Platform: platformName, Title: title,
		Artists: []platform.Artist{{Name: artist, Platform: platformName}},
		Album:   &platform.Album{Title: album, Platform: platformName},
		ISRC:    isrc, Duration: duration,
	}
}

func TestEnrichMetadataResolvesFourPlatformsAndISRC(t *testing.T) {
	manager := platform.NewManager()
	source := metadataTrack("netease", "n1", "Future", "Artist", "Album", "", 3*time.Minute)
	apple := metadataTrack("applemusic", "a1", "Future", "Artist", "Album", "USAAA2400001", 3*time.Minute)
	spotify := metadataTrack("spotify", "s1", "未来", "艺人", "海外专辑", "USAAA2400001", 3*time.Minute)
	qq := metadataTrack("qqmusic", "q1", "Future", "Artist", "Album", "", 3*time.Minute)

	manager.Register(&metadataTestPlatform{name: "netease", tracks: map[string]platform.Track{"n1": source}})
	manager.Register(&metadataTestPlatform{name: "applemusic", search: []platform.Track{apple}, tracks: map[string]platform.Track{"a1": apple}})
	manager.Register(&metadataTestPlatform{name: "spotify", search: []platform.Track{spotify}, tracks: map[string]platform.Track{"s1": spotify}})
	manager.Register(&metadataTestPlatform{name: "qqmusic", search: []platform.Track{qq}, tracks: map[string]platform.Track{"q1": qq}})

	service := &Service{platforms: manager}
	got := service.enrichMetadata(context.Background(), &source, Metadata{
		MusicName: "Future", Artists: []string{"Artist"}, Album: "Album", DurationMS: source.Duration.Milliseconds(),
		ExternalIDs: map[string][]string{"netease": {"n1"}},
	})

	for platformName, want := range map[string]string{"netease": "n1", "qqmusic": "q1", "spotify": "s1", "applemusic": "a1"} {
		if len(got.ExternalIDs[platformName]) != 1 || got.ExternalIDs[platformName][0] != want {
			t.Fatalf("%s IDs = %#v, want %q", platformName, got.ExternalIDs[platformName], want)
		}
	}
	if len(got.ISRCs) != 1 || got.ISRCs[0] != "USAAA2400001" {
		t.Fatalf("ISRCs = %#v", got.ISRCs)
	}
	if got.Matches["spotify"].MatchType != "exact_isrc" || got.Matches["spotify"].Score != 100 {
		t.Fatalf("spotify match = %+v", got.Matches["spotify"])
	}
	if len(got.UnresolvedPlatforms) != 0 {
		t.Fatalf("unresolved = %#v", got.UnresolvedPlatforms)
	}
}

func TestEnrichMetadataDoesNotAcceptAmbiguousFuzzyMatch(t *testing.T) {
	manager := platform.NewManager()
	source := metadataTrack("netease", "n1", "Future", "Artist", "Source Album", "", 3*time.Minute)
	first := metadataTrack("qqmusic", "q1", "Future", "Artist", "Other Album", "", 3*time.Minute)
	second := metadataTrack("qqmusic", "q2", "Future", "Artist", "Another Album", "", 3*time.Minute)
	manager.Register(&metadataTestPlatform{name: "netease", tracks: map[string]platform.Track{"n1": source}})
	manager.Register(&metadataTestPlatform{name: "qqmusic", search: []platform.Track{first, second}})

	service := &Service{platforms: manager}
	got := service.enrichMetadata(context.Background(), &source, Metadata{
		MusicName: "Future", Artists: []string{"Artist"}, Album: "Source Album", DurationMS: source.Duration.Milliseconds(),
		ExternalIDs: map[string][]string{"netease": {"n1"}},
	})
	if len(got.ExternalIDs["qqmusic"]) != 0 {
		t.Fatalf("ambiguous IDs should not be accepted: %#v", got.ExternalIDs["qqmusic"])
	}
	if !got.Matches["qqmusic"].RequiresConfirmation || got.Matches["qqmusic"].Score != 85 {
		t.Fatalf("qq match = %+v", got.Matches["qqmusic"])
	}
}

func TestMergeLyricAssetMetadataPreservesAllAMLLDBIDs(t *testing.T) {
	metadata := Metadata{MusicName: "Future", ExternalIDs: map[string][]string{"netease": {"n1"}}}
	asset := &lyricservice.Asset{
		ExternalIDs: map[string][]string{"netease": {"n1"}, "qqmusic": {"q1"}, "spotify": {"s1"}, "applemusic": {"a1"}},
		Metadata:    map[string][]string{"musicName": {"Future", "未来"}, "album": {"Album"}, "isrc": {"US-AAA-24-00001"}},
	}
	mergeLyricAssetMetadata(&metadata, nil, asset)
	if len(metadata.ExternalIDs) != 4 {
		t.Fatalf("external IDs = %#v", metadata.ExternalIDs)
	}
	if len(metadata.MusicNames) != 2 || len(metadata.ISRCs) != 1 || metadata.ISRCs[0] != "USAAA2400001" {
		t.Fatalf("metadata aliases/isrc = %#v / %#v", metadata.MusicNames, metadata.ISRCs)
	}
}
