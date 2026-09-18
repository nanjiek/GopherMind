package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// SessionRepository 提供会话与消息事务访问。
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 构建 SessionRepository。
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// CreateSessionWithFirstMessage 在一个事务内写入会话和首条用户消息。
func (r *SessionRepository) CreateSessionWithFirstMessage(ctx context.Context, userID string, title string, question string, requestID string) (model.Session, error) {
	var out model.Session
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		sess := SessionModel{
			ID:            uuid.NewString(),
			UserID:        userID,
			Title:         title,
			LastMessageAt: &now,
		}
		if err := tx.Create(&sess).Error; err != nil {
			return err
		}

		msg := MessageModel{
			SessionID: sess.ID,
			UserID:    userID,
			Role:      "user",
			Content:   question,
			RequestID: requestID,
		}
		if err := tx.Create(&msg).Error; err != nil {
			return err
		}

		out = model.Session{
			ID:            sess.ID,
			UserID:        sess.UserID,
			Title:         sess.Title,
			ModelPref:     sess.ModelPref,
			CreatedAt:     sess.CreatedAt,
			UpdatedAt:     sess.UpdatedAt,
			LastMessageAt: now,
		}
		return nil
	})
	return out, err
}

// AppendUserMessage 追加用户消息并更新会话时间。
func (r *SessionRepository) AppendUserMessage(ctx context.Context, userID string, sessionID string, question string, requestID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		result := tx.Model(&SessionModel{}).
			Where("id = ? AND user_id = ?", sessionID, userID).
			Update("last_message_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		msg := MessageModel{
			SessionID: sessionID,
			UserID:    userID,
			Role:      "user",
			Content:   question,
			RequestID: requestID,
		}
		if err := tx.Create(&msg).Error; err != nil {
			return err
		}
		return nil
	})
}

// AppendAssistantMessage 追加 AI 回复并更新会话时间。
func (r *SessionRepository) AppendAssistantMessage(ctx context.Context, userID string, sessionID string, answer string, requestID string, provider string, modelName string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		result := tx.Model(&SessionModel{}).
			Where("id = ? AND user_id = ?", sessionID, userID).
			Update("last_message_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		msg := MessageModel{
			SessionID: sessionID,
			UserID:    userID,
			Role:      "assistant",
			Content:   answer,
			RequestID: requestID,
			Provider:  provider,
			ModelName: modelName,
		}
		if err := tx.Create(&msg).Error; err != nil {
			return err
		}
		return nil
	})
}

// GetSession 查询单个会话。
func (r *SessionRepository) GetSession(ctx context.Context, userID string, sessionID string) (model.Session, error) {
	var sess SessionModel
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", sessionID, userID).
		First(&sess).Error
	if err != nil {
		return model.Session{}, err
	}
	return mapSession(sess), nil
}

// ListSessions reads latest sessions for current user ordered by activity time.
func (r *SessionRepository) ListSessions(ctx context.Context, userID string, limit int) ([]model.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	var sessions []SessionModel
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("last_message_at DESC, updated_at DESC").
		Limit(limit).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	out := make([]model.Session, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, mapSession(sess))
	}
	return out, nil
}

// ListMessages 按时间顺序读取会话消息。
func (r *SessionRepository) ListMessages(ctx context.Context, userID string, sessionID string) ([]model.Message, error) {
	var sess SessionModel
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", sessionID, userID).
		First(&sess).Error
	if err != nil {
		return nil, err
	}

	var msgs []MessageModel
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND user_id = ?", sessionID, userID).
		Order("created_at ASC").
		Find(&msgs).Error; err != nil {
		return nil, err
	}

	out := make([]model.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, mapMessage(m))
	}
	return out, nil
}

func mapSession(in SessionModel) model.Session {
	last := in.UpdatedAt
	if in.LastMessageAt != nil {
		last = *in.LastMessageAt
	}
	return model.Session{
		ID:            in.ID,
		UserID:        in.UserID,
		Title:         in.Title,
		ModelPref:     in.ModelPref,
		LastMessageAt: last,
		CreatedAt:     in.CreatedAt,
		UpdatedAt:     in.UpdatedAt,
	}
}

func mapMessage(in MessageModel) model.Message {
	return model.Message{
		ID:        in.ID,
		SessionID: in.SessionID,
		UserID:    in.UserID,
		Role:      in.Role,
		Content:   in.Content,
		RequestID: in.RequestID,
		Provider:  in.Provider,
		ModelName: in.ModelName,
		CreatedAt: in.CreatedAt,
	}
}

// IsNotFoundError 提供 repository 级 not found 判断。
func IsNotFoundError(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
