package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
	"gophermind/internal/model/factory"
	"gophermind/internal/model/providers"
	logpkg "gophermind/internal/obs/logger"
	metricspkg "gophermind/internal/obs/metrics"
	otelpkg "gophermind/internal/obs/otel"
	"gophermind/internal/queue/rabbitmq"
	ragclient "gophermind/internal/rag/client"
	postgresrepo "gophermind/internal/repo/postgres"
	redisrepo "gophermind/internal/repo/redis"
	"gophermind/internal/security/secret"
	mcptransport "gophermind/internal/transport/mcp"
)

func main() {
	cfg := config.Load()
	secretProvider := secret.NewEnvProvider()
	if v := secretProvider.Get("JWT_ACCESS_SECRET"); v != "" {
		cfg.Auth.AccessSecret = v
	}
	if v := secretProvider.Get("JWT_REFRESH_SECRET"); v != "" {
		cfg.Auth.RefreshSecret = v
	}

	logg, err := logpkg.New("info")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logg.Sync() }()
	if err := validateMCPConfig(cfg.MCP); err != nil {
		logg.Fatal("invalid mcp config", zap.Error(err))
	}

	_, shutdownTrace, err := otelpkg.InitTracerProvider(cfg.ServiceName+"-mcp", os.Stdout)
	if err != nil {
		logg.Fatal("init otel failed", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()
	metricspkg.RegisterAll()

	db, err := postgresrepo.NewDB(cfg.Postgres)
	if err != nil {
		logg.Fatal("init postgres failed", zap.Error(err))
	}
	sessionRepo := postgresrepo.NewSessionRepository(db)
	cache := redisrepo.NewSessionCache(cfg.Redis, logg)
	producer := rabbitmq.NewNoopProducer(logg)
	defer producer.Close()

	openaiProvider := providers.NewOpenAIProvider(cfg.Model, logg)
	kimiProvider := providers.NewKimiProvider(cfg.Model, logg)
	qwenProvider := providers.NewQwenProvider(cfg.Model, logg)
	ollamaProvider := providers.NewOllamaProvider(cfg.Model, logg)
	bgeProvider := providers.NewBGEProvider(cfg.Model, logg)
	modelRouter := factory.NewModelFactory(openaiProvider, kimiProvider, qwenProvider, ollamaProvider, bgeProvider, logg)

	rag := ragclient.NewPythonClient(cfg.RAG, logg)
	sessionService := service.NewSessionService(sessionRepo, cache, logg)
	// Optional memory, tracing and judge services are not enabled in this baseline.
	queryService := service.NewQueryService(sessionRepo, sessionService, modelRouter, rag, producer, cache, nil, nil, nil, logg)
	streamService := service.NewStreamService(sessionRepo, sessionService, modelRouter, rag, cache, nil, nil, nil, logg)

	mcpServer, err := mcptransport.NewServer(cfg, logg, queryService, sessionService, streamService)
	if err != nil {
		logg.Fatal("init mcp server failed", zap.Error(err))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		logg.Info("mcp server shutting down")
	}()

	if err := mcpServer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logg.Fatal("mcp server run failed", zap.Error(err))
	}
}

func validateMCPConfig(cfg config.MCPConfig) error {
	if !cfg.Enabled {
		return errors.New("MCP_ENABLED=false; set MCP_ENABLED=true to run mcp server")
	}
	if cfg.Transport != "" && cfg.Transport != "stdio" {
		return errors.New("only MCP_TRANSPORT=stdio is supported")
	}
	if cfg.DefaultUserID == "" {
		return errors.New("MCP_DEFAULT_USER_ID must not be empty")
	}
	return nil
}
