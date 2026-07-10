package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"gorm.io/gorm/logger"
)

func TestStudioRevisionConflictAndRestore(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "cache.db"), filepath.Join(t.TempDir(), "data.db"), logpkg.NewGormLogger(base, logger.Silent))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	err = repo.CreateStudioProject(ctx, &StudioProjectModel{ProjectID: "p1", Platform: "netease", TrackID: "1", CurrentRevision: 1, Status: "active"}, &StudioRevisionModel{Content: "<tt>one</tt>"})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.UpdateStudioProjectMetadata(ctx, "p1", `{"isrc":"USAAA2400001"}`); err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	project, err := repo.GetStudioProject(ctx, "p1")
	if err != nil || project.MetadataJSON != `{"isrc":"USAAA2400001"}` {
		t.Fatalf("updated metadata=%q err=%v", project.MetadataJSON, err)
	}

	next, err := repo.SaveStudioRevision(ctx, "p1", 1, &StudioRevisionModel{Content: "<tt>two</tt>"})
	if err != nil || next != 2 {
		t.Fatalf("save revision: next=%d err=%v", next, err)
	}
	if _, err = repo.SaveStudioRevision(ctx, "p1", 1, &StudioRevisionModel{Content: "<tt>stale</tt>"}); !errors.Is(err, ErrStudioRevisionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	next, err = repo.RestoreStudioRevision(ctx, "p1", 2, 1)
	if err != nil || next != 3 {
		t.Fatalf("restore revision: next=%d err=%v", next, err)
	}
	revision, err := repo.GetStudioRevision(ctx, "p1", 3)
	if err != nil || revision.Content != "<tt>one</tt>" {
		t.Fatalf("restored content=%q err=%v", revision.Content, err)
	}
}
