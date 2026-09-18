package postgres

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gophermind/internal/session/projection"
)

type ProjectionStore struct {
	db *gorm.DB
}

var _ projection.Store = (*ProjectionStore)(nil)

func NewProjectionStore(db *gorm.DB) *ProjectionStore {
	return &ProjectionStore{db: db}
}

type ProjectionCheckpointModel struct {
	ProjectionName string `gorm:"primaryKey"`
	Shard          int    `gorm:"primaryKey"`
	LastEventID    *string
	LastRecordedAt *time.Time
	UpdatedAt      time.Time
}

func (ProjectionCheckpointModel) TableName() string { return "projection_checkpoints" }

func (s *ProjectionStore) Load(ctx context.Context, projectionName string, shard int) (projection.Checkpoint, bool, error) {
	var row ProjectionCheckpointModel
	result := s.db.WithContext(ctx).
		Where("projection_name = ? AND shard = ?", projectionName, shard).
		First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return projection.Checkpoint{}, false, nil
	}
	if result.Error != nil {
		return projection.Checkpoint{}, false, result.Error
	}
	return mapProjectionCheckpoint(row), true, nil
}

func (s *ProjectionStore) Save(ctx context.Context, checkpoint projection.Checkpoint) error {
	row := ProjectionCheckpointModel{
		ProjectionName: checkpoint.ProjectionName,
		Shard:          checkpoint.Shard,
		LastEventID:    checkpoint.LastEventID,
		LastRecordedAt: checkpoint.LastRecordedAt,
		UpdatedAt:      time.Now().UTC(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "projection_name"}, {Name: "shard"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_event_id", "last_recorded_at", "updated_at"}),
	}).Create(&row).Error
}

func mapProjectionCheckpoint(row ProjectionCheckpointModel) projection.Checkpoint {
	return projection.Checkpoint{
		ProjectionName: row.ProjectionName,
		Shard:          row.Shard,
		LastEventID:    row.LastEventID,
		LastRecordedAt: row.LastRecordedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
