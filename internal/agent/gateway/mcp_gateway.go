package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"gophermind/internal/agent/runtime"
)

var (
	ErrInvalidMCPManifest   = errors.New("gateway MCP manifest is invalid")
	ErrDuplicateMCPPeer     = errors.New("gateway MCP peer is already registered")
	ErrUnknownMCPPeer       = errors.New("gateway MCP peer is not registered")
	ErrInvalidMCPInvocation = errors.New("gateway MCP invocation is invalid")
	ErrInvalidMCPResult     = errors.New("gateway MCP result is invalid")
	ErrMCPArgumentsTooLarge = errors.New("gateway MCP arguments exceed limit")
	ErrMCPResultTooLarge    = errors.New("gateway MCP result exceeds limit")
)

// MCPManifest binds an internal executable ID to one known MCP peer and one
// fixed remote tool. Calls cannot discover peers or select a different tool.
type MCPManifest struct {
	ID             string
	Capability     runtime.Capability
	Tool           string
	MaxInputBytes  int
	MaxOutputBytes int
	Timeout        time.Duration
}

// MCPInvocation combines trusted authorization data with untrusted JSON tool
// arguments. Arguments must be a JSON object, as required by MCP tool calls.
type MCPInvocation struct {
	Scope      runtime.Metadata
	AgentID    string
	Executable string
	Purpose    string
	Arguments  json.RawMessage
}

// MCPResult contains the bounded JSON representation of a remote tool result.
// IsError is preserved because MCP tool errors are protocol-level results.
type MCPResult struct {
	Manifest MCPManifest
	Output   json.RawMessage
	IsError  bool
}

// MCPHealthRequest carries the same trusted authorization inputs as a tool
// call. A Ping is also an outbound interaction and must be authorized.
type MCPHealthRequest struct {
	Scope   runtime.Metadata
	AgentID string
	PeerID  string
	Purpose string
}

// MCPSession is the narrow portion of the MCP SDK client session used by this
// gateway. Connections are created and owned outside this P3 node.
type MCPSession interface {
	Ping(context.Context, *mcp.PingParams) error
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// MCPGateway adapts registered MCP peers to fixed internal executables. It
// never performs peer discovery, capability discovery, or connection setup.
type MCPGateway struct {
	policy *CapabilityPolicy
	mu     sync.RWMutex
	items  map[string]registeredMCPPeer
}

type registeredMCPPeer struct {
	manifest MCPManifest
	session  MCPSession
}

func NewMCPGateway(policy *CapabilityPolicy) (*MCPGateway, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: capability policy is required", ErrInvalidMCPInvocation)
	}
	return &MCPGateway{policy: policy, items: make(map[string]registeredMCPPeer)}, nil
}

// Register adds a pre-connected, static MCP peer. Registration itself grants
// no access; every Ping and CallTool authorizes immediately before the SDK call.
func (g *MCPGateway) Register(manifest MCPManifest, session MCPSession) error {
	if err := validateMCPManifest(manifest); err != nil || session == nil {
		return fmt.Errorf("%w: valid manifest and session are required", ErrInvalidMCPManifest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.items[manifest.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateMCPPeer, manifest.ID)
	}
	g.items[manifest.ID] = registeredMCPPeer{manifest: manifest, session: session}
	return nil
}

// Health sends an authorized Ping to a registered peer. It does not establish
// a connection or enumerate the peer's capabilities.
func (g *MCPGateway) Health(ctx context.Context, request MCPHealthRequest) error {
	if request.AgentID == "" || request.PeerID == "" || request.Purpose == "" {
		return ErrInvalidMCPInvocation
	}
	item, ok := g.lookup(request.PeerID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownMCPPeer, request.PeerID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, item.manifest.Timeout)
	defer cancel()
	if err := callCtx.Err(); err != nil {
		return err
	}
	if err := g.policy.Authorize(AccessRequest{
		Scope:      request.Scope,
		AgentID:    request.AgentID,
		Capability: item.manifest.Capability,
		Purpose:    request.Purpose,
	}); err != nil {
		return err
	}
	return item.session.Ping(callCtx, &mcp.PingParams{})
}

// Call executes the one tool fixed by a registered manifest. All parsing and
// request construction happens before authorization, leaving Authorize directly
// adjacent to CallTool, the outbound side effect.
func (g *MCPGateway) Call(ctx context.Context, invocation MCPInvocation) (MCPResult, error) {
	if invocation.AgentID == "" || invocation.Executable == "" || invocation.Purpose == "" || (len(invocation.Arguments) > 0 && !json.Valid(invocation.Arguments)) {
		return MCPResult{}, ErrInvalidMCPInvocation
	}
	item, ok := g.lookup(invocation.Executable)
	if !ok {
		return MCPResult{}, fmt.Errorf("%w: %q", ErrUnknownMCPPeer, invocation.Executable)
	}
	if len(invocation.Arguments) > item.manifest.MaxInputBytes {
		return MCPResult{}, ErrMCPArgumentsTooLarge
	}
	arguments := make(map[string]any)
	if len(invocation.Arguments) > 0 {
		if err := json.Unmarshal(invocation.Arguments, &arguments); err != nil {
			return MCPResult{}, ErrInvalidMCPInvocation
		}
		if arguments == nil {
			return MCPResult{}, ErrInvalidMCPInvocation
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, item.manifest.Timeout)
	defer cancel()
	if err := callCtx.Err(); err != nil {
		return MCPResult{}, err
	}
	params := &mcp.CallToolParams{Name: item.manifest.Tool, Arguments: arguments}
	if err := g.policy.Authorize(AccessRequest{
		Scope:      invocation.Scope,
		AgentID:    invocation.AgentID,
		Capability: item.manifest.Capability,
		Purpose:    invocation.Purpose,
	}); err != nil {
		return MCPResult{}, err
	}
	result, err := item.session.CallTool(callCtx, params)
	if err != nil {
		return MCPResult{}, err
	}
	if result == nil {
		return MCPResult{}, ErrInvalidMCPResult
	}
	output, err := json.Marshal(mcpResultOutput(result))
	if err != nil {
		return MCPResult{}, fmt.Errorf("%w: %v", ErrInvalidMCPResult, err)
	}
	if len(output) > item.manifest.MaxOutputBytes {
		return MCPResult{}, ErrMCPResultTooLarge
	}
	return MCPResult{Manifest: item.manifest, Output: json.RawMessage(output), IsError: result.IsError}, nil
}

func (g *MCPGateway) lookup(id string) (registeredMCPPeer, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	item, ok := g.items[id]
	return item, ok
}

func validateMCPManifest(manifest MCPManifest) error {
	if manifest.ID == "" || manifest.Capability == "" || manifest.Tool == "" || manifest.MaxInputBytes < 0 || manifest.MaxOutputBytes <= 0 || manifest.Timeout <= 0 {
		return ErrInvalidMCPManifest
	}
	return nil
}

func mcpResultOutput(result *mcp.CallToolResult) any {
	if result.StructuredContent != nil {
		return result.StructuredContent
	}
	return result.Content
}
