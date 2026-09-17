package main

import (
	"context"
	"errors"
	"net/http"
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
	mysqlrepo "gophermind/internal/repo/mysql"
	redisrepo "gophermind/internal/repo/redis"
	"gophermind/internal/security/secret"
	"gophermind/internal/security/token"
	httptransport "gophermind/internal/transport/http"
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
	if err := validateAuthConfig(cfg.Auth); err != nil {
		logg.Fatal("invalid auth config", zap.Error(err))
	}

	_, shutdownTrace, err := otelpkg.InitTracerProvider(cfg.ServiceName, os.Stdout)
	if err != nil {
		logg.Fatal("init otel failed", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()
	metricspkg.RegisterAll()

	db, err := mysqlrepo.NewDB(cfg.MySQL)
	if err != nil {
		logg.Fatal("init mysql failed", zap.Error(err))
	}
	sessionRepo := mysqlrepo.NewSessionRepository(db)
	authRepo := mysqlrepo.NewAuthRepository(db)
	inboxRepo := mysqlrepo.NewInboxRepository(db)

	cache := redisrepo.NewSessionCache(cfg.Redis, logg)
	var producer rabbitmq.Producer
	producer, err = rabbitmq.NewProducer(cfg.RabbitMQ, logg)
	if err != nil {
		logg.Warn("init rabbitmq producer failed, fallback to noop", zap.Error(err))
		producer = rabbitmq.NewNoopProducer(logg)
	}
	defer producer.Close()

	openaiProvider := providers.NewOpenAIProvider(cfg.Model, logg)
	kimiProvider := providers.NewKimiProvider(cfg.Model, logg)
	ollamaProvider := providers.NewOllamaProvider(cfg.Model, logg)
	bgeProvider := providers.NewBGEProvider(cfg.Model, logg)
	modelRouter := factory.NewModelFactory(openaiProvider, kimiProvider, ollamaProvider, bgeProvider, logg)

	rag := ragclient.NewPythonClient(cfg.RAG, logg)
	tokenManager := token.NewManager(cfg.Auth)
	authService := service.NewAuthService(authRepo, tokenManager)
	attachmentService := service.NewAttachmentService(cfg.Upload, logg)
	sessionService := service.NewSessionService(sessionRepo, cache, logg)
	queryService := service.NewQueryService(sessionRepo, sessionService, modelRouter, rag, producer, cache, logg)
	streamService := service.NewStreamService(sessionRepo, sessionService, modelRouter, rag, cache, logg)

	router := httptransport.NewRouter(
		cfg,
		logg,
		authService,
		attachmentService,
		queryService,
		sessionService,
		streamService,
	)

	server := &http.Server{
		Addr:         cfg.HTTP.Address,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	go func() {
		logg.Info("http server listening", zap.String("addr", cfg.HTTP.Address))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logg.Fatal("http serve failed", zap.Error(err))
		}
	}()

	consumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ, cache, inboxRepo, producer, logg)
	if err != nil {
		logg.Warn("init rabbitmq consumer failed", zap.Error(err))
	} else {
		go consumer.Start(context.Background())
		defer consumer.Close()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logg.Error("http shutdown failed", zap.Error(err))
	}
}

func validateAuthConfig(cfg config.AuthConfig) error {
	if cfg.AccessSecret == "" || cfg.RefreshSecret == "" {
		return errors.New("jwt access/refresh secret must not be empty")
	}
	if cfg.AccessSecret == cfg.RefreshSecret {
		return errors.New("jwt access and refresh secrets must be different")
	}
	if len(cfg.AccessSecret) < 32 || len(cfg.RefreshSecret) < 32 {
		return errors.New("jwt access/refresh secrets must be at least 32 bytes")
	}
	return nil
}
