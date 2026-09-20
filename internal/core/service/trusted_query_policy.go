package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

var (
	ErrInvalidTrustedQueryPolicy  = errors.New("trusted query policy is invalid")
	ErrNoTrustedQueryRoute        = errors.New("trusted query policy has no matching route")
	ErrAmbiguousTrustedQueryRoute = errors.New("trusted query policy has ambiguous route")
)

// TrustedQueryIdentity is established by authenticated server-side middleware
// and session-ownership checks. It is deliberately separate from client
// query content: a request body cannot select a tenant or user scope.
type TrustedQueryIdentity struct {
	TenantID  string
	UserID    string
	SessionID string
}

// TrustedQueryRouteRule is a configured deterministic policy rule. Route is
// configuration, not a client or model supplied field. Higher Priority wins;
// equal-priority matches fail closed instead of selecting an arbitrary route.
type TrustedQueryRouteRule struct {
	ID       string
	Priority int
	Phrases  []string
	Route    gateway.Request
}

// TrustedQueryPolicyInput contains the authenticated identity and ordinary
// request content that a deterministic policy may inspect. It intentionally
// exposes no risk, task, tenant, user, or Team-path override.
type TrustedQueryPolicyInput struct {
	Identity   TrustedQueryIdentity
	RunID      string
	Deadline   time.Time
	Question   string
	DocumentID string
}

type compiledTrustedQueryRouteRule struct {
	TrustedQueryRouteRule
	phrases []string
}

// TrustedQueryPolicy constructs RoutedTeamQueryInput only from server-side
// identity plus configured deterministic rules. It performs no external call
// and does not create a Team graph.
type TrustedQueryPolicy struct {
	rules []compiledTrustedQueryRouteRule
}

// NewTrustedQueryPolicy validates and freezes deterministic route rules.
func NewTrustedQueryPolicy(rules []TrustedQueryRouteRule) (*TrustedQueryPolicy, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("%w: at least one route rule is required", ErrInvalidTrustedQueryPolicy)
	}
	seenIDs := make(map[string]struct{}, len(rules))
	compiled := make([]compiledTrustedQueryRouteRule, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.ID) == "" {
			return nil, fmt.Errorf("%w: rule ID is required", ErrInvalidTrustedQueryPolicy)
		}
		if _, exists := seenIDs[rule.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate rule ID %q", ErrInvalidTrustedQueryPolicy, rule.ID)
		}
		seenIDs[rule.ID] = struct{}{}
		if err := gateway.ValidateRequest(rule.Route); err != nil {
			return nil, fmt.Errorf("%w: rule %q: %v", ErrInvalidTrustedQueryPolicy, rule.ID, err)
		}
		phrases := normalizedRulePhrases(rule.Phrases)
		if len(phrases) == 0 {
			return nil, fmt.Errorf("%w: rule %q needs a phrase", ErrInvalidTrustedQueryPolicy, rule.ID)
		}
		compiled = append(compiled, compiledTrustedQueryRouteRule{TrustedQueryRouteRule: rule, phrases: phrases})
	}
	sort.SliceStable(compiled, func(i, j int) bool { return compiled[i].Priority > compiled[j].Priority })
	return &TrustedQueryPolicy{rules: compiled}, nil
}

// Build fails closed unless exactly one highest-priority configured rule
// matches. The returned input is suitable for RoutedTeamQueryService.Start.
func (p *TrustedQueryPolicy) Build(in TrustedQueryPolicyInput) (RoutedTeamQueryInput, error) {
	if p == nil || len(p.rules) == 0 {
		return RoutedTeamQueryInput{}, fmt.Errorf("%w: policy is required", ErrInvalidTrustedQueryPolicy)
	}
	if in.Identity.TenantID == "" || in.Identity.UserID == "" || in.RunID == "" || in.Deadline.IsZero() || strings.TrimSpace(in.Question) == "" {
		return RoutedTeamQueryInput{}, fmt.Errorf("%w: authenticated tenant/user, run ID, deadline, and question are required", ErrInvalidTrustedQueryPolicy)
	}
	question := strings.ToLower(strings.TrimSpace(in.Question))
	var winner *compiledTrustedQueryRouteRule
	for i := range p.rules {
		rule := &p.rules[i]
		if !rule.matches(question) {
			continue
		}
		if winner == nil {
			winner = rule
			continue
		}
		if rule.Priority == winner.Priority {
			return RoutedTeamQueryInput{}, fmt.Errorf("%w: rules %q and %q", ErrAmbiguousTrustedQueryRoute, winner.ID, rule.ID)
		}
		break
	}
	if winner == nil {
		return RoutedTeamQueryInput{}, ErrNoTrustedQueryRoute
	}
	payload, err := json.Marshal(struct {
		Question   string `json:"question"`
		DocumentID string `json:"document_id,omitempty"`
	}{Question: in.Question, DocumentID: in.DocumentID})
	if err != nil {
		return RoutedTeamQueryInput{}, err
	}
	return RoutedTeamQueryInput{
		RunID:    in.RunID,
		Scope:    runtime.Metadata{TenantID: in.Identity.TenantID, UserID: in.Identity.UserID, SessionID: in.Identity.SessionID},
		Deadline: in.Deadline,
		Route:    winner.Route,
		Payload:  payload,
	}, nil
}

func (r compiledTrustedQueryRouteRule) matches(question string) bool {
	for _, phrase := range r.phrases {
		if strings.Contains(question, phrase) {
			return true
		}
	}
	return false
}

func normalizedRulePhrases(phrases []string) []string {
	seen := make(map[string]struct{}, len(phrases))
	result := make([]string, 0, len(phrases))
	for _, phrase := range phrases {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase == "" {
			continue
		}
		if _, exists := seen[phrase]; exists {
			continue
		}
		seen[phrase] = struct{}{}
		result = append(result, phrase)
	}
	return result
}
