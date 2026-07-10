package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrStudioRevisionConflict = errors.New("studio revision conflict")

func (r *Repository) CreateStudioProject(ctx context.Context, project *StudioProjectModel, initial *StudioRevisionModel) error {
	if r == nil || r.dataDB == nil || project == nil || initial == nil {
		return errors.New("studio repository not configured")
	}
	return r.dataDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return err
		}
		initial.ProjectID = project.ProjectID
		initial.Revision = 1
		return tx.Create(initial).Error
	})
}

func (r *Repository) GetStudioProject(ctx context.Context, projectID string) (*StudioProjectModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("studio repository not configured")
	}
	var project StudioProjectModel
	if err := r.dataDB.WithContext(ctx).Where("project_id = ?", projectID).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *Repository) SaveStudioRevision(ctx context.Context, projectID string, expectedRevision int, revision *StudioRevisionModel) (int, error) {
	if r == nil || r.dataDB == nil || revision == nil {
		return 0, errors.New("studio repository not configured")
	}
	next := expectedRevision + 1
	err := r.dataDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&StudioProjectModel{}).
			Where("project_id = ? AND current_revision = ?", projectID, expectedRevision).
			Update("current_revision", next)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrStudioRevisionConflict
		}
		revision.ProjectID = projectID
		revision.Revision = next
		return tx.Create(revision).Error
	})
	return next, err
}

func (r *Repository) GetStudioRevision(ctx context.Context, projectID string, revision int) (*StudioRevisionModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("studio repository not configured")
	}
	var value StudioRevisionModel
	query := r.dataDB.WithContext(ctx).Where("project_id = ?", projectID)
	if revision > 0 {
		query = query.Where("revision = ?", revision)
	} else {
		query = query.Order("revision DESC")
	}
	if err := query.First(&value).Error; err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *Repository) ListStudioRevisions(ctx context.Context, projectID string) ([]StudioRevisionModel, error) {
	if r == nil || r.dataDB == nil {
		return nil, errors.New("studio repository not configured")
	}
	var values []StudioRevisionModel
	err := r.dataDB.WithContext(ctx).Where("project_id = ?", projectID).Order("revision DESC").Find(&values).Error
	return values, err
}

func (r *Repository) RestoreStudioRevision(ctx context.Context, projectID string, expectedRevision, sourceRevision int) (int, error) {
	source, err := r.GetStudioRevision(ctx, projectID, sourceRevision)
	if err != nil {
		return 0, err
	}
	return r.SaveStudioRevision(ctx, projectID, expectedRevision, &StudioRevisionModel{Content: source.Content, MetadataJSON: source.MetadataJSON})
}

func (r *Repository) UpsertStudioProject(ctx context.Context, project *StudioProjectModel) error {
	if r == nil || r.dataDB == nil || project == nil {
		return errors.New("studio repository not configured")
	}
	return r.dataDB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"metadata_json", "playback_session", "status", "updated_at"}),
	}).Create(project).Error
}
