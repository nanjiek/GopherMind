package mysql

import (
	"context"

	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// DocumentRepository persists document upload and indexing state.
type DocumentRepository struct {
	db *gorm.DB
}

// NewDocumentRepository builds DocumentRepository.
func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

// Create stores a new document record.
func (r *DocumentRepository) Create(ctx context.Context, doc model.Document) error {
	return r.db.WithContext(ctx).Create(&DocumentModel{
		ID:          doc.ID,
		UserID:      doc.UserID,
		JobID:       doc.JobID,
		FileKey:     doc.FileKey,
		Filename:    doc.Filename,
		ContentType: doc.ContentType,
		SizeBytes:   doc.SizeBytes,
		Status:      doc.Status,
	}).Error
}

// Get returns one document by user and id.
func (r *DocumentRepository) Get(ctx context.Context, userID string, documentID string) (model.Document, error) {
	var in DocumentModel
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", documentID, userID).First(&in).Error; err != nil {
		return model.Document{}, err
	}
	return mapDocument(in), nil
}

// UpdateStatus updates lifecycle state and error.
func (r *DocumentRepository) UpdateStatus(ctx context.Context, documentID string, status string, errMsg string) error {
	return r.db.WithContext(ctx).Model(&DocumentModel{}).
		Where("id = ?", documentID).
		Updates(map[string]any{
			"status":        status,
			"error_message": errMsg,
		}).Error
}

// GetByID returns one document by id.
func (r *DocumentRepository) GetByID(ctx context.Context, documentID string) (model.Document, error) {
	var in DocumentModel
	if err := r.db.WithContext(ctx).Where("id = ?", documentID).First(&in).Error; err != nil {
		return model.Document{}, err
	}
	return mapDocument(in), nil
}

func mapDocument(in DocumentModel) model.Document {
	return model.Document{
		ID:           in.ID,
		UserID:       in.UserID,
		JobID:        in.JobID,
		FileKey:      in.FileKey,
		Filename:     in.Filename,
		ContentType:  in.ContentType,
		SizeBytes:    in.SizeBytes,
		Status:       in.Status,
		ErrorMessage: in.ErrorMessage,
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
}
