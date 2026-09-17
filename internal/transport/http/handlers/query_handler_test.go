package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	"gophermind/pkg/contracts/events"
	httpcontracts "gophermind/pkg/contracts/http"
)

type handlerFakeRepo struct {
	session model.Session
	msgs    []model.Message
}

func (f *handlerFakeRepo) CreateSessionWithFirstMessage(_ context.Context, userID, title, question, requestID string) (model.Session, error) {
	f.session = model.Session{ID: "session-1", UserID: userID, Title: title}
	f.msgs = append(f.msgs, model.Message{Role: "user", Content: question, RequestID: requestID, CreatedAt: time.Now()})
	return f.session, nil
}
func (f *handlerFakeRepo) AppendUserMessage(_ context.Context, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *handlerFakeRepo) AppendAssistantMessage(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ string) error {
	return nil
}
func (f *handlerFakeRepo) GetSession(_ context.Context, _ string, _ string) (model.Session, error) {
	return f.session, nil
}
func (f *handlerFakeRepo) ListSessions(_ context.Context, userID string, limit int) ([]model.Session, error) {
	if f.session.UserID == userID {
		return []model.Session{f.session}, nil
	}
	return []model.Session{}, nil
}
func (f *handlerFakeRepo) ListMessages(_ context.Context, _ string, _ string) ([]model.Message, error) {
	return f.msgs, nil
}

type handlerFakeCache struct{}

func (h *handlerFakeCache) GetSummary(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (h *handlerFakeCache) SetSummary(_ context.Context, _, _, _ string, _ time.Duration) error {
	return nil
}
func (h *handlerFakeCache) AppendStreamChunk(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (h *handlerFakeCache) GetStreamChunks(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (h *handlerFakeCache) IsIdempotent(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (h *handlerFakeCache) MarkIdempotent(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}
func (h *handlerFakeCache) MarkDegraded(_ error) {}
func (h *handlerFakeCache) IsDegraded() bool     { return false }

type handlerFakeRouter struct{}

func (h *handlerFakeRouter) Get(_ string) (service.ModelProvider, error) { return nil, nil }
func (h *handlerFakeRouter) GenerateWithFallback(_ context.Context, _ string, _ string) (string, model.Usage, error) {
	return "ok", model.Usage{Provider: "openai", InputTokens: 10, OutputTokens: 2}, nil
}
func (h *handlerFakeRouter) GenerateStreamWithFallback(_ context.Context, _ string, _ string, _ func(string) error) (string, model.Usage, error) {
	return "ok", model.Usage{Provider: "openai", InputTokens: 10, OutputTokens: 2}, nil
}

type handlerFakeRAG struct{}

func (h *handlerFakeRAG) Embed(_ context.Context, _ string) ([]float64, error) { return nil, nil }
func (h *handlerFakeRAG) Retrieve(_ context.Context, _ string, _ string, _ int) ([]model.RAGDocument, error) {
	return nil, nil
}
func (h *handlerFakeRAG) Rerank(_ context.Context, _ string, docs []model.RAGDocument, _ int) ([]model.RAGDocument, error) {
	return docs, nil
}
func (h *handlerFakeRAG) KnowledgeGraphPlaceholder(_ context.Context, _ string) (string, error) {
	return "", nil
}

type handlerFakeQueue struct{}

func (h *handlerFakeQueue) PublishTask(_ context.Context, _ events.TaskMessage) error     { return nil }
func (h *handlerFakeQueue) PublishResult(_ context.Context, _ events.ResultMessage) error { return nil }
func (h *handlerFakeQueue) PublishRetryTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}
func (h *handlerFakeQueue) PublishDLQTask(_ context.Context, _ events.TaskMessage) error {
	return nil
}

func TestQueryHandler_Handle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &handlerFakeRepo{}
	cache := &handlerFakeCache{}
	sessionSvc := service.NewSessionService(repo, cache, nil)
	querySvc := service.NewQueryService(repo, sessionSvc, &handlerFakeRouter{}, &handlerFakeRAG{}, &handlerFakeQueue{}, cache, nil)
	handler := NewQueryHandler(querySvc, nil)

	r := gin.New()
	r.POST("/query", func(c *gin.Context) {
		c.Set("user_id", "u1")
		handler.Handle(c)
	})

	reqBody := httpcontracts.QueryRequest{Question: "hello", UseRAG: false}
	raw, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp httpcontracts.APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
}
