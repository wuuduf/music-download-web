package applemusic

import (
	"reflect"
	"testing"
)

func TestSplitComposerNames(t *testing.T) {
	got := splitComposerNames("方大同 & Edward Chan、AC/DC")
	want := []string{"方大同", "Edward Chan", "AC/DC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitComposerNames() = %#v, want %#v", got, want)
	}
}

func TestConvertSongIncludesComposerCredits(t *testing.T) {
	track := convertSong(appleMusicResource{ID: "1", Attributes: appleMusicAttributes{
		Name: "Song", ArtistName: "Artist", ComposerName: "Writer A & Writer B",
	}})
	want := []string{"Writer A", "Writer B"}
	if !reflect.DeepEqual(track.Songwriters, want) {
		t.Fatalf("songwriters = %#v, want %#v", track.Songwriters, want)
	}
}
