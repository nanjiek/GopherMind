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

	"gophermind/internal/agent/runtime"
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
func (h *handlerFakeRAG) Retrieve(_ context.Context, _ string, _ string, _ string, _ int) ([]model.RAGDocument, error) {
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
	_ = service.NewQueryService(repo, sessionSvc, &handlerFakeRouter{}, &handlerFakeRAG{}, &handlerFakeQueue{}, cache, nil, nil, nil, nil)
	handler := NewQueryHandler(nil, "", 0, nil, nil)

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

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp httpcontracts.APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 50321, resp.Code)
}

type handlerCommittedReader struct {
	response service.CommittedTeamResponse
	scope    runtime.Metadata
	runID    string
}

func (r *handlerCommittedReader) LoadCommittedResponse(_ context.Context, scope runtime.Metadata, runID string) (service.CommittedTeamResponse, int64, error) {
	r.scope, r.runID = scope, runID
	return r.response, 1, nil
}

func TestCommittedResponseHandler_ReplaysOnlyScopedCommittedData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &handlerCommittedReader{response: service.CommittedTeamResponse{RunID: "8ba75bdd-8306-4ee0-8e1d-b1303232687f", Data: json.RawMessage(`{"answer":"reviewed"}`)}}
	handler := NewCommittedResponseHandler(&service.TeamQueryApplication{Reader: reader}, "tenant-1", nil)
	r := gin.New()
	r.GET("/query/:run/replay", func(c *gin.Context) {
		c.Set("user_id", "u1")
		handler.Handle(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/query/8ba75bdd-8306-4ee0-8e1d-b1303232687f/replay?session_id=s1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, runtime.Metadata{TenantID: "tenant-1", UserID: "u1", SessionID: "s1"}, reader.scope)
	require.Equal(t, "8ba75bdd-8306-4ee0-8e1d-b1303232687f", reader.runID)
	var response struct {
		Data struct {
			Response struct {
				Answer string `json:"answer"`
			} `json:"response"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "reviewed", response.Data.Response.Answer)
}

func (f *handlerFakeCache) GetWindow(_ context.Context, _, _ string) ([]model.Message, bool, error) {
	return nil, false, nil
}
func (f *handlerFakeCache) SetWindow(_ context.Context, _, _ string, _ []model.Message, _ time.Duration) error {
	return nil
}
