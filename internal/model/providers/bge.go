package providers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
)

// BGEProvider represents the shared embedding and rerank service.
type BGEProvider struct {
	baseURL    string
	modelName  string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewBGEProvider builds BGEProvider.
func NewBGEProvider(cfg config.ModelConfig, logger *zap.Logger) *BGEProvider {
	return &BGEProvider{
		baseURL:   strings.TrimRight(cfg.BGEBaseURL, "/"),
		modelName: cfg.BGEModel,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		logger: logger,
	}
}

func (p *BGEProvider) Name() string { return "bge" }

// Generate keeps the shared provider interface satisfied.
func (p *BGEProvider) Generate(_ context.Context, prompt string) (string, model.Usage, error) {
	answer := "[bge-rerank-provider] " + prompt
	return answer, model.Usage{Provider: p.Name(), InputTokens: len(prompt) / 4, OutputTokens: len(answer) / 4}, nil
}

// GenerateStream keeps the shared provider interface satisfied.
func (p *BGEProvider) GenerateStream(ctx context.Context, prompt string, onToken func(string) error) (string, model.Usage, error) {
	answer, usage, err := p.Generate(ctx, prompt)
	if err != nil {
		return "", model.Usage{}, err
	}
	for _, token := range splitToTokens(answer) {
		if err := onToken(token); err != nil {
			return "", model.Usage{}, err
		}
	}
	return answer, usage, nil
}

// EmbedText returns one embedding vector with a deterministic local fallback.
func (p *BGEProvider) EmbedText(ctx context.Context, text string) ([]float64, error) {
	vectors, err := p.EmbedTexts(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return fallbackDenseVector(text), nil
	}
	return vectors[0], nil
}

// EmbedTexts embeds a list of texts.
func (p *BGEProvider) EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	if p.baseURL != "" {
		out, err := p.callEmbed(ctx, texts)
		if err == nil && len(out) == len(texts) {
			return out, nil
		}
		if err != nil && p.logger != nil {
			p.logger.Warn("bge embed fallback", zap.Error(err))
		}
	}
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		out = append(out, fallbackDenseVector(text))
	}
	return out, nil
}

// SparseEncode builds a simple lexical sparse vector for hybrid retrieval.
func (p *BGEProvider) SparseEncode(text string) ([]uint32, []float32) {
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		return nil, nil
	}
	weights := make(map[uint32]float32, len(tokens))
	for _, token := range tokens {
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		weights[h.Sum32()%100000] += 1
	}
	indices := make([]uint32, 0, len(weights))
	values := make([]float32, 0, len(weights))
	for index, value := range weights {
		indices = append(indices, index)
		values = append(values, value)
	}
	return indices, values
}

// Rerank reranks docs with an external service when available.
func (p *BGEProvider) Rerank(ctx context.Context, query string, docs []model.RAGDocument, topN int) ([]model.RAGDocument, error) {
	if p.baseURL != "" {
		out, err := p.callRerank(ctx, query, docs, topN)
		if err == nil && len(out) > 0 {
			return out, nil
		}
		if err != nil && p.logger != nil {
			p.logger.Warn("bge rerank fallback", zap.Error(err))
		}
	}
	query = strings.ToLower(query)
	type scored struct {
		doc   model.RAGDocument
		score int
	}
	all := make([]scored, 0, len(docs))
	for _, d := range docs {
		cnt := strings.Count(strings.ToLower(d.Content), query)
		all = append(all, scored{doc: d, score: cnt})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score == all[j].score {
			return all[i].doc.Score > all[j].doc.Score
		}
		return all[i].score > all[j].score
	})
	if topN <= 0 || topN > len(all) {
		topN = len(all)
	}
	out := make([]model.RAGDocument, 0, topN)
	for i := 0; i < topN; i++ {
		d := all[i].doc
		d.Score += float64(all[i].score) * 0.01
		out = append(out, d)
	}
	return out, nil
}

func (p *BGEProvider) callEmbed(ctx context.Context, texts []string) ([][]float64, error) {
	payload := map[string]any{
		"model": p.modelName,
		"texts": texts,
	}
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embed", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Vectors [][]float64 `json:"vectors"`
		Vector  []float64   `json:"vector"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Vectors) > 0 {
		return parsed.Vectors, nil
	}
	if len(parsed.Vector) > 0 {
		return [][]float64{parsed.Vector}, nil
	}
	return nil, nil
}

func (p *BGEProvider) callRerank(ctx context.Context, query string, docs []model.RAGDocument, topN int) ([]model.RAGDocument, error) {
	payload := map[string]any{
		"model":     p.modelName,
		"query":     query,
		"top_n":     topN,
		"documents": docs,
	}
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/rerank", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Documents []model.RAGDocument `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Documents, nil
}

func fallbackDenseVector(text string) []float64 {
	vec := make([]float64, 8)
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], h.Sum64())
	for i := range vec {
		vec[i] = float64(buf[i]) / 255.0
	}
	return vec
}
