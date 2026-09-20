package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gophermind/internal/agent/runtime"
	"gophermind/internal/core/service"
	httpcontracts "gophermind/pkg/contracts/http"
)

// CommittedResponseHandler exposes read-only replay of a response that has
// already passed Safety and the response commit barrier.
type CommittedResponseHandler struct {
	team     *service.TeamQueryApplication
	tenantID string
	logger   *zap.Logger
}

func NewCommittedResponseHandler(team *service.TeamQueryApplication, tenantID string, logger *zap.Logger) *CommittedResponseHandler {
	return &CommittedResponseHandler{team: team, tenantID: tenantID, logger: logger}
}

func (h *CommittedResponseHandler) Handle(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, httpcontracts.Err(40103, "missing user id"))
		return
	}
	if h == nil || h.team == nil || h.team.Reader == nil || h.tenantID == "" {
		c.JSON(http.StatusServiceUnavailable, httpcontracts.Err(50321, "trusted team query path unavailable"))
		return
	}
	runID := c.Param("run")
	scope := runtime.Metadata{TenantID: h.tenantID, UserID: userID, SessionID: c.Query("session_id")}
	response, eventSeq, err := h.team.Replay(c.Request.Context(), scope, runID)
	if err != nil {
		if h.logger != nil && !errors.Is(err, runtime.ErrTaskDAGNotFound) {
			h.logger.Error("committed response replay failed", zap.Error(err))
		}
		// A missing or differently scoped response is deliberately indistinguishable.
		c.JSON(http.StatusNotFound, httpcontracts.Err(40441, "committed response not found"))
		return
	}
	var data json.RawMessage
	if json.Unmarshal(response.Data, &data) != nil {
		c.JSON(http.StatusInternalServerError, httpcontracts.Err(50001, "committed response invalid"))
		return
	}
	c.JSON(http.StatusOK, httpcontracts.OK(gin.H{"run_id": response.RunID, "event_seq": eventSeq, "response": data}))
}
