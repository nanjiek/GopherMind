package httpcontracts

// QueryRequest 对应 POST /query。
type QueryRequest struct {
	SessionID string `json:"session_id,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	Question  string `json:"question" binding:"required"`
	ModelType string `json:"model_type,omitempty"`
	UseRAG    bool   `json:"use_rag"`
}

// CitationResponse 描述 RAG 引用信息。
type CitationResponse struct {
	DocID   string  `json:"doc_id"`
	ChunkID string  `json:"chunk_id"`
	Score   float64 `json:"score"`
}

// UsageResponse 描述模型调用计量。
type UsageResponse struct {
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// QueryData 是 query 的 data 字段。
type QueryData struct {
	SessionID string             `json:"session_id"`
	Answer    string             `json:"answer"`
	Citations []CitationResponse `json:"citations,omitempty"`
	Usage     UsageResponse      `json:"usage"`
	RequestID string             `json:"request_id"`
}
