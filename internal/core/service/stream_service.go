package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/obs/metrics"
)

// StreamService handles streaming QA.
type StreamService struct {
	repo     SessionRepository
	sessions *SessionService
	router   ModelRouter
	rag      RAGClient
	cache    SessionCache
	memories *MemoryService
	traces   TraceReporter
	evals    *EvalService
	logger   *zap.Logger
}

// NewStreamService builds StreamService.
func NewStreamService(repo SessionRepository, sessions *SessionService, router ModelRouter, rag RAGClient, cache SessionCache, memories *MemoryService, traces TraceReporter, evals *EvalService, logger *zap.Logger) *StreamService {
	return &StreamService{
		repo:     repo,
		sessions: sessions,
		router:   router,
		rag:      rag,
		cache:    cache,
		memories: memories,
		traces:   traces,
		evals:    evals,
		logger:   logger,
	}
}

// Stream executes streaming QA and emits tokens.
func (s *StreamService) Stream(ctx context.Context, in model.QueryInput, onToken func(string) error) (model.QueryOutput, error) {
	modelType := normalizedStreamModelType(in.ModelType)
	success := false
	start := time.Now()
	requestID := uuid.NewString()
	traceID := requestID
	defer func() {
		metrics.IncStreamRequest(success, modelType, in.UseRAG)
	}()

	sessionID, err := s.ensureSessionAndUserMessage(ctx, in, requestID)
	if err != nil {
		return model.QueryOutput{}, err
	}

	prompt, rewrittenQuery, retrievedDocs, citations := s.buildPrompt(ctx, in.UserID, sessionID, in.DocumentID, in.Question, in.UseRAG)
	if in.UseRAG && len(citations) == 0 {
		answer := NoDataFallbackAnswer
		usage := fallbackUsage()
		if err := onToken(answer); err != nil {
			return model.QueryOutput{}, err
		}
		if err := s.repo.AppendAssistantMessage(ctx, in.UserID, sessionID, answer, requestID, usage.Provider, modelType); err != nil {
			return model.QueryOutput{}, err
		}
		_ = s.sessions.UpdateShortTermMemory(ctx, in.UserID, sessionID)
		if s.traces != nil {
			_ = s.traces.ReportQuery(ctx, QueryTrace{
				TraceID:        traceID,
				RequestID:      requestID,
				SessionID:      sessionID,
				UserID:         in.UserID,
				Model:          modelType,
				Prompt:         prompt,
				Question:       in.Question,
				RewrittenQuery: rewrittenQuery,
				RetrievedDocs:  retrievedDocs,
				Citations:      citations,
				Answer:         answer,
				Latency:        time.Since(start),
				Status:         "ok",
			})
		}
		success = true
		return model.QueryOutput{
			RequestID: requestID,
			SessionID: sessionID,
			Answer:    answer,
			Citations: citations,
			Usage:     usage,
		}, nil
	}

	var streamErr error
	firstTokenObserved := false
	modelCtx, cancel := context.WithTimeout(ctx, modelRequestTimeout(modelType))
	defer cancel()

	answer, usage, err := s.router.GenerateStreamWithFallback(modelCtx, modelType, prompt, func(token string) error {
		if !firstTokenObserved {
			firstTokenObserved = true
			metrics.ObserveStreamFirstToken(time.Since(start))
		}
		if err := s.cache.AppendStreamChunk(ctx, requestID, token, 10*time.Minute); err != nil && s.logger != nil {
			s.logger.Warn("cache stream chunk failed", zap.Error(err))
		}
		if err := onToken(token); err != nil {
			streamErr = err
			return err
		}
		return nil
	})
	if err != nil {
		return model.QueryOutput{}, err
	}
	if streamErr != nil {
		return model.QueryOutput{}, streamErr
	}

	if err := s.repo.AppendAssistantMessage(ctx, in.UserID, sessionID, answer, requestID, usage.Provider, modelType); err != nil {
		return model.QueryOutput{}, err
	}
	_ = s.sessions.UpdateShortTermMemory(ctx, in.UserID, sessionID)
	if s.traces != nil {
		_ = s.traces.ReportQuery(ctx, QueryTrace{
			TraceID:        traceID,
			RequestID:      requestID,
			SessionID:      sessionID,
			UserID:         in.UserID,
			Model:          modelType,
			Prompt:         prompt,
			Question:       in.Question,
			RewrittenQuery: rewrittenQuery,
			RetrievedDocs:  retrievedDocs,
			Citations:      citations,
			Answer:         answer,
			Latency:        time.Since(start),
			Status:         "ok",
		})
	}
	if s.evals != nil {
		s.evals.DispatchIfSampled(ctx, requestID, traceID, in.UserID, sessionID, in.Question, answer, modelType, citations)
	}

	success = true
	return model.QueryOutput{
		RequestID: requestID,
		SessionID: sessionID,
		Answer:    answer,
		Citations: citations,
		Usage:     usage,
	}, nil
}

