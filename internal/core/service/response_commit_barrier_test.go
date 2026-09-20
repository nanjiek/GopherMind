package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

type recordedReviewVerifier struct {
	calls int
	err   error
}

func (r *recordedReviewVerifier) VerifyReviewedResponse(_ context.Context, _ ReviewedTeamResponse) error {
	r.calls++
	return r.err
}

type recordedCommitAuthorizer struct {
	calls   int
	request gateway.AccessRequest
	err     error
}

func (a *recordedCommitAuthorizer) Authorize(request gateway.AccessRequest) error {
	a.calls++
	a.request = request
	return a.err
}

type recordedResponseCommitter struct {
	calls    int
	response CommittedTeamResponse
	err      error
}

func (c *recordedResponseCommitter) CommitReviewedResponse(_ context.Context, response CommittedTeamResponse) error {
	c.calls++
	c.response = response
	return c.err
}

func TestResponseCommitBarrierVerifiesThenImmediatelyAuthorizesAndCommits(t *testing.T) {
	verifier, authorizer, committer := &recordedReviewVerifier{}, &recordedCommitAuthorizer{}, &recordedResponseCommitter{}
	barrier := newResponseCommitBarrier(verifier, authorizer, committer)
	candidate := validReviewedTeamResponse()

	require.NoError(t, barrier.Commit(context.Background(), candidate))
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, 1, authorizer.calls)
	require.Equal(t, 1, committer.calls)
	require.Equal(t, "response-agent", authorizer.request.AgentID)
	require.Equal(t, runtime.Capability("response.commit"), authorizer.request.Capability)
	require.JSONEq(t, `{"answer":"reviewed"}`, string(committer.response.Data))
	candidate.Data[2] = 'X'
	require.JSONEq(t, `{"answer":"reviewed"}`, string(committer.response.Data))
}

func TestResponseCommitBarrierRejectsHumanOrInvalidOutputBeforeAnyEffect(t *testing.T) {
	verifier, authorizer, committer := &recordedReviewVerifier{}, &recordedCommitAuthorizer{}, &recordedResponseCommitter{}
	barrier := newResponseCommitBarrier(verifier, authorizer, committer)
	human := validReviewedTeamResponse()
	human.RequiresHuman = true
	require.ErrorIs(t, barrier.Commit(context.Background(), human), ErrResponseCommitBlocked)
	invalid := validReviewedTeamResponse()
	invalid.Data = []byte(`{"answer":""}`)
	require.ErrorIs(t, barrier.Commit(context.Background(), invalid), ErrResponseCommitBlocked)
	require.Zero(t, verifier.calls)
	require.Zero(t, authorizer.calls)
	require.Zero(t, committer.calls)
}

func TestResponseCommitBarrierStopsOnVerificationOrReauthorizationFailure(t *testing.T) {
	verificationFailure := errors.New("safety task is not committed")
	verifier, authorizer, committer := &recordedReviewVerifier{err: verificationFailure}, &recordedCommitAuthorizer{}, &recordedResponseCommitter{}
	barrier := newResponseCommitBarrier(verifier, authorizer, committer)
	require.ErrorIs(t, barrier.Commit(context.Background(), validReviewedTeamResponse()), verificationFailure)
	require.Zero(t, authorizer.calls)
	require.Zero(t, committer.calls)

	authorizationFailure := errors.New("capability revoked")
	verifier, authorizer, committer = &recordedReviewVerifier{}, &recordedCommitAuthorizer{err: authorizationFailure}, &recordedResponseCommitter{}
	barrier = newResponseCommitBarrier(verifier, authorizer, committer)
	require.ErrorIs(t, barrier.Commit(context.Background(), validReviewedTeamResponse()), authorizationFailure)
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, 1, authorizer.calls)
	require.Zero(t, committer.calls)
}

func newResponseCommitBarrier(verifier ResponseReviewVerifier, authorizer ResponseCommitAuthorizer, committer ReviewedResponseCommitter) *ResponseCommitBarrier {
	return &ResponseCommitBarrier{Verifier: verifier, Authorizer: authorizer, Committer: committer, AgentID: "response-agent", Capability: "response.commit", Purpose: "commit reviewed team response"}
}

func validReviewedTeamResponse() ReviewedTeamResponse {
	return ReviewedTeamResponse{RunID: "run-a", Scope: runtime.Metadata{TenantID: "tenant-a", UserID: "user-a", SessionID: "session-a"}, Data: []byte(`{"answer":"reviewed"}`)}
}
