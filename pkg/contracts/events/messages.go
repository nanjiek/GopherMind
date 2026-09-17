package events

import "time"

const (
	// TaskQueueName 对应异步任务队列。
	TaskQueueName = "task_queue"
	// ResultQueueName 对应异步结果队列。
	ResultQueueName = "result_queue"
)

// TaskMessage 是发送到 task_queue 的消息。
type TaskMessage struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	JobID          string    `json:"job_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	UserID         string    `json:"user_id"`
	SessionID      string    `json:"session_id"`
	RequestID      string    `json:"request_id"`
	ModelType      string    `json:"model_type"`
	Question       string    `json:"question"`
	UseRAG         bool      `json:"use_rag"`
	RetryCount     int       `json:"retry_count"`
	LastError      string    `json:"last_error,omitempty"`
	TraceID        string    `json:"trace_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// ResultMessage 是发送到 result_queue 的消息。
type ResultMessage struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	JobID          string    `json:"job_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	RequestID      string    `json:"request_id"`
	SessionID      string    `json:"session_id"`
	UserID         string    `json:"user_id"`
	Status         string    `json:"status"`
	Answer         string    `json:"answer"`
	Error          string    `json:"error"`
	Provider       string    `json:"provider"`
	TraceID        string    `json:"trace_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// DocumentIngestMessage describes a document indexing job.
type DocumentIngestMessage struct {
	EventType string    `json:"event_type"`
	Version   string    `json:"version"`
	JobID     string    `json:"job_id"`
	UserID    string    `json:"user_id"`
	DocumentID string   `json:"document_id"`
	FileKey   string    `json:"file_key"`
	Filename  string    `json:"filename"`
	TraceID   string    `json:"trace_id"`
	CreatedAt time.Time `json:"created_at"`
}

// JudgeMessage describes an offline evaluation task.
type JudgeMessage struct {
	EventType  string    `json:"event_type"`
	Version    string    `json:"version"`
	JobID      string    `json:"job_id"`
	RequestID  string    `json:"request_id"`
	TraceID    string    `json:"trace_id"`
	UserID     string    `json:"user_id"`
	SessionID  string    `json:"session_id"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	ModelType  string    `json:"model_type"`
	Citations  []string  `json:"citations,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// MCPToolMessage describes an asynchronous MCP remote tool job.
type MCPToolMessage struct {
	EventType   string         `json:"event_type"`
	Version     string         `json:"version"`
	JobID       string         `json:"job_id"`
	UserID      string         `json:"user_id"`
	ToolName    string         `json:"tool_name"`
	ResumeToken string         `json:"resume_token"`
	Payload     map[string]any `json:"payload"`
	TraceID     string         `json:"trace_id"`
	CreatedAt   time.Time      `json:"created_at"`
}
