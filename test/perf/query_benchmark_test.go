package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	httptransport "gophermind/internal/transport/http"
	"gophermind/pkg/contracts/events"
	httpcontracts "gophermind/pkg/contracts/http"
)

type benchRepo struct {
	session model.Session
	msgs    []model.Message
}

func (f *benchRepo) CreateSessionWithFirstMessage(_ context.Context, userID, title, question, requestID string) (model.Session, error) {
	f.session = model.Session{ID: "session-bench", UserID: userID, Title: title}
	f.msgs = append(f.msgs, model.Message{Role: "user", Content: question, RequestID: requestID, CreatedAt: time.Now()})
	return f.session, nil
}
func (f *benchRepo) AppendUserMessage(_ context.Context, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *benchRepo) AppendAssistantMessage(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *benchRepo) GetSession(_ context.Context, _ string, _ string) (model.Session, error) {
	return f.session, nil
}
func (f *benchRepo) ListSessions(_ context.Context, userID string, limit int) ([]model.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	if f.session.UserID != userID {
		return []model.Session{}, nil
	}
	return []model.Session{f.session}, nil
}
func (f *benchRepo) ListMessages(_ context.Context, _ string, _ string) ([]model.Message, error) {
	return f.msgs, nil
}

type benchCache struct{}

func (s *benchCache) GetSummary(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (s *benchCache) SetSummary(_ context.Context, _, _, _ string, _ time.Duration) error { return nil }
func (s *benchCache) AppendStreamChunk(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (s *benchCache) GetStreamChunks(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s *benchCache) IsIdempotent(_ context.Context, _, _ string) (bool, error)     { return false, nil }
func (s *benchCache) MarkIdempotent(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}
func (s *benchCache) MarkDegraded(_ error) {}
func (s *benchCache) IsDegraded() bool     { return false }

type benchRouter struct{}

func (s *benchRouter) Get(_ string) (service.ModelProvider, error) { return nil, nil }
func (s *benchRouter) GenerateWithFallback(_ context.Context, _ string, _ string) (string, model.Usage, error) {
	return "bench-ok", model.Usage{Provider: "openai"}, nil
}
func (s *benchRouter) GenerateStreamWithFallback(_ context.Context, _ string, _ string, _ func(string) error) (string, model.Usage, error) {
	return "bench-ok", model.Usage{Provider: "openai"}, nil
}

type benchRAG struct{}

func (s *benchRAG) Embed(_ context.Context, _ string) ([]float64, error) { return nil, nil }
func (s *benchRAG) Retrieve(_ context.Context, _ string, _ string, _ int) ([]model.RAGDocument, error) {
	return nil, nil
}
func (s *benchRAG) Rerank(_ context.Context, _ string, docs []model.RAGDocument, _ int) ([]model.RAGDocument, error) {
	return docs, nil
}
func (s *benchRAG) KnowledgeGraphPlaceholder(_ context.Context, _ string) (string, error) {
	return "", nil
}

type benchQueue struct{}

func (s *benchQueue) PublishTask(_ context.Context, _ events.TaskMessage) error     { return nil }
func (s *benchQueue) PublishResult(_ context.Context, _ events.ResultMessage) error { return nil }
func (s *benchQueue) PublishRetryTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}
func (s *benchQueue) PublishDLQTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}

func BenchmarkQueryEndpoint(b *testing.B) {
	_ = os.Setenv("AUTH_DEV_BYPASS", "true")
	defer os.Unsetenv("AUTH_DEV_BYPASS")

	cfg := config.Load()
	logger := zap.NewNop()

	repo := &benchRepo{}
	cache := &benchCache{}
	sessionSvc := service.NewSessionService(repo, cache, logger)
	querySvc := service.NewQueryService(repo, sessionSvc, &benchRouter{}, &benchRAG{}, &benchQueue{}, cache, logger)
	streamSvc := service.NewStreamService(repo, sessionSvc, &benchRouter{}, &benchRAG{}, cache, logger)
	router := httptransport.NewRouter(cfg, logger, nil, nil, querySvc, sessionSvc, streamSvc)

	payload, _ := json.Marshal(httpcontracts.QueryRequest{
		Question: "benchmark question",
		UseRAG:   false,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "bench-user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status code: %d", w.Code)
		}
	}
}
