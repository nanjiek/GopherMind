package tools

import (
	"context"
	"errors"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
	"gophermind/internal/transport/mcp/auth"
)

type StreamQueryToolInput struct {
	UserID    string `json:"user_id,omitempty" jsonschema:"User identifier. Optional for stdio mode when MCP_DEFAULT_USER_ID is set."`
	SessionID string `json:"session_id,omitempty" jsonschema:"Existing session id. Omit to create a new session."`
	Question  string `json:"question" jsonschema:"Question content to query."`
	ModelType string `json:"model_type,omitempty" jsonschema:"Model route type, e.g. auto/openai/kimi/ollama."`
	UseRAG    bool   `json:"use_rag,omitempty" jsonschema:"Whether to enable RAG retrieval."`
}

type StreamQueryToolOutput struct {
	RequestID string           `json:"request_id"`
	SessionID string           `json:"session_id"`
	Answer    string           `json:"answer"`
	Citations []model.Citation `json:"citations,omitempty"`
	Usage     model.Usage      `json:"usage"`
	Tokens    []string         `json:"tokens,omitempty"`
}

func RegisterStreamQueryTool(server *mcp.Server, cfg config.MCPConfig, streamService *service.StreamService) {
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "gm.stream_query",
			Description: "Run streaming QA and return final answer with collected tokens.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StreamQueryToolInput) (*mcp.CallToolResult, StreamQueryToolOutput, error) {
			userID, err := auth.ResolveUserID(input.UserID, cfg.DefaultUserID)
			if err != nil {
				return nil, StreamQueryToolOutput{}, err
			}
			question := strings.TrimSpace(input.Question)
			if question == "" {
				return nil, StreamQueryToolOutput{}, errors.New("question must not be empty")
			}
			tokens := make([]string, 0, 64)
			out, err := streamService.Stream(ctx, model.QueryInput{
				UserID:    userID,
				SessionID: strings.TrimSpace(input.SessionID),
				Question:  question,
				ModelType: strings.TrimSpace(input.ModelType),
				UseRAG:    input.UseRAG,
			}, func(token string) error {
				tokens = append(tokens, token)
				return nil
			})
			if err != nil {
				return nil, StreamQueryToolOutput{}, err
			}

			return nil, StreamQueryToolOutput{
				RequestID: out.RequestID,
				SessionID: out.SessionID,
				Answer:    out.Answer,
				Citations: out.Citations,
				Usage:     out.Usage,
				Tokens:    tokens,
			}, nil
		},
	)
}
