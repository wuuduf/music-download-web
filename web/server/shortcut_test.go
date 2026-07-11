package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/app"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
)

func TestWriteShortcutZipEntry(t *testing.T) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	if err := writeShortcutZipEntry(archive, "网易云音乐", "歌手-歌曲-网易云音乐.lrc", strings.NewReader("[00:00.00]歌词")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "网易云音乐/歌手-歌曲-网易云音乐.lrc" {
		t.Fatalf("ZIP entries = %+v", reader.File)
	}
}

func TestAdminCreatesQuotaManagedShortcutKey(t *testing.T) {
	dir := t.TempDir()
	repo, err := db.NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	s := &Server{core: &app.Core{DB: repo}}
	r := httptest.NewRequest(http.MethodPost, "/admin/api/shortcut-keys", bytes.NewBufferString(`{"name":"Jelly iPhone"}`))
	w := httptest.NewRecorder()
	s.handleAdminShortcutKeys(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		APIKey string           `json:"api_key"`
		Key    adminShortcutKey `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.APIKey, "mwsk_") || created.Key.UsageLimit != 100 || created.Key.Unlimited {
		t.Fatalf("created=%+v", created)
	}
	values, err := repo.ListShortcutAPIKeys(t.Context())
	if err != nil || len(values) != 1 || values[0].SecretHash == created.APIKey {
		t.Fatalf("stored=%+v err=%v", values, err)
	}
}

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

func TestGeneratedShortcutAPIKeyAuthentication(t *testing.T) {
	dir := t.TempDir()
	repo, err := db.NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	keyID, token, prefix, hash, err := generateShortcutAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(token))
	if hash != hex.EncodeToString(wantHash[:]) {
		t.Fatal("generated key hash mismatch")
	}
	if err := repo.CreateShortcutAPIKey(t.Context(), &db.ShortcutAPIKeyModel{KeyID: keyID, Name: "phone", Prefix: prefix, SecretHash: hash, UsageLimit: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s := &Server{core: &app.Core{DB: repo}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	principal, ok := s.authenticateShortcutAPIKey(httptest.NewRecorder(), r)
	if !ok || principal.Key == nil || principal.Key.KeyID != keyID {
		t.Fatalf("principal=%+v ok=%v", principal, ok)
	}
	if _, ok := s.consumeShortcutParse(httptest.NewRecorder(), r, principal); !ok {
		t.Fatal("first quota consume rejected")
	}
	w := httptest.NewRecorder()
	if _, ok := s.authenticateShortcutAPIKey(w, r); ok || w.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted key accepted, status=%d", w.Code)
	}
	if _, ok := s.authenticateShortcutAPIKeyAllowExhausted(httptest.NewRecorder(), r, true); !ok {
		t.Fatal("exhausted key could not retrieve assets from its final parse")
	}
}

func TestShortcutAssetNamesAndCoverUpscale(t *testing.T) {
	if got := shortcutFileStem("歌手/A", "歌:名", "netease"); got != "歌手_A-歌_名-网易云音乐" {
		t.Fatalf("stem = %q", got)
	}
	got := bestShortcutCoverURL("https://p1.music.126.net/example.jpg?param=300y300")
	if !strings.Contains(got, "param=3000y3000") {
		t.Fatalf("cover URL = %q", got)
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
	if _, ok := s.authenticateShortcutAPIKey(httptest.NewRecorder(), good); !ok {
		t.Fatal("valid bearer key rejected")
	}
	if got := s.shortcutPublicBaseURL(good); got != "https://music.example.com" {
		t.Fatalf("base URL = %q", got)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/v1/shortcut/resolve", nil)
	bad.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	if _, ok := s.authenticateShortcutAPIKey(w, bad); ok || w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid key accepted, status=%d", w.Code)
	}
}
