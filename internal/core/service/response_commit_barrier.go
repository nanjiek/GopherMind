package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

var ErrResponseCommitBlocked = errors.New("response commit is blocked")

// ResponseCommitAuthorizer is invoked immediately before the only external
// effect in this boundary. A prior authorization check is never reusable.
type ResponseCommitAuthorizer interface {
	Authorize(gateway.AccessRequest) error
}

// ResponseReviewVerifier is the trusted adapter to the completed Safety task.
// Its durable Task proof is added in the following persistence node; the
// barrier intentionally does not accept a caller supplied "approved" boolean.
type ResponseReviewVerifier interface {
	VerifyReviewedResponse(context.Context, ReviewedTeamResponse) error
}

// ReviewedResponseCommitter performs the effect after all barrier checks. It
// will later be backed by committed-response storage and an Action/Event outbox.
type ReviewedResponseCommitter interface {
	CommitReviewedResponse(context.Context, CommittedTeamResponse) error
}

// ReviewedTeamResponse is a completed Team result that is still uncommitted.
// RequiresHuman is terminal at this boundary and can never become a normal
// assistant response.
type ReviewedTeamResponse struct {
	RunID         string
	Scope         runtime.Metadata
	RequiresHuman bool
	Data          json.RawMessage
}

// CommittedTeamResponse is the minimal effect payload passed only after
// review and immediate Capability authorization succeed.
type CommittedTeamResponse struct {
	RunID string
	Scope runtime.Metadata
	Data  json.RawMessage
}

// ResponseCommitBarrier is the sole P5 application contract allowed to hand a
// reviewed Team answer to a future publisher. It does not itself persist,
// stream, queue, or expose a response.
type ResponseCommitBarrier struct {
	Verifier   ResponseReviewVerifier
	Authorizer ResponseCommitAuthorizer
	Committer  ReviewedResponseCommitter
	AgentID    string
	Capability runtime.Capability
	Purpose    string
}

// Commit verifies the final response before authorizing. Authorization is
// deliberately the statement immediately preceding CommitReviewedResponse.
func (b *ResponseCommitBarrier) Commit(ctx context.Context, candidate ReviewedTeamResponse) error {
	if b == nil || b.Verifier == nil || b.Authorizer == nil || b.Committer == nil || b.AgentID == "" || b.Capability == "" || b.Purpose == "" {
		return fmt.Errorf("%w: verifier, authorizer, committer, agent, capability, and purpose are required", ErrResponseCommitBlocked)
	}
	if err := validateReviewedTeamResponse(candidate); err != nil {
		return err
	}
	if candidate.RequiresHuman {
		return fmt.Errorf("%w: human escalation cannot be published", ErrResponseCommitBlocked)
	}
	if err := b.Verifier.VerifyReviewedResponse(ctx, cloneReviewedTeamResponse(candidate)); err != nil {
		return err
	}
	if err := b.Authorizer.Authorize(gateway.AccessRequest{
		Scope: candidate.Scope, AgentID: b.AgentID, Capability: b.Capability, Purpose: b.Purpose,
	}); err != nil {
		return err
	}
	return b.Committer.CommitReviewedResponse(ctx, CommittedTeamResponse{
		RunID: candidate.RunID, Scope: candidate.Scope, Data: append(json.RawMessage(nil), candidate.Data...),
	})
}

func validateReviewedTeamResponse(candidate ReviewedTeamResponse) error {
	if candidate.RunID == "" || candidate.Scope.TenantID == "" || candidate.Scope.UserID == "" {
		return fmt.Errorf("%w: trusted run and tenant/user scope are required", ErrResponseCommitBlocked)
	}
	var schema struct {
		Answer string `json:"answer"`
	}
	if len(bytes.TrimSpace(candidate.Data)) == 0 || json.Unmarshal(candidate.Data, &schema) != nil || strings.TrimSpace(schema.Answer) == "" {
		return fmt.Errorf("%w: final response requires a non-empty answer schema", ErrResponseCommitBlocked)
	}
	return nil
}

func cloneReviewedTeamResponse(candidate ReviewedTeamResponse) ReviewedTeamResponse {
	candidate.Data = append(json.RawMessage(nil), candidate.Data...)
	return candidate
}
