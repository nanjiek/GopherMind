package service

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/rag/langchain"
	"gophermind/pkg/contracts/events"
)

// DocumentService handles upload, status lookup, and async ingestion orchestration.
type DocumentService struct {
	attachments *AttachmentService
	repo        DocumentRepository
	dispatcher  AsyncDispatcher
	engine      *langchain.Engine
	store       DocumentVectorStore
	logger      *zap.Logger
}

// NewDocumentService builds DocumentService.
func NewDocumentService(attachments *AttachmentService, repo DocumentRepository, dispatcher AsyncDispatcher, engine *langchain.Engine, store DocumentVectorStore, logger *zap.Logger) *DocumentService {
	return &DocumentService{
		attachments: attachments,
		repo:        repo,
		dispatcher:  dispatcher,
		engine:      engine,
		store:       store,
		logger:      logger,
	}
}

// Upload stores the file and dispatches indexing.
func (s *DocumentService) Upload(ctx context.Context, userID string, originalName string, file io.Reader, declaredSize int64, traceID string) (model.Document, error) {
	if s.attachments == nil {
		return model.Document{}, errors.New("attachment service unavailable")
	}
	uploaded, err := s.attachments.Upload(ctx, userID, originalName, file, declaredSize)
	if err != nil {
		return model.Document{}, err
	}
	now := time.Now()
	doc := model.Document{
		ID:          uuid.NewString(),
		UserID:      userID,
		JobID:       uuid.NewString(),
		FileKey:     uploaded.FileKey,
		Filename:    uploaded.OriginalName,
		ContentType: uploaded.ContentType,
		SizeBytes:   uploaded.SizeBytes,
		Status:      "indexing",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return model.Document{}, err
	}
	_ = s.dispatcher.DispatchDocumentIngest(ctx, events.DocumentIngestMessage{
		EventType:  "doc.ingest.request",
		Version:    "v1",
		JobID:      doc.JobID,
		UserID:     doc.UserID,
		DocumentID: doc.ID,
		FileKey:    doc.FileKey,
		Filename:   doc.Filename,
		TraceID:    traceID,
		CreatedAt:  now,
	})
	return doc, nil
}

// Get returns one document by user/id.
func (s *DocumentService) Get(ctx context.Context, userID string, documentID string) (model.Document, error) {
	return s.repo.Get(ctx, userID, documentID)
}

// WaitUntilReady waits until the document is ready or failed.
func (s *DocumentService) WaitUntilReady(ctx context.Context, userID string, documentID string, timeout time.Duration) (model.Document, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		doc, err := s.repo.Get(ctx, userID, documentID)
		if err != nil {
			return model.Document{}, err
		}
		switch doc.Status {
		case "ready", "failed":
			return doc, nil
		}
		select {
		case <-ctx.Done():
			return model.Document{}, ctx.Err()
		case <-deadline.C:
			return doc, context.DeadlineExceeded
		case <-ticker.C:
		}
	}
}

// HandleIngestMessage is the document indexing worker.
func (s *DocumentService) HandleIngestMessage(ctx context.Context, message events.DocumentIngestMessage) error {
	doc, err := s.repo.GetByID(ctx, message.DocumentID)
	if err != nil {
		return err
	}
	path, err := s.attachments.ResolveFilePath(doc.UserID, doc.FileKey)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, doc.ID, "failed", err.Error())
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, doc.ID, "failed", err.Error())
		return err
	}
	chunks, err := s.engine.BuildChunks(ctx, doc.ID, string(content))
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, doc.ID, "failed", err.Error())
		return err
	}
	for i := range chunks {
		if chunks[i].Metadata == nil {
			chunks[i].Metadata = map[string]string{}
		}
		chunks[i].Metadata["filename"] = doc.Filename
		chunks[i].Metadata["user_id"] = doc.UserID
	}
	if err := s.store.UpsertDocumentChunks(ctx, doc.UserID, doc.ID, chunks); err != nil {
		_ = s.repo.UpdateStatus(ctx, doc.ID, "failed", err.Error())
		return err
	}
	return s.repo.UpdateStatus(ctx, doc.ID, "ready", "")
}

// ShouldBlockQuery returns whether the caller must wait for indexing.
func (s *DocumentService) ShouldBlockQuery(doc model.Document) bool {
	return strings.TrimSpace(doc.Status) == "indexing" || strings.TrimSpace(doc.Status) == "uploaded"
}
