package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestShortcutAPIKeyQuota(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	model := &ShortcutAPIKeyModel{KeyID: "key-one", Name: "phone", SecretHash: "hash-one", Prefix: "mwsk_key…", UsageLimit: 2, Enabled: true}
	if err := repo.CreateShortcutAPIKey(ctx, model); err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 2; want++ {
		got, err := repo.ConsumeShortcutAPIKey(ctx, model.KeyID)
		if err != nil || got.Used != want {
			t.Fatalf("consume %d: used=%v err=%v", want, got, err)
		}
	}
	if _, err := repo.ConsumeShortcutAPIKey(ctx, model.KeyID); !errors.Is(err, ErrShortcutAPIKeyExhausted) {
		t.Fatalf("third consume error = %v", err)
	}
	reset, err := repo.ResetShortcutAPIKeyUsage(ctx, model.KeyID)
	if err != nil || reset.Used != 0 {
		t.Fatalf("reset=%+v err=%v", reset, err)
	}
}

func TestShortcutAPIKeyUnlimited(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	model := &ShortcutAPIKeyModel{KeyID: "unlimited", Name: "unlimited", SecretHash: "hash-unlimited", Prefix: "mwsk_unlimited…", UsageLimit: 0, Enabled: true}
	if err := repo.CreateShortcutAPIKey(ctx, model); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 3; i++ {
		got, err := repo.ConsumeShortcutAPIKey(ctx, model.KeyID)
		if err != nil || got.Used != i {
			t.Fatalf("consume %d: got=%+v err=%v", i, got, err)
		}
	}
}
