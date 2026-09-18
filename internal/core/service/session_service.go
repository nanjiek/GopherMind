package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/session/surface"
)

// SessionService 封装会话读取和摘要缓存逻辑。
type SessionService struct {
	repo   SessionRepository
	cache  SessionCache
	logger *zap.Logger
}

type surfaceRepository interface {
	CurrentStreamSequence(ctx context.Context, userID string, sessionID string) (int64, error)
	BuildRecentSurface(ctx context.Context, userID string, sessionID string, limit int) (surface.Surface, error)
}

type surfaceCache interface {
	GetSurface(ctx context.Context, key surface.Key) (surface.Surface, bool, error)
	PutSurface(ctx context.Context, item surface.Surface, ttl time.Duration) error
	InvalidateSurface(ctx context.Context, key surface.Key) error
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
	return summary, nil
}

// LoadWindow returns the recent short-term window.
func (s *SessionService) LoadWindow(ctx context.Context, userID string, sessionID string) ([]model.Message, error) {
	surfaceRepo, repoOK := s.repo.(surfaceRepository)
	surfaceCache, cacheOK := s.cache.(surfaceCache)
	if !repoOK || !cacheOK {
		return s.loadLegacyWindow(ctx, userID, sessionID)
	}
	key := surface.ConversationKey(userID, sessionID)
	sourceSeq, err := surfaceRepo.CurrentStreamSequence(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	item, ok, err := surfaceCache.GetSurface(ctx, key)
	if err == nil && ok && item.FreshFor(sourceSeq, time.Now()) {
		return item.Messages, nil
	}
	item, err = surfaceRepo.BuildRecentSurface(ctx, userID, sessionID, 6)
	if err != nil {
		return nil, err
	}
	_ = surfaceCache.PutSurface(ctx, item, 24*time.Hour)
	return item.Messages, nil
}

func (s *SessionService) loadLegacyWindow(ctx context.Context, userID string, sessionID string) ([]model.Message, error) {
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
	summary := buildSummaryFromMessages(msgs)
	updatedSurface := false
	if surfaceRepo, repoOK := s.repo.(surfaceRepository); repoOK {
		if surfaceCache, cacheOK := s.cache.(surfaceCache); cacheOK {
			item, buildErr := surfaceRepo.BuildRecentSurface(ctx, userID, sessionID, 6)
			if buildErr != nil {
				return buildErr
			}
			if err := surfaceCache.PutSurface(ctx, item, 24*time.Hour); err != nil && s.logger != nil {
				s.logger.Warn("put surface failed", zap.Error(err))
			}
			updatedSurface = true
		}
	}
	if !updatedSurface {
		if err := s.cache.SetWindow(ctx, userID, sessionID, trimWindowMessages(msgs), 24*time.Hour); err != nil && s.logger != nil {
			s.logger.Warn("set window failed", zap.Error(err))
		}
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
