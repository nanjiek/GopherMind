package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamHandlerFailsClosedWithoutApprovedTeamStreamingPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewStreamHandler(nil, nil, 0, nil)
	r := gin.New()
	r.GET("/stream/:session", handler.Handle)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stream/s1?q=hello", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
