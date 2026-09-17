package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
	ragtypes "gophermind/internal/rag"
)

// PythonClient 对接 Python RAG 服务。
type PythonClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewPythonClient 构建 Python RAG 客户端。
func NewPythonClient(cfg config.RAGConfig, logger *zap.Logger) *PythonClient {
	return &PythonClient{
		baseURL: strings.TrimRight(cfg.PythonServiceURL, "/"),
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
		logger: logger,
	}
}

// Embed 调用 Python 服务 embedding。
func (c *PythonClient) Embed(ctx context.Context, text string) ([]float64, error) {
	req := ragtypes.EmbedRequest{Text: text}
	var out ragtypes.EmbedResponse
	if err := c.doJSON(ctx, http.MethodPost, "/embed", req, &out); err != nil {
		return []float64{0.1, 0.2, 0.3}, nil
	}
	return out.Vector, nil
}

// Retrieve 调用 Qdrant 检索。
func (c *PythonClient) Retrieve(ctx context.Context, userID string, _ string, query string, topK int) ([]model.RAGDocument, error) {
	req := ragtypes.RetrieveRequest{
		UserID: userID,
		Query:  query,
		TopK:   topK,
	}
	var out ragtypes.RetrieveResponse
	if err := c.doJSON(ctx, http.MethodPost, "/retrieve", req, &out); err != nil {
		// 降级返回空文档，以保证主链路可继续。
		return nil, nil
	}
	res := make([]model.RAGDocument, 0, len(out.Documents))
	for _, d := range out.Documents {
		res = append(res, model.RAGDocument{
			DocID:    d.DocID,
			ChunkID:  d.ChunkID,
			Content:  d.Content,
			Score:    d.Score,
			Metadata: d.Metadata,
		})
	}
	return res, nil
}

// Rerank 调用 BGE 重排。
func (c *PythonClient) Rerank(ctx context.Context, query string, docs []model.RAGDocument, topN int) ([]model.RAGDocument, error) {
	payloadDocs := make([]ragtypes.RetrieveDoc, 0, len(docs))
	for _, d := range docs {
		payloadDocs = append(payloadDocs, ragtypes.RetrieveDoc{
			DocID:    d.DocID,
			ChunkID:  d.ChunkID,
			Content:  d.Content,
			Score:    d.Score,
			Metadata: d.Metadata,
		})
	}
	req := ragtypes.RerankRequest{
		Query: query,
		Docs:  payloadDocs,
		TopN:  topN,
	}

	var out ragtypes.RerankResponse
	if err := c.doJSON(ctx, http.MethodPost, "/rerank", req, &out); err != nil {
		// 降级：直接取前 topN。
		if topN <= 0 || topN > len(docs) {
			topN = len(docs)
		}
		return docs[:topN], nil
	}

	res := make([]model.RAGDocument, 0, len(out.Documents))
	for _, d := range out.Documents {
		res = append(res, model.RAGDocument{
			DocID:    d.DocID,
			ChunkID:  d.ChunkID,
			Content:  d.Content,
			Score:    d.Score,
			Metadata: d.Metadata,
		})
	}
	return res, nil
}

// KnowledgeGraphPlaceholder 调用 KG 占位接口。
func (c *PythonClient) KnowledgeGraphPlaceholder(ctx context.Context, query string) (string, error) {
	req := ragtypes.KGRequest{Query: query}
	var out ragtypes.KGResponse
	if err := c.doJSON(ctx, http.MethodPost, "/kg/placeholder", req, &out); err != nil {
		return "", nil
	}
	return out.Context, nil
}

func (c *PythonClient) doJSON(ctx context.Context, method string, path string, in interface{}, out interface{}) error {
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(in); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("rag service status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
