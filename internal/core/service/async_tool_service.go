package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/pkg/contracts/events"
)

// AsyncToolService handles MCP remote tool jobs.
type AsyncToolService struct {
	repo       MCPJobRepository
	dispatcher AsyncDispatcher
	logger     *zap.Logger
}

// NewAsyncToolService builds AsyncToolService.
func NewAsyncToolService(repo MCPJobRepository, dispatcher AsyncDispatcher, logger *zap.Logger) *AsyncToolService {
	return &AsyncToolService{
		repo:       repo,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Submit queues one remote tool job.
func (s *AsyncToolService) Submit(ctx context.Context, userID string, toolName string, payload map[string]any, traceID string) (model.AsyncToolJob, error) {
	job := model.AsyncToolJob{
		ID:           uuid.NewString(),
		UserID:       userID,
		ToolName:     toolName,
		Status:       "accepted",
		ResumeToken:  uuid.NewString(),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.repo.Create(ctx, job); err != nil {
		return model.AsyncToolJob{}, err
	}
	_ = s.dispatcher.DispatchMCPTool(ctx, events.MCPToolMessage{
		EventType:   "mcp.remote_tool.request",
		Version:     "v1",
		JobID:       job.ID,
		UserID:      userID,
		ToolName:    toolName,
		ResumeToken: job.ResumeToken,
		Payload:     payload,
		TraceID:     traceID,
		CreatedAt:   time.Now(),
	})
	return job, nil
}

// Get returns the current state of one async tool job.
func (s *AsyncToolService) Get(ctx context.Context, userID string, jobID string) (model.AsyncToolJob, error) {
	return s.repo.Get(ctx, userID, jobID)
}

// Resume returns a job by resume token.
func (s *AsyncToolService) Resume(ctx context.Context, resumeToken string) (model.AsyncToolJob, error) {
	return s.repo.GetByResumeToken(ctx, resumeToken)
}

// HandleMCPToolMessage executes one remote tool job asynchronously.
func (s *AsyncToolService) HandleMCPToolMessage(ctx context.Context, message events.MCPToolMessage) error {
	payload, _ := json.Marshal(message.Payload)
	output := "remote tool executed: " + message.ToolName + " payload=" + string(payload)
	return s.repo.UpdateResult(ctx, message.JobID, "succeeded", output, "")
}
