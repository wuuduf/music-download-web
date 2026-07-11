package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/app"
	"github.com/liuran001/MusicBot-Go/bot/config"
)

func TestDecodeShortcutResolveRequestJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", strings.NewReader(`{"input":"分享文字 https://example.com/song","action":"download","quality":"lossless","wait_seconds":7}`))
	r.Header.Set("Content-Type", "application/json")
	got, err := decodeShortcutResolveRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Input != "分享文字 https://example.com/song" || got.Action != "download" || got.Quality != "lossless" || got.WaitSeconds == nil || *got.WaitSeconds != 7 {
		t.Fatalf("request = %+v", got)
	}
}

func TestDecodeShortcutResolveRequestPlainText(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", strings.NewReader("歌曲分享：https://example.com/song"))
	r.Header.Set("Content-Type", "text/plain; charset=utf-8")
	got, err := decodeShortcutResolveRequest(r)
	if err != nil || got.Input != "歌曲分享：https://example.com/song" {
		t.Fatalf("request=%+v err=%v", got, err)
	}
}

func TestShortcutAPIKeyAndPublicURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(path, []byte("WebShortcutAPIKey = test-secret\nWebPublicBaseURL = https://music.example.com/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWeb(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{core: &app.Core{Config: cfg}}

	good := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", nil)
	good.Header.Set("Authorization", "Bearer test-secret")
	if !s.requireShortcutAPIKey(httptest.NewRecorder(), good) {
		t.Fatal("valid bearer key rejected")
	}
	if got := s.shortcutPublicBaseURL(good); got != "https://music.example.com" {
		t.Fatalf("base URL = %q", got)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", nil)
	bad.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	if s.requireShortcutAPIKey(w, bad) || w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key accepted, status=%d", w.Code)
	}
}
