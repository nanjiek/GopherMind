package langchain

import (
	"context"
	"strings"

	"github.com/tmc/langchaingo/textsplitter"
	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
	"gophermind/internal/model/providers"
	"gophermind/internal/vector/pinecone"
)

// Engine is the Go-native RAG orchestrator built around langchain-go primitives.
type Engine struct {
	cfg      config.RAGConfig
	bge      *providers.BGEProvider
	store    *pinecone.Client
	logger   *zap.Logger
	splitter textsplitter.TextSplitter
}

// NewEngine builds Engine.
func NewEngine(cfg config.RAGConfig, bge *providers.BGEProvider, store *pinecone.Client, logger *zap.Logger) *Engine {
	return &Engine{
		cfg:    cfg,
		bge:    bge,
		store:  store,
		logger: logger,
		splitter: textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(cfg.ChunkSize),
			textsplitter.WithChunkOverlap(cfg.ChunkOverlap),
		),
	}
}

// Embed implements service.RAGClient.
func (e *Engine) Embed(ctx context.Context, text string) ([]float64, error) {
	return e.bge.EmbedText(ctx, text)
}

// Retrieve implements service.RAGClient using Pinecone hybrid search.
func (e *Engine) Retrieve(ctx context.Context, userID string, documentID string, query string, topK int) ([]model.RAGDocument, error) {
	rewritten := e.Rewrite(query)
	vector, err := e.bge.EmbedText(ctx, rewritten)
	if err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = e.cfg.TopK
	}
	return e.store.HybridSearch(ctx, userID, documentID, rewritten, vector, topK)
}

// Rerank implements service.RAGClient.
func (e *Engine) Rerank(ctx context.Context, query string, docs []model.RAGDocument, topN int) ([]model.RAGDocument, error) {
	if topN <= 0 {
		topN = e.cfg.RerankTopN
	}
	return e.bge.Rerank(ctx, query, docs, topN)
}

// KnowledgeGraphPlaceholder keeps the current contract but no longer depends on Python.
func (e *Engine) KnowledgeGraphPlaceholder(_ context.Context, query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", nil
	}
	return "kg-placeholder: " + query, nil
}

// Rewrite returns a normalized retrieval query.
func (e *Engine) Rewrite(query string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

// ChunkText splits raw text into chunks using langchain-go text splitters.
func (e *Engine) ChunkText(text string) ([]string, error) {
	return e.splitter.SplitText(text)
}

// BuildChunks turns raw text into vectorized document chunks ready for Pinecone.
func (e *Engine) BuildChunks(ctx context.Context, documentID string, text string) ([]model.VectorDocumentChunk, error) {
	parts, err := e.ChunkText(text)
	if err != nil {
		return nil, err
	}
	vectors, err := e.bge.EmbedTexts(ctx, parts)
	if err != nil {
		return nil, err
	}
	out := make([]model.VectorDocumentChunk, 0, len(parts))
	for i, part := range parts {
		indices, values := e.bge.SparseEncode(part)
		out = append(out, model.VectorDocumentChunk{
			DocID:         documentID,
			ChunkID:       documentID + "-chunk-" + strings.ReplaceAll(strings.TrimSpace(strings.ToLower(strings.Join(strings.Fields(part[:min(len(part), 16)]), "-"))), " ", "-"),
			Content:       part,
			DenseVector:   vectors[i],
			SparseIndices: indices,
			SparseValues:  values,
			Metadata: map[string]string{
				"document_id": documentID,
			},
		})
	}
	return out, nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