func (s *StreamService) ensureSessionAndUserMessage(ctx context.Context, in model.QueryInput, requestID string) (string, error) {
	sessionID := in.SessionID
	if sessionID == "" {
		title := in.Question
		if len(title) > 64 {
			title = title[:64]
		}
		created, err := s.repo.CreateSessionWithFirstMessage(ctx, in.UserID, title, in.Question, requestID)
		if err != nil {
			return "", err
		}
		return created.ID, nil
	}
	if err := s.repo.AppendUserMessage(ctx, in.UserID, sessionID, in.Question, requestID); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *StreamService) buildPrompt(ctx context.Context, userID string, sessionID string, documentID string, question string, useRAG bool) (string, string, []model.RAGDocument, []model.Citation) {
	summary, err := s.sessions.LoadSummary(ctx, userID, sessionID)
	if err != nil && s.logger != nil {
		s.logger.Warn("load summary failed", zap.Error(err))
	}
	window, err := s.sessions.LoadWindow(ctx, userID, sessionID)
	if err != nil && s.logger != nil {
		s.logger.Warn("load window failed", zap.Error(err))
	}
	base := "You are a medical QA assistant.\n" +
		"Use retrieved evidence and conversation memory when available.\n" +
		"If evidence is insufficient, state uncertainty and ask for more specific disease/drug details.\n"
	if summary != "" {
		base += "Conversation summary:\n" + summary + "\n"
	}
	if len(window) > 0 {
		base += "Recent conversation window:\n"
		for _, item := range window {
			base += "[" + item.Role + "] " + item.Content + "\n"
		}
	}
	if s.memories != nil {
		mems, err := s.memories.Search(ctx, userID, question, 3)
		if err == nil && len(mems) > 0 {
			base += "Long-term user memories:\n"
			for _, item := range mems {
				base += "- " + item.Content + "\n"
			}
		}
	}

	rewrittenQuery := strings.Join(strings.Fields(strings.TrimSpace(question)), " ")
	var docs []model.RAGDocument
	if useRAG {
		retrieved, err := s.rag.Retrieve(ctx, userID, documentID, rewrittenQuery, 20)
		if err != nil && s.logger != nil {
			s.logger.Warn("rag retrieve failed", zap.Error(err))
		}
		if len(retrieved) > 0 {
			reranked, err := s.rag.Rerank(ctx, rewrittenQuery, retrieved, 5)
			if err == nil {
				docs = reranked
			} else {
				docs = retrieved
			}
		}
		kg, _ := s.rag.KnowledgeGraphPlaceholder(ctx, rewrittenQuery)
		if kg != "" {
			base += "Knowledge graph context:\n" + kg + "\n"
		}
	}

	citations := make([]model.Citation, 0, len(docs))
	if len(docs) > 0 {
		base += "Retrieved context:\n"
		for _, d := range docs {
			base += "- " + d.Content + "\n"
			citations = append(citations, model.Citation{
				DocID:   d.DocID,
				ChunkID: d.ChunkID,
				Score:   d.Score,
			})
		}
	}
	base += "\nUser question:\n" + question
	return base, rewrittenQuery, docs, citations
}

func normalizedStreamModelType(modelType string) string {
	modelType = strings.TrimSpace(strings.ToLower(modelType))
	if modelType == "" {
		return "auto"
	}
	return modelType
}
