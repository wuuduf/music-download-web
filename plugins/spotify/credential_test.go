package spotify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestSpotifyCredentialImportRejectsInvalidWVD(t *testing.T) {
	plat := NewPlatform(nil)
	_, err := plat.ImportCredentialFile(context.Background(), platform.CredentialFileImportRequest{
		Kind:        "widevine",
		FileName:    "device.wvd",
		Destination: filepath.Join(t.TempDir(), "device.wvd"),
		Data:        []byte("not a wvd"),
	})
	if err == nil {
		t.Fatal("ImportCredentialFile() accepted invalid WVD")
	}
}

func TestAtomicWriteCredentialUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "device.wvd")
	if err := atomicWriteCredential(path, []byte("test")); err != nil {
		t.Fatalf("atomicWriteCredential() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
