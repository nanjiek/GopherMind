package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	httpcontracts "gophermind/pkg/contracts/http"
)

// StreamHandler handles /stream/:session and negotiates SSE/WebSocket.
type StreamHandler struct {
	svc         *service.StreamService
	documents   *service.DocumentService
	waitTimeout time.Duration
	logger      *zap.Logger
	upgrader    websocket.Upgrader
}

// NewStreamHandler builds StreamHandler.
func NewStreamHandler(svc *service.StreamService, documents *service.DocumentService, waitTimeout time.Duration, logger *zap.Logger) *StreamHandler {
	return &StreamHandler{
		svc:         svc,
		documents:   documents,
		waitTimeout: waitTimeout,
		logger:      logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Handle dispatches SSE or WebSocket streaming.
func (h *StreamHandler) Handle(c *gin.Context) {
	if websocket.IsWebSocketUpgrade(c.Request) {
		h.handleWS(c)
		return
	}
	h.handleSSE(c)
}

func (h *StreamHandler) handleSSE(c *gin.Context) {
	userID := c.GetString("user_id")
	sessionID := c.Param("session")
	documentID := c.Query("document_id")
	question := c.Query("q")
	modelType := c.DefaultQuery("model_type", "auto")
	useRAG := parseBool(c.DefaultQuery("use_rag", "false"))

	if strings.TrimSpace(question) == "" {
		c.JSON(http.StatusBadRequest, httpcontracts.Err(40003, "missing query parameter q"))
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()

	emit := func(event string, payload interface{}) error {
		b, _ := json.Marshal(payload)
		_, err := c.Writer.WriteString(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(b)))
		if err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}

	if blocked, jobID, err := h.waitForDocumentReady(c.Request.Context(), userID, documentID); err != nil {
		_ = emit("error", gin.H{"code": "document_index_failed", "message": err.Error()})
		return
	} else if blocked {
		_ = emit("status", gin.H{"phase": "waiting_document", "document_id": documentID, "job_id": jobID})
	}

	out, err := h.svc.Stream(c.Request.Context(), model.QueryInput{
		UserID:     userID,
		SessionID:  sessionID,
		DocumentID: documentID,
		Question:   question,
		ModelType:  modelType,
		UseRAG:     useRAG,
	}, func(token string) error {
		return emit("token", gin.H{"delta": token})
	})
	if err != nil {
		code := "query_failed"
		if err == contextDeadlineExceeded {
			code = "document_index_timeout"
		}
		_ = emit("error", gin.H{"code": code, "message": err.Error()})
		return
	}

	_ = emit("done", gin.H{
		"request_id": out.RequestID,
		"session_id": out.SessionID,
		"usage": gin.H{
			"provider":      out.Usage.Provider,
			"input_tokens":  out.Usage.InputTokens,
			"output_tokens": out.Usage.OutputTokens,
		},
		"citations": out.Citations,
	})
}

func (h *StreamHandler) handleWS(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	userID := c.GetString("user_id")
	sessionID := c.Param("session")
	documentID := c.Query("document_id")
	question := c.Query("q")
	modelType := c.DefaultQuery("model_type", "auto")
	useRAG := parseBool(c.DefaultQuery("use_rag", "false"))
	if strings.TrimSpace(question) == "" {
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "missing query parameter q"})
		return
	}

	if blocked, jobID, err := h.waitForDocumentReady(c.Request.Context(), userID, documentID); err != nil {
		_ = conn.WriteJSON(gin.H{"type": "error", "code": "document_index_failed", "message": err.Error()})
		return
	} else if blocked {
		_ = conn.WriteJSON(gin.H{"type": "status", "phase": "waiting_document", "document_id": documentID, "job_id": jobID})
	}

	seq := 0
	out, err := h.svc.Stream(c.Request.Context(), model.QueryInput{
		UserID:     userID,
		SessionID:  sessionID,
		DocumentID: documentID,
		Question:   question,
		ModelType:  modelType,
		UseRAG:     useRAG,
	}, func(token string) error {
		seq++
		return conn.WriteJSON(gin.H{
			"type":  "token",
			"delta": token,
			"seq":   seq,
		})
	})
	if err != nil {
		_ = conn.WriteJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}
	_ = conn.WriteJSON(gin.H{
		"type":       "done",
		"request_id": out.RequestID,
		"session_id": out.SessionID,
		"usage": gin.H{
			"provider":      out.Usage.Provider,
			"input_tokens":  out.Usage.InputTokens,
			"output_tokens": out.Usage.OutputTokens,
		},
		"citations": out.Citations,
	})
}

var contextDeadlineExceeded = fmt.Errorf("document wait timeout")

func (h *StreamHandler) waitForDocumentReady(ctx context.Context, userID string, documentID string) (bool, string, error) {
	if documentID == "" || h.documents == nil {
		return false, "", nil
	}
	doc, err := h.documents.Get(ctx, userID, documentID)
	if err != nil {
		return false, "", err
	}
	if !h.documents.ShouldBlockQuery(doc) {
		return false, doc.JobID, nil
	}
	ready, err := h.documents.WaitUntilReady(ctx, userID, documentID, h.waitTimeout)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("wait document ready failed", zap.Error(err))
		}
		return true, doc.JobID, contextDeadlineExceeded
	}
	if ready.Status == "failed" {
		return true, ready.JobID, errors.New(ready.ErrorMessage)
	}
	return true, ready.JobID, nil
}

func parseBool(v string) bool {
	ok, _ := strconv.ParseBool(v)
	return ok
}
