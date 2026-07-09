package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveWebDownloadJob upserts a website download job by job_id.
func (r *Repository) SaveWebDownloadJob(ctx context.Context, job *WebDownloadJobModel) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	if job == nil {
		return errors.New("web download job required")
	}
	return r.dataDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job_id"}},
		UpdateAll: true,
	}).Create(job).Error
}

// FindWebDownloadJob returns a persisted website download job by job_id.
func (r *Repository) FindWebDownloadJob(ctx context.Context, jobID string) (*WebDownloadJobModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	var model WebDownloadJobModel
	err := r.dataDB.WithContext(ctx).Where("job_id = ?", jobID).First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// FindReadyWebDownloadJobByCacheKey returns a non-expired ready job for a cache key.
func (r *Repository) FindReadyWebDownloadJobByCacheKey(ctx context.Context, cacheKey string, now time.Time) (*WebDownloadJobModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	var model WebDownloadJobModel
	err := r.dataDB.WithContext(ctx).
		Where("cache_key = ? AND status = ? AND expires_at > ?", cacheKey, "ready", now).
		Order("updated_at DESC").
		First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// MarkInterruptedWebDownloadJobs marks stale active jobs failed on startup.
func (r *Repository) MarkInterruptedWebDownloadJobs(ctx context.Context) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	return r.dataDB.WithContext(ctx).Model(&WebDownloadJobModel{}).
		Where("status IN ?", []string{"queued", "running", "tagging"}).
		Updates(map[string]any{
			"status":     "failed",
			"progress":   100,
			"error":      "任务因服务重启中断，请重新创建下载任务",
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		}).Error
}

// ListWebDownloadJobs returns recent website download jobs in reverse update order.
func (r *Repository) ListWebDownloadJobs(ctx context.Context, limit, offset int) ([]*WebDownloadJobModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var models []*WebDownloadJobModel
	err := r.dataDB.WithContext(ctx).
		Order("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

// FindExpiredWebDownloadJobs returns jobs whose file cache has expired.
func (r *Repository) FindExpiredWebDownloadJobs(ctx context.Context, now time.Time, limit int) ([]*WebDownloadJobModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("repository not configured")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var models []*WebDownloadJobModel
	err := r.dataDB.WithContext(ctx).
		Where("expires_at <= ?", now).
		Order("expires_at ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

// DeleteWebDownloadJobs deletes persisted web download jobs by job_id.
func (r *Repository) DeleteWebDownloadJobs(ctx context.Context, jobIDs []string) error {
	if r == nil || r.dataDB == nil {
		return errors.New("repository not configured")
	}
	if len(jobIDs) == 0 {
		return nil
	}
	return r.dataDB.WithContext(ctx).Where("job_id IN ?", jobIDs).Delete(&WebDownloadJobModel{}).Error
}
