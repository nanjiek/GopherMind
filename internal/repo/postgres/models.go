package postgres

import "time"

// SessionModel 对应 sessions 表。
type SessionModel struct {
	ID            string         `gorm:"type:char(36);primaryKey"`
	UserID        string         `gorm:"size:64;index:idx_sessions_user_updated,priority:1;not null"`
	Title         string         `gorm:"size:255;not null"`
	ModelPref     string         `gorm:"size:32"`
	LastMessageAt *time.Time     `gorm:"index:idx_sessions_user_updated,priority:2,sort:desc"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	Messages      []MessageModel `gorm:"foreignKey:SessionID;references:ID"`
}

// MessageModel 对应 messages 表。
type MessageModel struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	SessionID string    `gorm:"type:char(36);index:idx_messages_session_time,priority:1;not null"`
	UserID    string    `gorm:"size:64;not null"`
	Role      string    `gorm:"size:16;not null"`
	Content   string    `gorm:"type:text;not null"`
	RequestID string    `gorm:"size:36;index"`
	Provider  string    `gorm:"size:64"`
	ModelName string    `gorm:"size:64"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_messages_session_time,priority:2"`
}

// UserModel 对应 users 表，用于认证。
type UserModel struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement"`
	Username     string    `gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string    `gorm:"size:255;not null"`
	Role         string    `gorm:"size:32;not null;default:user"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

// RefreshTokenModel 对应 refresh_tokens 表，保存刷新令牌哈希。
type RefreshTokenModel struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement"`
	UserID    uint64     `gorm:"index;not null"`
	TokenJTI  string     `gorm:"size:64;uniqueIndex;not null"`
	TokenHash string     `gorm:"size:128;not null"`
	DeviceID  string     `gorm:"size:128"`
	ExpiresAt time.Time  `gorm:"index;not null"`
	RevokedAt *time.Time `gorm:"index"`
	CreatedAt time.Time  `gorm:"autoCreateTime"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime"`
}

// ConsumerInboxModel 对应 consumer_inbox 表，保证消息消费幂等落库。
type ConsumerInboxModel struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"`
	Consumer    string     `gorm:"size:64;not null;uniqueIndex:uk_consumer_message,priority:1"`
	MessageID   string     `gorm:"size:128;not null;uniqueIndex:uk_consumer_message,priority:2"`
	Status      string     `gorm:"size:32;not null;index"`
	RetryCount  int        `gorm:"not null;default:0"`
	LastError   string     `gorm:"size:1024"`
	ProcessedAt *time.Time `gorm:"index"`
	CreatedAt   time.Time  `gorm:"autoCreateTime"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime"`
}

// DocumentModel stores document upload and indexing lifecycle.
type DocumentModel struct {
	ID           string    `gorm:"type:char(36);primaryKey"`
	UserID       string    `gorm:"size:64;index:idx_documents_user_status,priority:1;not null"`
	JobID        string    `gorm:"size:36;index"`
	FileKey      string    `gorm:"size:512;not null"`
	Filename     string    `gorm:"size:255;not null"`
	ContentType  string    `gorm:"size:128"`
	SizeBytes    int64     `gorm:"not null"`
	Status       string    `gorm:"size:32;index:idx_documents_user_status,priority:2;not null"`
	ErrorMessage string    `gorm:"size:1024"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

// EvalRunModel stores offline judge results for reporting.
type EvalRunModel struct {
	ID                 string    `gorm:"type:char(36);primaryKey"`
	RequestID          string    `gorm:"size:36;index;not null"`
	TraceID            string    `gorm:"size:128;index"`
	AnswerCorrectness  float64   `gorm:"not null"`
	AnswerCompleteness float64   `gorm:"not null"`
	Groundedness       float64   `gorm:"not null"`
	CitationSupport    float64   `gorm:"not null"`
	HallucinationRisk  float64   `gorm:"not null"`
	MedicalSafetyFlag  bool      `gorm:"not null"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
}

// MCPJobModel stores asynchronous remote tool job state.
type MCPJobModel struct {
	ID           string    `gorm:"type:char(36);primaryKey"`
	UserID       string    `gorm:"size:64;index;not null"`
	ToolName     string    `gorm:"size:128;not null"`
	Status       string    `gorm:"size:32;index;not null"`
	ResumeToken  string    `gorm:"size:64;uniqueIndex;not null"`
	Output       string    `gorm:"type:text"`
	ErrorMessage string    `gorm:"size:1024"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

// MemoryCatalogModel stores listable memory metadata while vectors stay in Pinecone.
type MemoryCatalogModel struct {
	ID        string    `gorm:"type:char(36);primaryKey"`
	UserID    string    `gorm:"size:64;index;not null"`
	Content   string    `gorm:"type:text;not null"`
	Tags      string    `gorm:"size:1024"`
	Enabled   bool      `gorm:"not null;default:true"`
	Source    string    `gorm:"size:32;not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (SessionModel) TableName() string { return "sessions" }

func (MessageModel) TableName() string { return "messages" }

func (UserModel) TableName() string { return "users" }

func (RefreshTokenModel) TableName() string { return "refresh_tokens" }

func (ConsumerInboxModel) TableName() string { return "consumer_inbox" }

func (DocumentModel) TableName() string { return "documents" }

func (EvalRunModel) TableName() string { return "eval_runs" }

func (MCPJobModel) TableName() string { return "mcp_jobs" }

func (MemoryCatalogModel) TableName() string { return "memory_records" }
