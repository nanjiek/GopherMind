package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gophermind/internal/core/service"
	httpcontracts "gophermind/pkg/contracts/http"
)

// QueryHandler handles /query.
type QueryHandler struct {
	team      *service.TeamQueryApplication
	tenantID  string
	deadline  time.Duration
	documents *service.DocumentService
	logger    *zap.Logger
}

// NewQueryHandler builds QueryHandler.
func NewQueryHandler(team *service.TeamQueryApplication, tenantID string, deadline time.Duration, documents *service.DocumentService, logger *zap.Logger) *QueryHandler {
	return &QueryHandler{team: team, tenantID: tenantID, deadline: deadline, documents: documents, logger: logger}
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
	if h.team == nil || h.tenantID == "" {
		c.JSON(http.StatusServiceUnavailable, httpcontracts.Err(50321, "trusted team query path unavailable"))
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

	deadline := h.deadline
	if deadline <= 0 {
		deadline = 30 * time.Second
	}
	out, err := h.team.Execute(c.Request.Context(), service.TrustedQueryPolicyInput{Identity: service.TrustedQueryIdentity{TenantID: h.tenantID, UserID: userID, SessionID: req.SessionID}, RunID: c.GetString("request_id"), Deadline: time.Now().Add(deadline), Question: req.Question, DocumentID: req.DocumentID})
	if err != nil {
		if h.logger != nil {
			h.logger.Error("query failed", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, httpcontracts.Err(50001, "query failed"))
		return
	}

	if out.RequiresHuman {
		c.JSON(http.StatusAccepted, httpcontracts.APIResponse{Code: 20231, Message: "human review required", Data: gin.H{"requires_human": true, "request_id": c.GetString("request_id")}})
		return
	}
	var response struct {
		Answer string `json:"answer"`
	}
	if json.Unmarshal(out.Data, &response) != nil || response.Answer == "" {
		c.JSON(http.StatusInternalServerError, httpcontracts.Err(50001, "committed response invalid"))
		return
	}

	c.JSON(http.StatusOK, httpcontracts.OK(httpcontracts.QueryData{
		SessionID: req.SessionID,
		Answer:    response.Answer,
		RequestID: c.GetString("request_id"),
	}))
}
