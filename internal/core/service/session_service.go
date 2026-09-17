package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/core/model"
)

// SessionService 封装会话读取和摘要缓存逻辑。
type SessionService struct {
	repo   SessionRepository
	cache  SessionCache
	logger *zap.Logger
}

// NewSessionService 构建 SessionService。
func NewSessionService(repo SessionRepository, cache SessionCache, logger *zap.Logger) *SessionService {
	return &SessionService{
		repo:   repo,
		cache:  cache,
		logger: logger,
	}
}

// GetSessionWithMessages 获取会话及消息历史。
func (s *SessionService) GetSessionWithMessages(ctx context.Context, userID string, sessionID string) (model.Session, []model.Message, error) {
	session, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return model.Session{}, nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return model.Session{}, nil, err
	}
	return session, msgs, nil
}

// ListSessions returns latest sessions by user.
func (s *SessionService) ListSessions(ctx context.Context, userID string, limit int) ([]model.Session, error) {
	return s.repo.ListSessions(ctx, userID, limit)
}

// LoadSummary 读取会话摘要，若缓存 miss 则从历史推导并回填。
func (s *SessionService) LoadSummary(ctx context.Context, userID string, sessionID string) (string, error) {
	summary, ok, err := s.cache.GetSummary(ctx, userID, sessionID)
	if err == nil && ok {
		return summary, nil
	}

	msgs, err := s.repo.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return "", err
	}
	summary = buildSummaryFromMessages(msgs)
	_ = s.cache.SetSummary(ctx, userID, sessionID, summary, 24*time.Hour)
	_ = s.cache.SetWindow(ctx, userID, sessionID, trimWindowMessages(msgs), 24*time.Hour)
	return summary, nil
}

// LoadWindow returns the recent short-term window.
func (s *SessionService) LoadWindow(ctx context.Context, userID string, sessionID string) ([]model.Message, error) {
	window, ok, err := s.cache.GetWindow(ctx, userID, sessionID)
	if err == nil && ok {
		return window, nil
	}
	msgs, err := s.repo.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	window = trimWindowMessages(msgs)
	_ = s.cache.SetWindow(ctx, userID, sessionID, window, 24*time.Hour)
	return window, nil
}

// UpdateShortTermMemory refreshes both summary and recent window synchronously.
func (s *SessionService) UpdateShortTermMemory(ctx context.Context, userID string, sessionID string) error {
	msgs, err := s.repo.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	window := trimWindowMessages(msgs)
	summary := buildSummaryFromMessages(msgs)
	if err := s.cache.SetWindow(ctx, userID, sessionID, window, 24*time.Hour); err != nil && s.logger != nil {
		s.logger.Warn("set window failed", zap.Error(err))
	}
	if err := s.cache.SetSummary(ctx, userID, sessionID, summary, 24*time.Hour); err != nil && s.logger != nil {
		s.logger.Warn("set summary failed", zap.Error(err))
	}
	return nil
}

func buildSummaryFromMessages(messages []model.Message) string {
	if len(messages) == 0 {
		return ""
	}
	start := summaryStart(messages)
	s := ""
	for i := start; i < len(messages); i++ {
		s += "[" + messages[i].Role + "] " + messages[i].Content + "\n"
	}
	return s
}

func trimWindowMessages(messages []model.Message) []model.Message {
	if len(messages) <= 6 {
		return messages
	}
	return messages[len(messages)-6:]
}

func summaryStart(messages []model.Message) int {
	start := 0
	if len(messages) > 8 {
		start = len(messages) - 8
	}
	return start
}
