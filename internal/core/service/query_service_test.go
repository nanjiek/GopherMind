package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gophermind/internal/core/model"
	"gophermind/pkg/contracts/events"
)

type fakeRepo struct {
	sessions map[string]model.Session
	msgs     map[string][]model.Message
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		sessions: map[string]model.Session{},
		msgs:     map[string][]model.Message{},
	}
}

func (f *fakeRepo) CreateSessionWithFirstMessage(_ context.Context, userID, title, question, requestID string) (model.Session, error) {
	s := model.Session{ID: "s1", UserID: userID, Title: title, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.sessions[s.ID] = s
	f.msgs[s.ID] = append(f.msgs[s.ID], model.Message{Role: "user", Content: question, RequestID: requestID, CreatedAt: time.Now()})
	return s, nil
}

func (f *fakeRepo) AppendUserMessage(_ context.Context, _ string, sessionID string, question string, requestID string) error {
	f.msgs[sessionID] = append(f.msgs[sessionID], model.Message{Role: "user", Content: question, RequestID: requestID, CreatedAt: time.Now()})
	return nil
}

func (f *fakeRepo) AppendAssistantMessage(_ context.Context, _ string, sessionID string, answer string, requestID string, provider string, modelName string) error {
	f.msgs[sessionID] = append(f.msgs[sessionID], model.Message{Role: "assistant", Content: answer, RequestID: requestID, Provider: provider, ModelName: modelName, CreatedAt: time.Now()})
	return nil
}

func (f *fakeRepo) GetSession(_ context.Context, _ string, sessionID string) (model.Session, error) {
	return f.sessions[sessionID], nil
}

func (f *fakeRepo) ListSessions(_ context.Context, userID string, limit int) ([]model.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	out := make([]model.Session, 0, limit)
	for _, session := range f.sessions {
		if session.UserID == userID {
			out = append(out, session)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) ListMessages(_ context.Context, _ string, sessionID string) ([]model.Message, error) {
	return f.msgs[sessionID], nil
}

type fakeCache struct {
	summaries map[string]string
}

func newFakeCache() *fakeCache {
	return &fakeCache{summaries: map[string]string{}}
}

func (f *fakeCache) GetSummary(_ context.Context, userID, sessionID string) (string, bool, error) {
	v, ok := f.summaries[userID+":"+sessionID]
	return v, ok, nil
}
func (f *fakeCache) SetSummary(_ context.Context, userID, sessionID, summary string, _ time.Duration) error {
	f.summaries[userID+":"+sessionID] = summary
	return nil
}
func (f *fakeCache) AppendStreamChunk(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (f *fakeCache) GetStreamChunks(_ context.Context, _ string) ([]string, error)        { return nil, nil }
func (f *fakeCache) IsIdempotent(_ context.Context, _, _ string) (bool, error)            { return false, nil }
func (f *fakeCache) MarkIdempotent(_ context.Context, _, _ string, _ time.Duration) error { return nil }
func (f *fakeCache) MarkDegraded(_ error)                                                 {}
func (f *fakeCache) IsDegraded() bool                                                     { return false }

type fakeRouter struct{}

func (f *fakeRouter) Get(_ string) (ModelProvider, error) { return nil, nil }
func (f *fakeRouter) GenerateWithFallback(_ context.Context, _ string, _ string) (string, model.Usage, error) {
	return "hello", model.Usage{Provider: "openai", InputTokens: 10, OutputTokens: 5}, nil
}
func (f *fakeRouter) GenerateStreamWithFallback(_ context.Context, _ string, _ string, _ func(string) error) (string, model.Usage, error) {
	return "hello", model.Usage{Provider: "openai", InputTokens: 10, OutputTokens: 5}, nil
}

type fakeRAG struct{}

func (f *fakeRAG) Embed(_ context.Context, _ string) ([]float64, error) { return nil, nil }
func (f *fakeRAG) Retrieve(_ context.Context, _ string, _ string, _ int) ([]model.RAGDocument, error) {
	return []model.RAGDocument{
		{DocID: "d1", ChunkID: "c1", Content: "context", Score: 0.9},
	}, nil
}
func (f *fakeRAG) Rerank(_ context.Context, _ string, docs []model.RAGDocument, _ int) ([]model.RAGDocument, error) {
	return docs, nil
}
func (f *fakeRAG) KnowledgeGraphPlaceholder(_ context.Context, _ string) (string, error) {
	return "kg", nil
}

type fakeQueue struct {
	taskCount   int
	resultCount int
}

func (f *fakeQueue) PublishTask(_ context.Context, _ events.TaskMessage) error {
	f.taskCount++
	return nil
}
func (f *fakeQueue) PublishResult(_ context.Context, _ events.ResultMessage) error {
	f.resultCount++
	return nil
}
func (f *fakeQueue) PublishRetryTask(_ context.Context, _ events.TaskMessage) error { return nil }
func (f *fakeQueue) PublishDLQTask(_ context.Context, _ events.TaskMessage) error   { return nil }

func TestQueryService_Query(t *testing.T) {
	repo := newFakeRepo()
	cache := newFakeCache()
	sessionSvc := NewSessionService(repo, cache, nil)
	queue := &fakeQueue{}
	svc := NewQueryService(repo, sessionSvc, &fakeRouter{}, &fakeRAG{}, queue, cache, nil)

	out, err := svc.Query(context.Background(), model.QueryInput{
		UserID:    "u1",
		SessionID: "",
		Question:  "what is gophermind",
		ModelType: "auto",
		UseRAG:    true,
	})
	require.NoError(t, err)
	require.Equal(t, "s1", out.SessionID)
	require.Equal(t, "hello", out.Answer)
	require.Len(t, out.Citations, 1)
	require.Equal(t, 1, queue.taskCount)
	require.Equal(t, 1, queue.resultCount)
}
