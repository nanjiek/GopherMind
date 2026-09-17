package mcptransport

import (
	"context"
	"errors"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
	"gophermind/internal/transport/mcp/tools"
)

type Server struct {
	server *mcp.Server
	logger *zap.Logger
}

func NewServer(
	cfg config.Config,
	logger *zap.Logger,
	queryService *service.QueryService,
	sessionService *service.SessionService,
	streamService *service.StreamService,
) (*Server, error) {
	if strings.TrimSpace(cfg.MCP.Transport) != "stdio" && strings.TrimSpace(cfg.MCP.Transport) != "" {
		return nil, errors.New("unsupported MCP transport, only stdio is implemented")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gophermind-mcp",
		Version: "0.1.0",
	}, nil)

	tools.RegisterQueryTool(server, cfg.MCP, queryService)
	tools.RegisterGetSessionTool(server, cfg.MCP, sessionService)
	tools.RegisterStreamQueryTool(server, cfg.MCP, streamService)

	return &Server{
		server: server,
		logger: logger,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s.logger != nil {
		s.logger.Info("mcp stdio server starting")
	}
	transport := mcp.NewStdioTransport()
	return s.server.Run(ctx, transport)
}
