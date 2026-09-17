package rocketmq

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/pkg/contracts/events"
)

// Dispatcher dispatches asynchronous jobs. It currently falls back to in-process handlers when no broker is configured.
type Dispatcher struct {
	cfg    config.RocketMQConfig
	logger *zap.Logger

	mu          sync.RWMutex
	docHandler  func(context.Context, events.DocumentIngestMessage) error
	judgeHandler func(context.Context, events.JudgeMessage) error
	mcpHandler  func(context.Context, events.MCPToolMessage) error
}

// NewDispatcher builds Dispatcher.
func NewDispatcher(cfg config.RocketMQConfig, logger *zap.Logger) *Dispatcher {
	return &Dispatcher{cfg: cfg, logger: logger}
}

// RegisterDocumentHandler registers the document worker callback.
func (d *Dispatcher) RegisterDocumentHandler(fn func(context.Context, events.DocumentIngestMessage) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docHandler = fn
}

// RegisterJudgeHandler registers the judge worker callback.
func (d *Dispatcher) RegisterJudgeHandler(fn func(context.Context, events.JudgeMessage) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.judgeHandler = fn
}

// RegisterMCPHandler registers the remote tool worker callback.
func (d *Dispatcher) RegisterMCPHandler(fn func(context.Context, events.MCPToolMessage) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mcpHandler = fn
}

// DispatchDocumentIngest dispatches one document ingest task.
func (d *Dispatcher) DispatchDocumentIngest(ctx context.Context, message events.DocumentIngestMessage) error {
	d.mu.RLock()
	handler := d.docHandler
	d.mu.RUnlock()
	if handler == nil {
		return nil
	}
	go func() { _ = handler(context.Background(), message) }()
	if d.logger != nil {
		d.logger.Info("dispatch document ingest", zap.String("job_id", message.JobID), zap.String("document_id", message.DocumentID))
	}
	return nil
}

// DispatchJudge dispatches one offline judge task.
func (d *Dispatcher) DispatchJudge(ctx context.Context, message events.JudgeMessage) error {
	d.mu.RLock()
	handler := d.judgeHandler
	d.mu.RUnlock()
	if handler == nil {
		return nil
	}
	go func() { _ = handler(context.Background(), message) }()
	if d.logger != nil {
		d.logger.Info("dispatch judge", zap.String("job_id", message.JobID), zap.String("request_id", message.RequestID))
	}
	return nil
}

// DispatchMCPTool dispatches one asynchronous tool task.
func (d *Dispatcher) DispatchMCPTool(ctx context.Context, message events.MCPToolMessage) error {
	d.mu.RLock()
	handler := d.mcpHandler
	d.mu.RUnlock()
	if handler == nil {
		return nil
	}
	go func() { _ = handler(context.Background(), message) }()
	if d.logger != nil {
		d.logger.Info("dispatch mcp tool", zap.String("job_id", message.JobID), zap.String("tool_name", message.ToolName))
	}
	return nil
}
