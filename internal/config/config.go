package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 汇总服务运行配置，默认值与设计文档保持一致。
type Config struct {
	ServiceName string
	HTTP        HTTPConfig
	MCP         MCPConfig
	MySQL       MySQLConfig
	Redis       RedisConfig
	RabbitMQ    RabbitMQConfig
	RocketMQ    RocketMQConfig
	Auth        AuthConfig
	Upload      UploadConfig
	Model       ModelConfig
	RAG         RAGConfig
	Pinecone    PineconeConfig
	Langfuse    LangfuseConfig
	Eval        EvalConfig
}

type HTTPConfig struct {
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type MCPConfig struct {
	Enabled       bool
	Transport     string
	DefaultUserID string
}

type MySQLConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Addrs        []string
	Username     string
	Password     string
	PoolSize     int
	MinIdleConns int
}

type RabbitMQConfig struct {
	URL         string
	TaskQueue   string
	ResultQueue string
	RetryQueue1 string
	RetryQueue2 string
	RetryQueue3 string
	DLQQueue    string
	MaxRetry    int
	RetryDelay1 time.Duration
	RetryDelay2 time.Duration
	RetryDelay3 time.Duration
}

type RocketMQConfig struct {
	Enabled         bool
	NameServers     []string
	ProducerGroup   string
	ConsumerGroup   string
	TopicDocIngest  string
	TopicMCPRemote  string
	TopicEvalJudge  string
}

type AuthConfig struct {
	AccessSecret    string
	RefreshSecret   string
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	EnableDevBypass bool
}

type UploadConfig struct {
	Dir              string
	MaxFileSizeBytes int64
	AllowedExts      []string
}

type ModelConfig struct {
	OpenAIBaseURL string
	OpenAIAPIKey  string
	OpenAIModel   string
	KimiBaseURL   string
	KimiAPIKey    string
	KimiModel     string
	QwenBaseURL   string
	QwenAPIKey    string
	QwenModel     string
	OllamaBaseURL string
	OllamaModel   string
	BGEBaseURL    string
	BGEModel      string
}

type RAGConfig struct {
	PythonServiceURL         string
	ChunkSize                int
	ChunkOverlap             int
	TopK                     int
	RerankTopN               int
	DocumentWaitReadyTimeout time.Duration
}

type PineconeConfig struct {
	APIKey           string
	DenseHost        string
	SparseHost       string
	MemoryHost       string
	DocumentTopK     int
	MemoryTopK       int
	Namespace        string
	MemoryNamespace  string
}

type LangfuseConfig struct {
	Enabled     bool
	BaseURL     string
	PublicKey   string
	SecretKey   string
	Environment string
}

type EvalConfig struct {
	Enabled        bool
	SampleRate     float64
	JudgeModelType string
}

