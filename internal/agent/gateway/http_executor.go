package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gophermind/internal/agent/runtime"
)

var (
	ErrInvalidHTTPManifest   = errors.New("gateway HTTP manifest is invalid")
	ErrDuplicateHTTPTarget   = errors.New("gateway HTTP target is already registered")
	ErrUnknownHTTPTarget     = errors.New("gateway HTTP target is not registered")
	ErrInvalidHTTPInvocation = errors.New("gateway HTTP invocation is invalid")
	ErrHTTPRequestTooLarge   = errors.New("gateway HTTP request exceeds limit")
	ErrHTTPResponseTooLarge  = errors.New("gateway HTTP response exceeds limit")
	ErrInvalidHTTPResponse   = errors.New("gateway HTTP response is not valid JSON")
	ErrUnexpectedHTTPStatus  = errors.New("gateway HTTP response has unexpected status")
)

// HTTPManifest is a static, allow-listed external endpoint. Invocation input
// can supply only a JSON request body; it cannot select a URL, method, or
// headers. This keeps policy authorization tied to a known side-effect target.
type HTTPManifest struct {
	ID               string
	Capability       runtime.Capability
	Method           string
	URL              string
	MaxRequestBytes  int
	MaxResponseBytes int
	Timeout          time.Duration
}

// HTTPInvocation carries trusted authorization information and untrusted JSON
// request data for a registered HTTP endpoint.
type HTTPInvocation struct {
	Scope      runtime.Metadata
	AgentID    string
	Executable string
	Purpose    string
	Body       json.RawMessage
}

// HTTPResult is the bounded, structured response from a registered endpoint.
type HTTPResult struct {
	Manifest   HTTPManifest
	StatusCode int
	Body       json.RawMessage
}

// HTTPExecutor calls only registered static endpoints. It deliberately uses a
// no-redirect client so a redirect cannot turn an authorized endpoint into an
// unreviewed side effect.
type HTTPExecutor struct {
	policy *CapabilityPolicy
	client *http.Client
	mu     sync.RWMutex
	items  map[string]HTTPManifest
}

// NewHTTPExecutor creates a constrained external executor. The supplied
// client is cloned and redirects are disabled for every registered target.
func NewHTTPExecutor(policy *CapabilityPolicy, client *http.Client) (*HTTPExecutor, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: capability policy is required", ErrInvalidHTTPInvocation)
	}
	if client == nil {
		client = http.DefaultClient
	}
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPExecutor{policy: policy, client: &noRedirect, items: make(map[string]HTTPManifest)}, nil
}

// Register adds a fixed HTTP endpoint. Registration grants no capability;
// Execute always authorizes again immediately before issuing the request.
func (e *HTTPExecutor) Register(manifest HTTPManifest) error {
	if err := validateHTTPManifest(manifest); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.items[manifest.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateHTTPTarget, manifest.ID)
	}
	e.items[manifest.ID] = manifest
	return nil
}

// Execute builds a request from a static manifest and validates all data before
// re-authorizing the precise capability immediately before client.Do, which is
// the external side effect.
func (e *HTTPExecutor) Execute(ctx context.Context, invocation HTTPInvocation) (HTTPResult, error) {
	if invocation.AgentID == "" || invocation.Executable == "" || invocation.Purpose == "" || (len(invocation.Body) > 0 && !json.Valid(invocation.Body)) {
		return HTTPResult{}, ErrInvalidHTTPInvocation
	}
	e.mu.RLock()
	manifest, ok := e.items[invocation.Executable]
	e.mu.RUnlock()
	if !ok {
		return HTTPResult{}, fmt.Errorf("%w: %q", ErrUnknownHTTPTarget, invocation.Executable)
	}
	if len(invocation.Body) > manifest.MaxRequestBytes {
		return HTTPResult{}, ErrHTTPRequestTooLarge
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, manifest.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, manifest.Method, manifest.URL, strings.NewReader(string(invocation.Body)))
	if err != nil {
		return HTTPResult{}, fmt.Errorf("%w: %v", ErrInvalidHTTPInvocation, err)
	}
	request.Header.Set("Accept", "application/json")
	if len(invocation.Body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}

	if err := e.policy.Authorize(AccessRequest{
		Scope:      invocation.Scope,
		AgentID:    invocation.AgentID,
		Capability: manifest.Capability,
		Purpose:    invocation.Purpose,
	}); err != nil {
		return HTTPResult{}, err
	}
	response, err := e.client.Do(request)
	if err != nil {
		return HTTPResult{}, err
	}
	defer response.Body.Close()
	body, err := readBoundedResponse(response.Body, manifest.MaxResponseBytes)
	if err != nil {
		return HTTPResult{}, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return HTTPResult{}, fmt.Errorf("%w: %s", ErrUnexpectedHTTPStatus, response.Status)
	}
	if len(body) > 0 && !json.Valid(body) {
		return HTTPResult{}, ErrInvalidHTTPResponse
	}
	return HTTPResult{Manifest: manifest, StatusCode: response.StatusCode, Body: json.RawMessage(body)}, nil
}

func validateHTTPManifest(manifest HTTPManifest) error {
	if manifest.ID == "" || manifest.Capability == "" || manifest.MaxRequestBytes < 0 || manifest.MaxResponseBytes <= 0 || manifest.Timeout <= 0 || (manifest.Method != http.MethodGet && manifest.Method != http.MethodPost) {
		return ErrInvalidHTTPManifest
	}
	parsed, err := url.Parse(manifest.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ErrInvalidHTTPManifest
	}
	return nil
}

func readBoundedResponse(reader io.Reader, maximum int) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maximum {
		return nil, ErrHTTPResponseTooLarge
	}
	return body, nil
}
