package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
)

// MemoryService handles user-managed long-term memories.
type MemoryService struct {
	catalog MemoryCatalogRepository
	store   MemoryStore
	rag     RAGClient
	logger  *zap.Logger
}

// NewMemoryService builds MemoryService.
func NewMemoryService(catalog MemoryCatalogRepository, store MemoryStore, rag RAGClient, logger *zap.Logger) *MemoryService {
	return &MemoryService{
		catalog: catalog,
		store:   store,
		rag:     rag,
		logger:  logger,
	}
}

// Create stores one memory in the catalog and vector store.
func (s *MemoryService) Create(ctx context.Context, userID string, content string, tags []string) (model.Memory, error) {
	now := time.Now()
	memory := model.Memory{
		ID:        uuid.NewString(),
		UserID:    strings.TrimSpace(userID),
		Content:   strings.TrimSpace(content),
		Tags:      normalizeTags(tags),
		Enabled:   true,
		Source:    "manual",
		CreatedAt: now,
		UpdatedAt: now,
	}
	vector, err := s.rag.Embed(ctx, memory.Content)
	if err != nil {
		return model.Memory{}, err
	}
	if err := s.store.UpsertMemory(ctx, memory, vector); err != nil {
		return model.Memory{}, err
	}
	if err := s.catalog.Upsert(ctx, memory); err != nil {
		return model.Memory{}, err
	}
	return memory, nil
}

// List returns user memories.
func (s *MemoryService) List(ctx context.Context, userID string, limit int) ([]model.Memory, error) {
	return s.catalog.List(ctx, userID, limit)
}

// Delete removes one memory.
func (s *MemoryService) Delete(ctx context.Context, userID string, memoryID string) error {
	if err := s.store.DeleteMemory(ctx, userID, memoryID); err != nil {
		return err
	}
	return s.catalog.Delete(ctx, userID, memoryID)
}

// Search returns user memories relevant to the current question.
func (s *MemoryService) Search(ctx context.Context, userID string, query string, topK int) ([]model.Memory, error) {
	vector, err := s.rag.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.store.SearchMemories(ctx, userID, query, vector, topK)
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}
