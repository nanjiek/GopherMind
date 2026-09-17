package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
	"gophermind/internal/transport/mcp/auth"
)

type GetSessionToolInput struct {
	UserID    string `json:"user_id,omitempty" jsonschema:"User identifier. Optional for stdio mode when MCP_DEFAULT_USER_ID is set."`
	SessionID string `json:"session_id" jsonschema:"Target session id."`
}

type GetSessionToolOutput struct {
	SessionID string                  `json:"session_id"`
	Title     string                  `json:"title"`
	Messages  []GetSessionToolMessage `json:"messages"`
}

type GetSessionToolMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func RegisterGetSessionTool(server *mcp.Server, cfg config.MCPConfig, sessionService *service.SessionService) {
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "gm.get_session",
			Description: "Fetch session metadata and full message history.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetSessionToolInput) (*mcp.CallToolResult, GetSessionToolOutput, error) {
			userID, err := auth.ResolveUserID(input.UserID, cfg.DefaultUserID)
			if err != nil {
				return nil, GetSessionToolOutput{}, err
			}
			sessionID := strings.TrimSpace(input.SessionID)
			if sessionID == "" {
				return nil, GetSessionToolOutput{}, errors.New("session_id must not be empty")
			}

			session, messages, err := sessionService.GetSessionWithMessages(ctx, userID, sessionID)
			if err != nil {
				return nil, GetSessionToolOutput{}, err
			}

			outMessages := make([]GetSessionToolMessage, 0, len(messages))
			for _, message := range messages {
				outMessages = append(outMessages, GetSessionToolMessage{
					Role:      message.Role,
					Content:   message.Content,
					CreatedAt: message.CreatedAt,
				})
			}

			return nil, GetSessionToolOutput{
				SessionID: session.ID,
				Title:     session.Title,
				Messages:  outMessages,
			}, nil
		},
	)
}
