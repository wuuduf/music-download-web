package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrShortcutAPIKeyNotFound  = errors.New("shortcut API key not found")
	ErrShortcutAPIKeyDisabled  = errors.New("shortcut API key disabled")
	ErrShortcutAPIKeyExhausted = errors.New("shortcut API key quota exhausted")
)

func (r *Repository) CreateShortcutAPIKey(ctx context.Context, model *ShortcutAPIKeyModel) error {
	if r == nil || r.dataDB == nil || model == nil {
		return errors.New("repository not configured")
	}
	return r.dataDB.WithContext(ctx).Create(model).Error
}

func (r *Repository) ListShortcutAPIKeys(ctx context.Context) ([]ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	var values []ShortcutAPIKeyModel
	err := r.dataDB.WithContext(ctx).Order("created_at DESC").Find(&values).Error
	return values, err
}

func (r *Repository) FindShortcutAPIKeyByHash(ctx context.Context, hash string) (*ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	var model ShortcutAPIKeyModel
	err := r.dataDB.WithContext(ctx).Where("secret_hash = ?", strings.TrimSpace(hash)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShortcutAPIKeyNotFound
	}
	return &model, err
}

func (r *Repository) FindShortcutAPIKey(ctx context.Context, keyID string) (*ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	var model ShortcutAPIKeyModel
	err := r.dataDB.WithContext(ctx).Where("key_id = ?", strings.TrimSpace(keyID)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShortcutAPIKeyNotFound
	}
	return &model, err
}

// ConsumeShortcutAPIKey atomically uses one successful parse from a key.
func (r *Repository) ConsumeShortcutAPIKey(ctx context.Context, keyID string) (*ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	now := time.Now()
	result := r.dataDB.WithContext(ctx).Model(&ShortcutAPIKeyModel{}).
		Where("key_id = ? AND enabled = ? AND (usage_limit = 0 OR used < usage_limit)", strings.TrimSpace(keyID), true).
		Updates(map[string]any{"used": gorm.Expr("used + 1"), "last_used_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		model, err := r.FindShortcutAPIKey(ctx, keyID)
		if err != nil {
			return nil, err
		}
		if !model.Enabled {
			return nil, ErrShortcutAPIKeyDisabled
		}
		return nil, ErrShortcutAPIKeyExhausted
	}
	return r.FindShortcutAPIKey(ctx, keyID)
}

func (r *Repository) UpdateShortcutAPIKey(ctx context.Context, keyID, name string, usageLimit int64, enabled bool) (*ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	result := r.dataDB.WithContext(ctx).Model(&ShortcutAPIKeyModel{}).Where("key_id = ?", strings.TrimSpace(keyID)).Updates(map[string]any{
		"name": name, "usage_limit": usageLimit, "enabled": enabled,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrShortcutAPIKeyNotFound
	}
	return r.FindShortcutAPIKey(ctx, keyID)
}

func (r *Repository) ResetShortcutAPIKeyUsage(ctx context.Context, keyID string) (*ShortcutAPIKeyModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	result := r.dataDB.WithContext(ctx).Model(&ShortcutAPIKeyModel{}).Where("key_id = ?", strings.TrimSpace(keyID)).Updates(map[string]any{"used": 0, "last_used_at": nil})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrShortcutAPIKeyNotFound
	}
	return r.FindShortcutAPIKey(ctx, keyID)
}

func (r *Repository) DeleteShortcutAPIKey(ctx context.Context, keyID string) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	result := r.dataDB.WithContext(ctx).Where("key_id = ?", strings.TrimSpace(keyID)).Delete(&ShortcutAPIKeyModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrShortcutAPIKeyNotFound
	}
	return nil
}
