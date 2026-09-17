package pinecone

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
)

// Client provides a Pinecone-shaped vector store with an in-process fallback.
type Client struct {
	cfg    config.PineconeConfig
	logger *zap.Logger

	mu         sync.RWMutex
	memoryData map[string][]memoryRecord
	docData    map[string][]model.VectorDocumentChunk
}

type memoryRecord struct {
	memory model.Memory
	vector []float64
}

// NewClient builds a Pinecone client with in-memory fallback.
func NewClient(cfg config.PineconeConfig, logger *zap.Logger) *Client {
	return &Client{
		cfg:        cfg,
		logger:     logger,
		memoryData: map[string][]memoryRecord{},
		docData:    map[string][]model.VectorDocumentChunk{},
	}
}

// UpsertMemory stores one memory vector.
func (c *Client) UpsertMemory(_ context.Context, memory model.Memory, vector []float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	items := c.memoryData[memory.UserID]
	for i := range items {
		if items[i].memory.ID == memory.ID {
			items[i] = memoryRecord{memory: memory, vector: cloneVector(vector)}
			c.memoryData[memory.UserID] = items
			return nil
		}
	}
	c.memoryData[memory.UserID] = append(items, memoryRecord{memory: memory, vector: cloneVector(vector)})
	return nil
}

// DeleteMemory removes one memory vector.
func (c *Client) DeleteMemory(_ context.Context, userID string, memoryID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	items := c.memoryData[userID]
	filtered := items[:0]
	for _, item := range items {
		if item.memory.ID != memoryID {
			filtered = append(filtered, item)
		}
	}
	c.memoryData[userID] = filtered
	return nil
}

// SearchMemories searches user memories by cosine similarity.
func (c *Client) SearchMemories(_ context.Context, userID string, query string, vector []float64, topK int) ([]model.Memory, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	type scored struct {
		memory model.Memory
		score  float64
	}
	all := make([]scored, 0, len(c.memoryData[userID]))
	for _, item := range c.memoryData[userID] {
		if !item.memory.Enabled {
			continue
		}
		score := cosine(vector, item.vector)
		if strings.Contains(strings.ToLower(item.memory.Content), strings.ToLower(query)) {
			score += 0.1
		}
		all = append(all, scored{memory: item.memory, score: score})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if topK <= 0 || topK > len(all) {
		topK = len(all)
	}
	out := make([]model.Memory, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, all[i].memory)
	}
	return out, nil
}

// UpsertDocumentChunks stores document chunks for hybrid retrieval.
func (c *Client) UpsertDocumentChunks(_ context.Context, userID string, documentID string, chunks []model.VectorDocumentChunk) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := docKey(userID, documentID)
	cloned := make([]model.VectorDocumentChunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunk.DenseVector = cloneVector(chunk.DenseVector)
		chunk.SparseIndices = append([]uint32(nil), chunk.SparseIndices...)
		chunk.SparseValues = append([]float32(nil), chunk.SparseValues...)
		cloned = append(cloned, chunk)
	}
	c.docData[key] = cloned
	return nil
}

// HybridSearch searches document chunks with dense+sparse scores.
func (c *Client) HybridSearch(_ context.Context, userID string, documentID string, query string, vector []float64, topK int) ([]model.RAGDocument, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	docs := make([]model.VectorDocumentChunk, 0)
	if documentID != "" {
		docs = append(docs, c.docData[docKey(userID, documentID)]...)
	} else {
		prefix := userID + "::"
		for key, items := range c.docData {
			if strings.HasPrefix(key, prefix) {
				docs = append(docs, items...)
			}
		}
	}
	type scored struct {
		doc   model.RAGDocument
		score float64
	}
	all := make([]scored, 0, len(docs))
	queryTerms := strings.Fields(strings.ToLower(query))
	for _, item := range docs {
		score := cosine(vector, item.DenseVector)
		content := strings.ToLower(item.Content)
		for _, term := range queryTerms {
			if strings.Contains(content, term) {
				score += 0.05
			}
		}
		all = append(all, scored{
			doc: model.RAGDocument{
				DocID:    item.DocID,
				ChunkID:  item.ChunkID,
				Content:  item.Content,
				Score:    score,
				Metadata: item.Metadata,
			},
			score: score,
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if topK <= 0 || topK > len(all) {
		topK = len(all)
	}
	out := make([]model.RAGDocument, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, all[i].doc)
	}
	return out, nil
}

func cloneVector(in []float64) []float64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]float64, len(in))
	copy(out, in)
	return out
}

func docKey(userID string, documentID string) string {
	return userID + "::" + documentID
}

func cosine(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	var normA float64
	var normB float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
