package service

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/agent/runtime"
)

// PathFixedTeamStarter selects only a prebuilt coordinator for the closed
// route path. Startup binds model configurations without letting a request or
// worker select a runtime topology.
type PathFixedTeamStarter struct {
	Simple   FixedTeamStarter
	Standard FixedTeamStarter
	Human    FixedTeamStarter
}

func (s PathFixedTeamStarter) Start(ctx context.Context, spec runtime.FixedTeamSpec, request json.RawMessage) (runtime.ResponseOutput, error) {
	var target FixedTeamStarter
	switch spec.Path {
	case runtime.FixedTeamPathSimple:
		target = s.Simple
	case runtime.FixedTeamPathStandard:
		target = s.Standard
	case runtime.FixedTeamPathHuman:
		target = s.Human
	default:
		return runtime.ResponseOutput{}, fmt.Errorf("fixed team path %q is not configured", spec.Path)
	}
	if target == nil {
		return runtime.ResponseOutput{}, fmt.Errorf("fixed team path %q is unavailable", spec.Path)
	}
	return target.Start(ctx, spec, request)
}
