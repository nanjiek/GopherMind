package model

import "time"

// Session 表示用户会话。
type Session struct {
	ID            string
	UserID        string
	Title         string
	ModelPref     string
	LastMessageAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Message 表示会话消息。
type Message struct {
	ID        int64
	SessionID string
	UserID    string
	Role      string
	Content   string
	RequestID string
	Provider  string
	ModelName string
	CreatedAt time.Time
}

// Citation 对应 RAG 引用片段。
type Citation struct {
	DocID   string
	ChunkID string
	Score   float64
}

// Usage 描述 token 使用。
type Usage struct {
	Provider     string
	InputTokens  int
	OutputTokens int
}

// QueryInput 是查询入口参数。
type QueryInput struct {
	UserID    string
	SessionID string
	DocumentID string
	Question  string
	ModelType string
	UseRAG    bool
}

// QueryOutput 是查询统一返回结构。
type QueryOutput struct {
	RequestID string
	SessionID string
	Answer    string
	Citations []Citation
	Usage     Usage
}

// RAGDocument 表示检索到的文档块。
type RAGDocument struct {
	DocID    string
	ChunkID  string
	Content  string
	Score    float64
	Metadata map[string]string
}

// VectorDocumentChunk stores one chunk with its dense and sparse representations.
type VectorDocumentChunk struct {
	DocID         string
	ChunkID       string
	Content       string
	DenseVector   []float64
	SparseIndices []uint32
	SparseValues  []float32
	Metadata      map[string]string
}

// AuthUser 是认证域用户模型。
type AuthUser struct {
	ID           uint64
	Username     string
	PasswordHash string
	Role         string
}

// RefreshTokenRecord 是刷新令牌落库模型。
type RefreshTokenRecord struct {
	UserID    uint64
	TokenJTI  string
	TokenHash string
	DeviceID  string
	ExpiresAt time.Time
}

// Memory represents a user-managed long-term memory item.
type Memory struct {
	ID        string
	UserID    string
	Content   string
	Tags      []string
	Enabled   bool
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Document represents an uploaded RAG document and indexing status.
type Document struct {
	ID           string
	UserID       string
	JobID        string
	FileKey      string
	Filename     string
	ContentType  string
	SizeBytes    int64
	Status       string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EvalRun stores an offline LLM-as-a-Judge result snapshot.
type EvalRun struct {
	ID                 string
	RequestID          string
	TraceID            string
	AnswerCorrectness  float64
	AnswerCompleteness float64
	Groundedness       float64
	CitationSupport    float64
	HallucinationRisk  float64
	MedicalSafetyFlag  bool
	CreatedAt          time.Time
}

// AsyncToolJob describes an MCP remote tool execution job.
type AsyncToolJob struct {
	ID          string
	UserID      string
	ToolName    string
	Status      string
	ResumeToken string
	Output      string
	ErrorMessage string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
