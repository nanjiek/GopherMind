package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

// ErrInvalidRoutedTeamQuery is returned before a Team graph is created when
// the caller has not supplied the trusted scope, routing attributes, or input
// required by the fixed Team entry point.
var ErrInvalidRoutedTeamQuery = errors.New("routed team query is invalid")

// TeamRouteSource is the P3 control-plane boundary. Its Request input must
// come from a trusted deterministic policy or triage component, never an HTTP
// request body or model output.
type TeamRouteSource interface {
	Route(gateway.Request) (gateway.Decision, error)
}

// FixedTeamStarter is deliberately the smallest P4 execution boundary needed
// by the query entry. Implementations may be durable, but this entry neither
// publishes an answer nor performs another external side effect.
type FixedTeamStarter interface {
	Start(context.Context, runtime.FixedTeamSpec, json.RawMessage) (runtime.ResponseOutput, error)
}

// RoutedTeamQueryInput has the attributes that must be established inside the
// trusted application boundary before a Query/API request may enter a P4 Team.
// Route is intentionally separate from Payload: user data cannot choose risk,
// task, workflow, model, or Team topology.
type RoutedTeamQueryInput struct {
	RunID    string
	Scope    runtime.Metadata
	Deadline time.Time
	Route    gateway.Request
	Payload  json.RawMessage
}

// RoutedTeamQueryOutput is an uncommitted Team result. A future Response
// commit barrier is the only component allowed to turn Data into a persisted
// assistant message, streamed chunk, queue event, or other external effect.
// RequiresHuman tells a caller to hand off instead of treating Data as an
// answer.
type RoutedTeamQueryOutput struct {
	Workflow      gateway.WorkflowID
	Path          runtime.FixedTeamPath
	RequiresHuman bool
	Data          json.RawMessage
}

// RoutedTeamQueryService bridges the trusted P3 Router and P4 fixed Team at
// the Query/API application boundary. It intentionally has no legacy session,
// model, queue, cache, or response-commit dependency.
type RoutedTeamQueryService struct {
	router TeamRouteSource
	team   FixedTeamStarter
}

// NewRoutedTeamQueryService builds the no-side-effect query Team entry.
func NewRoutedTeamQueryService(router TeamRouteSource, team FixedTeamStarter) *RoutedTeamQueryService {
	return &RoutedTeamQueryService{router: router, team: team}
}

// Start routes trusted attributes, derives the closed Team path, and starts
// that Team exactly once. It does not trust a caller-provided Decision or path.
func (s *RoutedTeamQueryService) Start(ctx context.Context, in RoutedTeamQueryInput) (RoutedTeamQueryOutput, error) {
	if s == nil || s.router == nil || s.team == nil {
		return RoutedTeamQueryOutput{}, fmt.Errorf("%w: router and fixed team are required", ErrInvalidRoutedTeamQuery)
	}
	if err := validRoutedTeamQueryInput(in); err != nil {
		return RoutedTeamQueryOutput{}, err
	}
	decision, err := s.router.Route(in.Route)
	if err != nil {
		return RoutedTeamQueryOutput{}, err
	}
	path, err := gateway.FixedTeamPath(decision)
	if err != nil {
		return RoutedTeamQueryOutput{}, err
	}
	response, err := s.team.Start(ctx, runtime.FixedTeamSpec{
		RunID:    in.RunID,
		Scope:    in.Scope,
		Deadline: in.Deadline,
		Path:     path,
	}, cloneRoutedTeamJSON(in.Payload))
	if err != nil {
		return RoutedTeamQueryOutput{}, err
	}
	return RoutedTeamQueryOutput{
		Workflow:      decision.Workflow,
		Path:          path,
		RequiresHuman: decision.RequiresHuman,
		Data:          cloneRoutedTeamJSON(response.Data),
	}, nil
}

func validRoutedTeamQueryInput(in RoutedTeamQueryInput) error {
	if in.RunID == "" || in.Scope.TenantID == "" || in.Scope.UserID == "" || in.Deadline.IsZero() {
		return fmt.Errorf("%w: run ID, tenant scope, user scope, and deadline are required", ErrInvalidRoutedTeamQuery)
	}
	var data map[string]json.RawMessage
	if len(bytes.TrimSpace(in.Payload)) == 0 || json.Unmarshal(in.Payload, &data) != nil || data == nil {
		return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidRoutedTeamQuery)
	}
	return nil
}

func cloneRoutedTeamJSON(data json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), data...)
}
