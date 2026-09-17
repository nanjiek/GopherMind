package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gophermind/internal/core/service"
	httpcontracts "gophermind/pkg/contracts/http"
)

// SessionHandler 处理 /session/:id。
type SessionHandler struct {
	svc    *service.SessionService
	logger *zap.Logger
}

// NewSessionHandler 构建 SessionHandler。
func NewSessionHandler(svc *service.SessionService, logger *zap.Logger) *SessionHandler {
	return &SessionHandler{svc: svc, logger: logger}
}

// GetSession 返回会话和历史消息。
func (h *SessionHandler) GetSession(c *gin.Context) {
	sessionID := c.Param("id")
	userID := c.GetString("user_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, httpcontracts.Err(40002, "missing session id"))
		return
	}

	sess, msgs, err := h.svc.GetSessionWithMessages(c.Request.Context(), userID, sessionID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("get session failed", zap.Error(err))
		}
		c.JSON(http.StatusNotFound, httpcontracts.Err(40401, "session not found"))
		return
	}

	items := make([]httpcontracts.SessionMessageResponse, 0, len(msgs))
	for _, m := range msgs {
		items = append(items, httpcontracts.SessionMessageResponse{
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, httpcontracts.OK(httpcontracts.SessionData{
		SessionID: sess.ID,
		Title:     sess.Title,
		Messages:  items,
	}))
}

// ListSessions returns recent sessions for current user.
func (h *SessionHandler) ListSessions(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, httpcontracts.Err(40103, "missing user id"))
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	sessions, err := h.svc.ListSessions(c.Request.Context(), userID, limit)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("list sessions failed", zap.Error(err))
		}
		c.JSON(http.StatusInternalServerError, httpcontracts.Err(50011, "list sessions failed"))
		return
	}
	items := make([]httpcontracts.SessionItemResponse, 0, len(sessions))
	for _, sess := range sessions {
		items = append(items, httpcontracts.SessionItemResponse{
			SessionID:     sess.ID,
			Title:         sess.Title,
			LastMessageAt: sess.LastMessageAt,
			UpdatedAt:     sess.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, httpcontracts.OK(httpcontracts.SessionListData{
		Items: items,
	}))
}
