package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	httptransport "gophermind/internal/transport/http"
	"gophermind/pkg/contracts/events"
	httpcontracts "gophermind/pkg/contracts/http"
)

type smokeRepo struct {
	session model.Session
	msgs    []model.Message
}

func (f *smokeRepo) CreateSessionWithFirstMessage(_ context.Context, userID, title, question, requestID string) (model.Session, error) {
	f.session = model.Session{ID: "session-e2e", UserID: userID, Title: title}
	f.msgs = append(f.msgs, model.Message{Role: "user", Content: question, RequestID: requestID, CreatedAt: time.Now()})
	return f.session, nil
}
func (f *smokeRepo) AppendUserMessage(_ context.Context, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *smokeRepo) AppendAssistantMessage(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *smokeRepo) GetSession(_ context.Context, _ string, _ string) (model.Session, error) {
	return f.session, nil
}
func (f *smokeRepo) ListSessions(_ context.Context, userID string, limit int) ([]model.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	if f.session.UserID != userID {
		return []model.Session{}, nil
	}
	return []model.Session{f.session}, nil
}
func (f *smokeRepo) ListMessages(_ context.Context, _ string, _ string) ([]model.Message, error) {
	return f.msgs, nil
}

type smokeCache struct{}

func (s *smokeCache) GetSummary(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (s *smokeCache) SetSummary(_ context.Context, _, _, _ string, _ time.Duration) error { return nil }
func (s *smokeCache) AppendStreamChunk(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (s *smokeCache) GetStreamChunks(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s *smokeCache) IsIdempotent(_ context.Context, _, _ string) (bool, error)     { return false, nil }
func (s *smokeCache) MarkIdempotent(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}
func (s *smokeCache) MarkDegraded(_ error) {}
func (s *smokeCache) IsDegraded() bool     { return false }

type smokeRouter struct{}

func (s *smokeRouter) Get(_ string) (service.ModelProvider, error) { return nil, nil }
func (s *smokeRouter) GenerateWithFallback(_ context.Context, _ string, _ string) (string, model.Usage, error) {
	return "smoke-ok", model.Usage{Provider: "openai"}, nil
}
func (s *smokeRouter) GenerateStreamWithFallback(_ context.Context, _ string, _ string, _ func(string) error) (string, model.Usage, error) {
	return "smoke-ok", model.Usage{Provider: "openai"}, nil
}

type smokeRAG struct{}

func (s *smokeRAG) Embed(_ context.Context, _ string) ([]float64, error) { return nil, nil }
func (s *smokeRAG) Retrieve(_ context.Context, _ string, _ string, _ string, _ int) ([]model.RAGDocument, error) {
	return nil, nil
}
func (s *smokeRAG) Rerank(_ context.Context, _ string, docs []model.RAGDocument, _ int) ([]model.RAGDocument, error) {
	return docs, nil
}
func (s *smokeRAG) KnowledgeGraphPlaceholder(_ context.Context, _ string) (string, error) {
	return "", nil
}

type smokeQueue struct{}

func (s *smokeQueue) PublishTask(_ context.Context, _ events.TaskMessage) error     { return nil }
func (s *smokeQueue) PublishResult(_ context.Context, _ events.ResultMessage) error { return nil }
func (s *smokeQueue) PublishRetryTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}
func (s *smokeQueue) PublishDLQTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}

func TestHTTPQuerySmoke(t *testing.T) {
	_ = os.Setenv("AUTH_DEV_BYPASS", "true")
	defer os.Unsetenv("AUTH_DEV_BYPASS")

	cfg := config.Load()
	logger := zap.NewNop()

	repo := &smokeRepo{}
	cache := &smokeCache{}
	sessionSvc := service.NewSessionService(repo, cache, logger)
	querySvc := service.NewQueryService(repo, sessionSvc, &smokeRouter{}, &smokeRAG{}, &smokeQueue{}, cache, nil, nil, nil, logger)
	streamSvc := service.NewStreamService(repo, sessionSvc, &smokeRouter{}, &smokeRAG{}, cache, nil, nil, nil, logger)

	router := httptransport.NewRouter(cfg, logger, nil, nil, querySvc, sessionSvc, streamSvc)
	body, _ := json.Marshal(httpcontracts.QueryRequest{
		Question: "smoke test",
		UseRAG:   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
	req.Header.Set("X-User-ID", "u1")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)
	// Public /query must fail closed until the trusted P5 Team application is
	// configured; it may not fall back to the legacy direct-model service.
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func (f *smokeCache) GetWindow(_ context.Context, _, _ string) ([]model.Message, bool, error) {
	return nil, false, nil
}
func (f *smokeCache) SetWindow(_ context.Context, _, _ string, _ []model.Message, _ time.Duration) error {
	return nil
}
