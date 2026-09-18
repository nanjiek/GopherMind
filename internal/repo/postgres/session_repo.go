package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gophermind/internal/core/model"
	"gophermind/internal/session/eventlog"
	"gophermind/internal/session/surface"
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
	if userID == "" || requestID == "" {
		return model.Session{}, eventlog.ErrInvalidAppend
	}
	scope := eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: userID}
	events := NewEventStore(r.db)
	if replay, ok, err := events.findReplay(ctx, scope, "", sessionCreatedKey(requestID)); err != nil {
		return model.Session{}, err
	} else if ok {
		return r.GetSession(ctx, userID, replay.SessionID)
	}

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
		if err := ensureEventStreamTx(tx, scope, sess.ID); err != nil {
			return err
		}
		if _, err := appendEventTx(tx, eventlog.AppendRequest{
			Scope: scope, SessionID: sess.ID,
			EventType: eventlog.SessionCreatedV1, SchemaVersion: 1,
			RequestID: requestID, IdempotencyKey: sessionCreatedKey(requestID),
			CorrelationID: requestID, ActorType: "user", ActorID: userID,
			Payload: map[string]any{"title": title, "model_preference": sess.ModelPref}, OccurredAt: now,
		}); err != nil {
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
		if _, err := appendEventTx(tx, messageEventRequest(scope, sess.ID, msg, userMessageKey(requestID))); err != nil {
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
	if err != nil && isPostgresUniqueViolation(err) {
		if replay, ok, replayErr := events.findReplay(ctx, scope, "", sessionCreatedKey(requestID)); replayErr != nil {
			return model.Session{}, replayErr
		} else if ok {
			return r.GetSession(ctx, userID, replay.SessionID)
		}
	}
	return out, err
}

// AppendUserMessage 追加用户消息并更新会话时间。
func (r *SessionRepository) AppendUserMessage(ctx context.Context, userID string, sessionID string, question string, requestID string) error {
	if userID == "" || sessionID == "" || requestID == "" {
		return eventlog.ErrInvalidAppend
	}
	scope := eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: userID}
	events := NewEventStore(r.db)
	if _, ok, err := events.findReplay(ctx, scope, sessionID, userMessageKey(requestID)); err != nil || ok {
		return err
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if _, err := appendEventTx(tx, messageEventRequest(scope, sessionID, msg, userMessageKey(requestID))); err != nil {
			return err
		}
		return nil
	})
	if err != nil && isPostgresUniqueViolation(err) {
		_, ok, replayErr := events.findReplay(ctx, scope, sessionID, userMessageKey(requestID))
		if replayErr != nil {
			return replayErr
		}
		if ok {
			return nil
		}
	}
	return err
}

// AppendAssistantMessage 追加 AI 回复并更新会话时间。
func (r *SessionRepository) AppendAssistantMessage(ctx context.Context, userID string, sessionID string, answer string, requestID string, provider string, modelName string) error {
	if userID == "" || sessionID == "" || requestID == "" {
		return eventlog.ErrInvalidAppend
	}
	scope := eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: userID}
	events := NewEventStore(r.db)
	if _, ok, err := events.findReplay(ctx, scope, sessionID, assistantMessageKey(requestID)); err != nil || ok {
		return err
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if _, err := appendEventTx(tx, messageEventRequest(scope, sessionID, msg, assistantMessageKey(requestID))); err != nil {
			return err
		}
		return nil
	})
	if err != nil && isPostgresUniqueViolation(err) {
		_, ok, replayErr := events.findReplay(ctx, scope, sessionID, assistantMessageKey(requestID))
		if replayErr != nil {
			return replayErr
		}
		if ok {
			return nil
		}
	}
	return err
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

func (r *SessionRepository) CurrentStreamSequence(ctx context.Context, userID string, sessionID string) (int64, error) {
	return NewEventStore(r.db).CurrentSequence(ctx, eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: userID}, sessionID)
}

func (r *SessionRepository) BuildRecentSurface(ctx context.Context, userID string, sessionID string, limit int) (surface.Surface, error) {
	if limit <= 0 || limit > 100 {
		limit = 6
	}
	result := surface.Surface{
		Key:               surface.ConversationKey(userID, sessionID),
		ProjectionVersion: surface.CurrentVersion,
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stream EventStreamModel
		if err := tx.Where("session_id = ? AND tenant_id = ? AND user_id = ?", sessionID, eventlog.DefaultTenantID, userID).First(&stream).Error; err != nil {
			return err
		}
		result.SourceSeq = stream.NextSeq - 1
		var rows []MessageModel
		if err := tx.Where("session_id = ? AND user_id = ?", sessionID, userID).
			Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		result.Messages = make([]model.Message, len(rows))
		for i := range rows {
			result.Messages[len(rows)-1-i] = mapMessage(rows[i])
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return result, err
}

func messageEventRequest(scope eventlog.Scope, sessionID string, msg MessageModel, idempotencyKey string) eventlog.AppendRequest {
	actorID := scope.UserID
	if msg.Role == "assistant" {
		actorID = msg.Provider
		if actorID == "" {
			actorID = "assistant"
		}
	}
	return eventlog.AppendRequest{
		Scope: scope, SessionID: sessionID,
		EventType: eventlog.MessageAppendedV1, SchemaVersion: 1,
		RequestID: msg.RequestID, IdempotencyKey: idempotencyKey,
		CorrelationID: msg.RequestID, ActorType: msg.Role, ActorID: actorID,
		Payload: map[string]any{
			"message_id": msg.ID, "role": msg.Role, "content": msg.Content,
			"provider": msg.Provider, "model": msg.ModelName,
		},
		OccurredAt: msg.CreatedAt,
	}
}

func sessionCreatedKey(requestID string) string   { return requestID + ":session" }
func userMessageKey(requestID string) string      { return requestID + ":user" }
func assistantMessageKey(requestID string) string { return requestID + ":assistant" }

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
