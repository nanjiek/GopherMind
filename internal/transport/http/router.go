package httptransport

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
	"gophermind/internal/security/token"
	"gophermind/internal/transport/http/handlers"
	"gophermind/internal/transport/http/middleware"
)

// NewRouter 构建 HTTP 路由。
func NewRouter(
	cfg config.Config,
	logger *zap.Logger,
	authService *service.AuthService,
	attachmentService *service.AttachmentService,
	queryService *service.QueryService,
	sessionService *service.SessionService,
	streamService *service.StreamService,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.RequestID())
	r.Use(middleware.HTTPMetrics())
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	tokenManager := token.NewManager(cfg.Auth)
	authMW := middleware.Auth(cfg.Auth, tokenManager, logger)
	ah := handlers.NewAuthHandler(authService, logger)
	qh := handlers.NewQueryHandler(queryService, nil, logger)
	sh := handlers.NewSessionHandler(sessionService, logger)
	sth := handlers.NewStreamHandler(streamService, nil, cfg.RAG.DocumentWaitReadyTimeout, logger)
	atth := handlers.NewAttachmentHandler(attachmentService, logger)

	public := r.Group("/auth")
	{
		public.POST("/register", ah.Register)
		public.POST("/login", ah.Login)
		public.POST("/refresh", ah.Refresh)
		public.POST("/logout", ah.Logout)
	}

	api := r.Group("/")
	api.Use(authMW)
	{
		api.POST("/query", qh.Handle)
		api.GET("/sessions", sh.ListSessions)
		api.GET("/session/:id", sh.GetSession)
		api.GET("/stream/:session", sth.Handle)
		api.POST("/attachments", atth.Upload)
		api.GET("/attachments/file", atth.Download)
	}
	return r
}
