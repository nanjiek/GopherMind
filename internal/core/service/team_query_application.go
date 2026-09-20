package service

import (
	"context"
	"fmt"

	"gophermind/internal/agent/runtime"
)

// TeamQueryApplication is the one-way P5 application boundary. It is usable
// by a future authenticated HTTP adapter, but deliberately accepts only the
// policy input rather than client-selected routing or a raw Team path.
type TeamQueryApplication struct {
	Policy  *TrustedQueryPolicy
	Team    *RoutedTeamQueryService
	Barrier *ResponseCommitBarrier
	Reader  CommittedResponseReader
}

// CommittedResponseReader is deliberately read-only. Replay is never allowed
// to run a Team worker, call a model, or cause a Tool effect.
type CommittedResponseReader interface {
	LoadCommittedResponse(context.Context, runtime.Metadata, string) (CommittedTeamResponse, int64, error)
}

type TeamQueryApplicationOutput struct {
	RequiresHuman bool
	Data          []byte
}

func (a *TeamQueryApplication) Execute(ctx context.Context, input TrustedQueryPolicyInput) (TeamQueryApplicationOutput, error) {
	if a == nil || a.Policy == nil || a.Team == nil || a.Barrier == nil {
		return TeamQueryApplicationOutput{}, fmt.Errorf("team query application requires policy, team, and commit barrier")
	}
	routed, err := a.Policy.Build(input)
	if err != nil {
		return TeamQueryApplicationOutput{}, err
	}
	result, err := a.Team.Start(ctx, routed)
	if err != nil {
		return TeamQueryApplicationOutput{}, err
	}
	if result.RequiresHuman {
		return TeamQueryApplicationOutput{RequiresHuman: true, Data: append([]byte(nil), result.Data...)}, nil
	}
	if err := a.Barrier.Commit(ctx, ReviewedTeamResponse{RunID: routed.RunID, Scope: routed.Scope, Data: result.Data}); err != nil {
		return TeamQueryApplicationOutput{}, err
	}
	return TeamQueryApplicationOutput{Data: append([]byte(nil), result.Data...)}, nil
}

// Replay returns only the response that passed the Safety and commit barriers.
func (a *TeamQueryApplication) Replay(ctx context.Context, scope runtime.Metadata, runID string) (CommittedTeamResponse, int64, error) {
	if a == nil || a.Reader == nil {
		return CommittedTeamResponse{}, 0, fmt.Errorf("team query replay reader is unavailable")
	}
	return a.Reader.LoadCommittedResponse(ctx, scope, runID)
}
