package mysql

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// MemoryCatalogRepository stores listable memory metadata.
type MemoryCatalogRepository struct {
	db *gorm.DB
}

// NewMemoryCatalogRepository builds MemoryCatalogRepository.
func NewMemoryCatalogRepository(db *gorm.DB) *MemoryCatalogRepository {
	return &MemoryCatalogRepository{db: db}
}

// Upsert inserts or updates memory metadata.
func (r *MemoryCatalogRepository) Upsert(ctx context.Context, memory model.Memory) error {
	return r.db.WithContext(ctx).Save(&MemoryCatalogModel{
		ID:      memory.ID,
		UserID:  memory.UserID,
		Content: memory.Content,
		Tags:    strings.Join(memory.Tags, ","),
		Enabled: memory.Enabled,
		Source:  memory.Source,
	}).Error
}

// List returns recent memories for one user.
func (r *MemoryCatalogRepository) List(ctx context.Context, userID string, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []MemoryCatalogModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]model.Memory, 0, len(items))
	for _, item := range items {
		tags := []string{}
		if item.Tags != "" {
			tags = strings.Split(item.Tags, ",")
		}
		out = append(out, model.Memory{
			ID:        item.ID,
			UserID:    item.UserID,
			Content:   item.Content,
			Tags:      tags,
			Enabled:   item.Enabled,
			Source:    item.Source,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return out, nil
}

// Delete removes one memory metadata row.
func (r *MemoryCatalogRepository) Delete(ctx context.Context, userID string, memoryID string) error {
	return r.db.WithContext(ctx).Where("id = ? AND user_id = ?", memoryID, userID).Delete(&MemoryCatalogModel{}).Error
}
