package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	httpcontracts "gophermind/pkg/contracts/http"
)

// QueryHandler handles /query.
type QueryHandler struct {
	svc       *service.QueryService
	documents *service.DocumentService
	logger    *zap.Logger
}

// NewQueryHandler builds QueryHandler.
func NewQueryHandler(svc *service.QueryService, documents *service.DocumentService, logger *zap.Logger) *QueryHandler {
	return &QueryHandler{svc: svc, documents: documents, logger: logger}
}

// Handle executes synchronous QA.
func (h *QueryHandler) Handle(c *gin.Context) {
	var req httpcontracts.QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, httpcontracts.Err(40001, "invalid request body"))
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, httpcontracts.Err(40103, "missing user id"))
		return
	}

	if req.DocumentID != "" && h.documents != nil {
		doc, err := h.documents.Get(c.Request.Context(), userID, req.DocumentID)
		if err != nil {
			c.JSON(http.StatusNotFound, httpcontracts.Err(40431, "document not found"))
			return
		}
		if h.documents.ShouldBlockQuery(doc) {
			c.JSON(http.StatusConflict, httpcontracts.APIResponse{
				Code:    40931,
				Message: "document still indexing; use stream API to wait automatically",
				Data: gin.H{
					"document_id": req.DocumentID,
					"job_id":      doc.JobID,
					"status":      doc.Status,
				},
			})
			return
		}
	}

	out, err := h.svc.Query(c.Request.Context(), model.QueryInput{
		UserID:     userID,
		SessionID:  req.SessionID,
		DocumentID: req.DocumentID,
		Question:   req.Question,
		ModelType:  req.ModelType,
		UseRAG:     req.UseRAG,
	})
	if err != nil {
		if h.logger != nil {
			h.logger.Error("query failed", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, httpcontracts.Err(50001, "query failed"))
		return
	}

	citations := make([]httpcontracts.CitationResponse, 0, len(out.Citations))
	for _, ct := range out.Citations {
		citations = append(citations, httpcontracts.CitationResponse{
			DocID:   ct.DocID,
			ChunkID: ct.ChunkID,
			Score:   ct.Score,
		})
	}

	c.JSON(http.StatusOK, httpcontracts.OK(httpcontracts.QueryData{
		SessionID: out.SessionID,
		Answer:    out.Answer,
		Citations: citations,
		Usage: httpcontracts.UsageResponse{
			Provider:     out.Usage.Provider,
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
		},
		RequestID: out.RequestID,
	}))
}
