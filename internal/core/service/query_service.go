package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/obs/metrics"
	"gophermind/pkg/contracts/events"
)

// QueryService handles synchronous QA requests.
type QueryService struct {
	repo      SessionRepository
	sessions  *SessionService
	router    ModelRouter
	rag       RAGClient
	queue     QueueProducer
	cache     SessionCache
	memories  *MemoryService
	traces    TraceReporter
	evals     *EvalService
	logger    *zap.Logger
}

// NewQueryService builds QueryService.
func NewQueryService(repo SessionRepository, sessions *SessionService, router ModelRouter, rag RAGClient, queue QueueProducer, cache SessionCache, memories *MemoryService, traces TraceReporter, evals *EvalService, logger *zap.Logger) *QueryService {
	return &QueryService{
		repo:     repo,
		sessions: sessions,
		router:   router,
		rag:      rag,
		queue:    queue,
		cache:    cache,
		memories: memories,
		traces:   traces,
		evals:    evals,
		logger:   logger,
	}
}

// Query executes synchronous QA.
func (s *QueryService) Query(ctx context.Context, in model.QueryInput) (model.QueryOutput, error) {
	modelType := normalizedModelType(in.ModelType)
	success := false
	start := time.Now()
	requestID := uuid.NewString()
	traceID := requestID
	defer func() {
		metrics.ObserveQueryLatency(time.Since(start))
		metrics.IncQueryRequest(success, modelType, in.UseRAG)
	}()

	sessionID, err := s.ensureSessionAndUserMessage(ctx, in, requestID)
	if err != nil {
		return model.QueryOutput{}, err
	}

	jobID := uuid.NewString()
	if s.queue != nil {
		_ = s.queue.PublishTask(ctx, events.TaskMessage{
			EventType:      "query.task",
			Version:        "v1",
			JobID:          jobID,
			IdempotencyKey: requestID,
			UserID:         in.UserID,
			SessionID:      sessionID,
			RequestID:      requestID,
			ModelType:      modelType,
			Question:       in.Question,
			UseRAG:         in.UseRAG,
			TraceID:        traceID,
			CreatedAt:      time.Now(),
		})
	}

	prompt, rewrittenQuery, retrievedDocs, citations := s.buildPrompt(ctx, in.UserID, sessionID, in.DocumentID, in.Question, in.UseRAG)
	answer := NoDataFallbackAnswer
	usage := fallbackUsage()
	if !(in.UseRAG && len(citations) == 0) {
		modelCtx, cancel := context.WithTimeout(ctx, modelRequestTimeout(modelType))
		defer cancel()
		answer, usage, err = s.router.GenerateWithFallback(modelCtx, modelType, prompt)
		if err != nil {
			if s.queue != nil {
				_ = s.queue.PublishResult(ctx, events.ResultMessage{
					EventType:      "query.result",
					Version:        "v1",
					JobID:          jobID,
					IdempotencyKey: requestID,
					RequestID:      requestID,
					SessionID:      sessionID,
					UserID:         in.UserID,
					Status:         "failed",
					Error:          err.Error(),
					TraceID:        traceID,
					CreatedAt:      time.Now(),
				})
			}
			return model.QueryOutput{}, err
		}
	}

	if err := s.repo.AppendAssistantMessage(ctx, in.UserID, sessionID, answer, requestID, usage.Provider, modelType); err != nil {
		return model.QueryOutput{}, err
	}
	_ = s.sessions.UpdateShortTermMemory(ctx, in.UserID, sessionID)

	if s.queue != nil {
		_ = s.queue.PublishResult(ctx, events.ResultMessage{
			EventType:      "query.result",
			Version:        "v1",
			JobID:          jobID,
			IdempotencyKey: requestID,
			RequestID:      requestID,
			SessionID:      sessionID,
			UserID:         in.UserID,
			Status:         "ok",
			Answer:         answer,
			Provider:       usage.Provider,
			TraceID:        traceID,
			CreatedAt:      time.Now(),
		})
	}

	trace := QueryTrace{
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
	}
	if s.traces != nil {
		_ = s.traces.ReportQuery(ctx, trace)
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

func (s *QueryService) ensureSessionAndUserMessage(ctx context.Context, in model.QueryInput, requestID string) (string, error) {
	title := in.Question
	if len(title) > 64 {
		title = title[:64]
	}
	if in.SessionID == "" {
		created, err := s.repo.CreateSessionWithFirstMessage(ctx, in.UserID, title, in.Question, requestID)
		if err != nil {
			return "", err
		}
		return created.ID, nil
	}
	if err := s.repo.AppendUserMessage(ctx, in.UserID, in.SessionID, in.Question, requestID); err != nil {
		return "", err
	}
	return in.SessionID, nil
}

func (s *QueryService) buildPrompt(ctx context.Context, userID string, sessionID string, documentID string, question string, useRAG bool) (string, string, []model.RAGDocument, []model.Citation) {
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
		"If evidence is insufficient, state uncertainty and recommend professional care when appropriate.\n"
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

func normalizedModelType(modelType string) string {
	modelType = strings.TrimSpace(strings.ToLower(modelType))
	if modelType == "" {
		return "auto"
	}
	return modelType
}