// Load 从环境变量读取配置并提供保守默认值。
func Load() Config {
	return Config{
		ServiceName: getEnv("SERVICE_NAME", "gophermind-backend"),
		HTTP: HTTPConfig{
			Address:         getEnv("HTTP_ADDR", ":9090"),
			ReadTimeout:     getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDuration("HTTP_WRITE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		MCP: MCPConfig{
			Enabled:       getBool("MCP_ENABLED", false),
			Transport:     getEnv("MCP_TRANSPORT", "stdio"),
			DefaultUserID: getEnv("MCP_DEFAULT_USER_ID", "mcp-user"),
		},
		MySQL: MySQLConfig{
			DSN:             getEnv("MYSQL_DSN", "root:password@tcp(mysql:3306)/gophermind?charset=utf8mb4&parseTime=True&loc=Local"),
			MaxOpenConns:    getInt("MYSQL_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    getInt("MYSQL_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getDuration("MYSQL_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Redis: RedisConfig{
			Addrs:        getStringSlice("REDIS_ADDRS", []string{"redis:6379"}),
			Username:     getEnv("REDIS_USERNAME", ""),
			Password:     getEnv("REDIS_PASSWORD", ""),
			PoolSize:     getInt("REDIS_POOL_SIZE", 200),
			MinIdleConns: getInt("REDIS_MIN_IDLE_CONNS", 10),
		},
		RabbitMQ: RabbitMQConfig{
			URL:         getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
			TaskQueue:   getEnv("RABBITMQ_TASK_QUEUE", "task_queue"),
			ResultQueue: getEnv("RABBITMQ_RESULT_QUEUE", "result_queue"),
			RetryQueue1: getEnv("RABBITMQ_RETRY_QUEUE_1", "task_queue.retry.1"),
			RetryQueue2: getEnv("RABBITMQ_RETRY_QUEUE_2", "task_queue.retry.2"),
			RetryQueue3: getEnv("RABBITMQ_RETRY_QUEUE_3", "task_queue.retry.3"),
			DLQQueue:    getEnv("RABBITMQ_DLQ_QUEUE", "task_queue.dlq"),
			MaxRetry:    getInt("RABBITMQ_MAX_RETRY", 3),
			RetryDelay1: getDuration("RABBITMQ_RETRY_DELAY_1", 5*time.Second),
			RetryDelay2: getDuration("RABBITMQ_RETRY_DELAY_2", 30*time.Second),
			RetryDelay3: getDuration("RABBITMQ_RETRY_DELAY_3", 120*time.Second),
		},
		RocketMQ: RocketMQConfig{
			Enabled:        getBool("ROCKETMQ_ENABLED", false),
			NameServers:    getStringSlice("ROCKETMQ_NAME_SERVERS", []string{"127.0.0.1:9876"}),
			ProducerGroup:  getEnv("ROCKETMQ_PRODUCER_GROUP", "gophermind-producer"),
			ConsumerGroup:  getEnv("ROCKETMQ_CONSUMER_GROUP", "gophermind-consumer"),
			TopicDocIngest: getEnv("ROCKETMQ_TOPIC_DOC_INGEST", "gm.doc.ingest"),
			TopicMCPRemote: getEnv("ROCKETMQ_TOPIC_MCP_REMOTE", "gm.mcp.remote_tool"),
			TopicEvalJudge: getEnv("ROCKETMQ_TOPIC_EVAL_JUDGE", "gm.eval.judge"),
		},
		Auth: AuthConfig{
			AccessSecret:    getEnv("JWT_ACCESS_SECRET", getEnv("JWT_SECRET", "gophermind-dev-access-secret")),
			RefreshSecret:   getEnv("JWT_REFRESH_SECRET", getEnv("JWT_SECRET", "gophermind-dev-refresh-secret")),
			AccessTTL:       getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:      getDuration("JWT_REFRESH_TTL", 168*time.Hour),
			EnableDevBypass: getBool("AUTH_DEV_BYPASS", false),
		},
		Upload: UploadConfig{
			Dir:              getEnv("UPLOAD_DIR", "./data/uploads"),
			MaxFileSizeBytes: getInt64("UPLOAD_MAX_FILE_SIZE_BYTES", 20*1024*1024),
			AllowedExts: getStringSlice("UPLOAD_ALLOWED_EXTS", []string{
				".txt", ".md", ".pdf", ".doc", ".docx", ".csv", ".json", ".png", ".jpg", ".jpeg", ".webp",
			}),
		},
		Model: ModelConfig{
			OpenAIBaseURL: getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			OpenAIAPIKey:  getEnv("OPENAI_API_KEY", ""),
			OpenAIModel:   getEnv("OPENAI_MODEL", "gpt-4.1-mini"),
			KimiBaseURL:   getEnv("KIMI_BASE_URL", "https://api.moonshot.cn/v1"),
			KimiAPIKey:    getEnv("KIMI_API_KEY", ""),
			KimiModel:     getEnv("KIMI_MODEL", "moonshot-v1-8k"),
			QwenBaseURL:   getEnv("QWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			QwenAPIKey:    getEnv("QWEN_API_KEY", ""),
			QwenModel:     getEnv("QWEN_MODEL", "qwen-plus"),
			OllamaBaseURL: getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),
			OllamaModel:   getEnv("OLLAMA_MODEL", "qwen2.5:7b"),
			BGEBaseURL:    getEnv("BGE_BASE_URL", "http://localhost:8001"),
			BGEModel:      getEnv("BGE_MODEL", "bge-reranker-v2-m3"),
		},
		RAG: RAGConfig{
			PythonServiceURL:         getEnv("RAG_PYTHON_URL", "http://localhost:8000"),
			ChunkSize:                getInt("RAG_CHUNK_SIZE", 600),
			ChunkOverlap:             getInt("RAG_CHUNK_OVERLAP", 120),
			TopK:                     getInt("RAG_TOP_K", 20),
			RerankTopN:               getInt("RAG_RERANK_TOP_N", 5),
			DocumentWaitReadyTimeout: getDuration("RAG_DOCUMENT_WAIT_READY_TIMEOUT", 20*time.Second),
		},
		Pinecone: PineconeConfig{
			APIKey:          getEnv("PINECONE_API_KEY", ""),
			DenseHost:       getEnv("PINECONE_DENSE_HOST", ""),
			SparseHost:      getEnv("PINECONE_SPARSE_HOST", ""),
			MemoryHost:      getEnv("PINECONE_MEMORY_HOST", ""),
			DocumentTopK:    getInt("PINECONE_DOCUMENT_TOP_K", 20),
			MemoryTopK:      getInt("PINECONE_MEMORY_TOP_K", 5),
			Namespace:       getEnv("PINECONE_NAMESPACE", "gophermind-docs"),
			MemoryNamespace: getEnv("PINECONE_MEMORY_NAMESPACE", "gophermind-memories"),
		},
		Langfuse: LangfuseConfig{
			Enabled:     getBool("LANGFUSE_ENABLED", false),
			BaseURL:     getEnv("LANGFUSE_BASE_URL", "https://cloud.langfuse.com"),
			PublicKey:   getEnv("LANGFUSE_PUBLIC_KEY", ""),
			SecretKey:   getEnv("LANGFUSE_SECRET_KEY", ""),
			Environment: getEnv("LANGFUSE_ENVIRONMENT", "development"),
		},
		Eval: EvalConfig{
			Enabled:        getBool("EVAL_ENABLED", false),
			SampleRate:     getFloat("EVAL_SAMPLE_RATE", 0.2),
			JudgeModelType: getEnv("EVAL_JUDGE_MODEL_TYPE", "qwen"),
		},
	}
}

func getEnv(key string, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getInt64(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func getStringSlice(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	if len(res) == 0 {
		return fallback
	}
	return res
}

func getBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getFloat(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}
