package mysql

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// MCPJobRepository persists asynchronous remote tool jobs.
type MCPJobRepository struct {
	db *gorm.DB
}

// NewMCPJobRepository builds MCPJobRepository.
func NewMCPJobRepository(db *gorm.DB) *MCPJobRepository {
	return &MCPJobRepository{db: db}
}

// Create stores a new MCP job.
func (r *MCPJobRepository) Create(ctx context.Context, job model.AsyncToolJob) error {
	return r.db.WithContext(ctx).Create(&MCPJobModel{
		ID:          job.ID,
		UserID:      job.UserID,
		ToolName:    job.ToolName,
		Status:      job.Status,
		ResumeToken: job.ResumeToken,
		Output:      job.Output,
		ErrorMessage: job.ErrorMessage,
	}).Error
}

// Get returns a job by id and user.
func (r *MCPJobRepository) Get(ctx context.Context, userID string, jobID string) (model.AsyncToolJob, error) {
	var in MCPJobModel
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID).First(&in).Error; err != nil {
		return model.AsyncToolJob{}, err
	}
	return mapMCPJob(in), nil
}

// UpdateResult updates async job state.
func (r *MCPJobRepository) UpdateResult(ctx context.Context, jobID string, status string, output string, errMsg string) error {
	return r.db.WithContext(ctx).Model(&MCPJobModel{}).Where("id = ?", jobID).Updates(map[string]any{
		"status":        status,
		"output":        output,
		"error_message": errMsg,
	}).Error
}

// GetByResumeToken resolves a job via resume token.
func (r *MCPJobRepository) GetByResumeToken(ctx context.Context, resumeToken string) (model.AsyncToolJob, error) {
	var in MCPJobModel
	if err := r.db.WithContext(ctx).Where("resume_token = ?", resumeToken).First(&in).Error; err != nil {
		return model.AsyncToolJob{}, err
	}
	return mapMCPJob(in), nil
}

// Exists returns whether the job exists.
func (r *MCPJobRepository) Exists(ctx context.Context, jobID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&MCPJobModel{}).Where("id = ?", jobID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsNotFound returns whether an error is a not-found condition.
func (r *MCPJobRepository) IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func mapMCPJob(in MCPJobModel) model.AsyncToolJob {
	return model.AsyncToolJob{
		ID:           in.ID,
		UserID:       in.UserID,
		ToolName:     in.ToolName,
		Status:       in.Status,
		ResumeToken:  in.ResumeToken,
		Output:       in.Output,
		ErrorMessage: in.ErrorMessage,
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
}
