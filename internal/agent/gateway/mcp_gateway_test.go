package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"gophermind/internal/agent/runtime"
)

func TestMCPGatewayAuthorizesImmediatelyBeforeToolCall(t *testing.T) {
	session := &fakeMCPSession{result: &mcp.CallToolResult{StructuredContent: map[string]any{"hits": []string{"a"}}}}
	gateway := newMCPGatewayForTest(t, []Grant{{AgentID: "evidence", Capability: "knowledge.search"}})
	registerMCPForTest(t, gateway, session, MCPManifest{ID: "evidence-search", Capability: "knowledge.search", Tool: "search", MaxInputBytes: 128, MaxOutputBytes: 128, Timeout: time.Second})

	result, err := gateway.Call(context.Background(), MCPInvocation{AgentID: "evidence", Executable: "evidence-search", Purpose: "evidence retrieval", Arguments: []byte(`{"query":"fever"}`)})
	if err != nil || !session.called.Load() || session.tool != "search" || string(result.Output) != `{"hits":["a"]}` {
		t.Fatalf("Call() = %#v, %v, called=%v tool=%q", result, err, session.called.Load(), session.tool)
	}
	if got := session.arguments["query"]; got != "fever" {
		t.Fatalf("arguments query = %#v", got)
	}
}

func TestMCPGatewayDenialPreventsToolCallAndPing(t *testing.T) {
	session := &fakeMCPSession{}
	gateway := newMCPGatewayForTest(t, nil)
	registerMCPForTest(t, gateway, session, MCPManifest{ID: "write", Capability: "patient.write", Tool: "write_record", MaxInputBytes: 128, MaxOutputBytes: 128, Timeout: time.Second})
	_, err := gateway.Call(context.Background(), MCPInvocation{AgentID: "untrusted", Executable: "write", Purpose: "test", Arguments: []byte(`{}`)})
	if !errors.Is(err, ErrCapabilityDenied) || session.called.Load() {
		t.Fatalf("Call() error = %v, called=%v", err, session.called.Load())
	}
	err = gateway.Health(context.Background(), MCPHealthRequest{AgentID: "untrusted", PeerID: "write", Purpose: "health check"})
	if !errors.Is(err, ErrCapabilityDenied) || session.pinged.Load() {
		t.Fatalf("Health() error = %v, pinged=%v", err, session.pinged.Load())
	}
}

func TestMCPGatewayHealthAndPatientScope(t *testing.T) {
	session := &fakeMCPSession{}
	gateway := newMCPGatewayForTest(t, []Grant{{AgentID: "medication", Capability: "patient.medication.read", PatientBound: true}})
	registerMCPForTest(t, gateway, session, MCPManifest{ID: "medications", Capability: "patient.medication.read", Tool: "lookup", MaxInputBytes: 128, MaxOutputBytes: 128, Timeout: time.Second})
	err := gateway.Health(context.Background(), MCPHealthRequest{AgentID: "medication", PeerID: "medications", Purpose: "health check"})
	if !errors.Is(err, ErrCapabilityDenied) || session.pinged.Load() {
		t.Fatalf("Health() without scope error = %v, pinged=%v", err, session.pinged.Load())
	}
	err = gateway.Health(context.Background(), MCPHealthRequest{AgentID: "medication", PeerID: "medications", Purpose: "health check", Scope: runtimeMetadataForTest()})
	if err != nil || !session.pinged.Load() {
		t.Fatalf("Health() with scope error = %v, pinged=%v", err, session.pinged.Load())
	}
}

func TestMCPGatewayRejectsInvalidAndOversizedResults(t *testing.T) {
	session := &fakeMCPSession{result: &mcp.CallToolResult{StructuredContent: map[string]any{"result": "too large"}}}
	gateway := newMCPGatewayForTest(t, []Grant{{AgentID: "agent", Capability: "external.call"}})
	registerMCPForTest(t, gateway, session, MCPManifest{ID: "tiny", Capability: "external.call", Tool: "tool", MaxInputBytes: 2, MaxOutputBytes: 2, Timeout: time.Second})
	_, err := gateway.Call(context.Background(), MCPInvocation{AgentID: "agent", Executable: "tiny", Purpose: "test", Arguments: []byte(`{}`)})
	if !errors.Is(err, ErrMCPResultTooLarge) {
		t.Fatalf("Call() error = %v, want ErrMCPResultTooLarge", err)
	}
	_, err = gateway.Call(context.Background(), MCPInvocation{AgentID: "agent", Executable: "tiny", Purpose: "test", Arguments: []byte(`[]`)})
	if !errors.Is(err, ErrInvalidMCPInvocation) {
		t.Fatalf("Call() invalid arguments error = %v", err)
	}
	_, err = gateway.Call(context.Background(), MCPInvocation{AgentID: "agent", Executable: "tiny", Purpose: "test", Arguments: []byte(`{"long":1}`)})
	if !errors.Is(err, ErrMCPArgumentsTooLarge) {
		t.Fatalf("Call() oversized arguments error = %v", err)
	}
}

func TestMCPGatewayPropagatesCanceledParentWithoutCall(t *testing.T) {
	session := &fakeMCPSession{}
	gateway := newMCPGatewayForTest(t, []Grant{{AgentID: "agent", Capability: "external.call"}})
	registerMCPForTest(t, gateway, session, MCPManifest{ID: "cancel", Capability: "external.call", Tool: "tool", MaxInputBytes: 128, MaxOutputBytes: 128, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gateway.Call(ctx, MCPInvocation{AgentID: "agent", Executable: "cancel", Purpose: "test", Arguments: []byte(`{}`)})
	if !errors.Is(err, context.Canceled) || session.called.Load() {
		t.Fatalf("Call() error = %v, called=%v", err, session.called.Load())
	}
}

type fakeMCPSession struct {
	called    atomic.Bool
	pinged    atomic.Bool
	tool      string
	arguments map[string]any
	result    *mcp.CallToolResult
}

func (s *fakeMCPSession) Ping(context.Context, *mcp.PingParams) error {
	s.pinged.Store(true)
	return nil
}

func (s *fakeMCPSession) CallTool(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	s.called.Store(true)
	s.tool = params.Name
	if params.Arguments != nil {
		s.arguments = params.Arguments.(map[string]any)
	}
	if s.result == nil {
		return &mcp.CallToolResult{StructuredContent: map[string]any{}}, nil
	}
	return s.result, nil
}

func newMCPGatewayForTest(t *testing.T, grants []Grant) *MCPGateway {
	t.Helper()
	policy, err := NewCapabilityPolicy(grants)
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	gateway, err := NewMCPGateway(policy)
	if err != nil {
		t.Fatalf("NewMCPGateway() error = %v", err)
	}
	return gateway
}

func registerMCPForTest(t *testing.T, gateway *MCPGateway, session MCPSession, manifest MCPManifest) {
	t.Helper()
	if err := gateway.Register(manifest, session); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func runtimeMetadataForTest() runtime.Metadata {
	return runtime.Metadata{TenantID: "tenant", UserID: "user", PatientID: "patient"}
}
