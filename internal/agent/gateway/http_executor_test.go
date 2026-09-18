package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPExecutorAuthorizesImmediatelyBeforeRequest(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called.Store(true)
		if request.Method != http.MethodPost || request.URL.Path != "/search" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"query":"fever"}` {
			t.Errorf("body = %s", body)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		_, _ = writer.Write([]byte(`{"hits":[]}`))
	}))
	defer server.Close()

	executor := newHTTPExecutorForTest(t, []Grant{{AgentID: "evidence", Capability: "knowledge.search"}}, server.Client())
	if err := executor.Register(HTTPManifest{ID: "search", Capability: "knowledge.search", Method: http.MethodPost, URL: server.URL + "/search", MaxRequestBytes: 64, MaxResponseBytes: 64, Timeout: time.Second}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	result, err := executor.Execute(context.Background(), HTTPInvocation{AgentID: "evidence", Executable: "search", Purpose: "evidence retrieval", Body: []byte(`{"query":"fever"}`)})
	if err != nil || !called.Load() || result.StatusCode != http.StatusOK || string(result.Body) != `{"hits":[]}` {
		t.Fatalf("Execute() = %#v, %v, called=%v", result, err, called.Load())
	}
}

func TestHTTPExecutorDenialPreventsRequest(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer server.Close()
	executor := newHTTPExecutorForTest(t, nil, server.Client())
	if err := executor.Register(HTTPManifest{ID: "write", Capability: "patient.write", Method: http.MethodPost, URL: server.URL, MaxRequestBytes: 64, MaxResponseBytes: 64, Timeout: time.Second}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	_, err := executor.Execute(context.Background(), HTTPInvocation{AgentID: "untrusted", Executable: "write", Purpose: "test", Body: []byte(`{}`)})
	if !errors.Is(err, ErrCapabilityDenied) || called.Load() {
		t.Fatalf("Execute() error = %v, called=%v", err, called.Load())
	}
}

func TestHTTPExecutorRejectsBoundViolationsAndInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/large":
			_, _ = writer.Write([]byte(`{"too":"large"}`))
		default:
			_, _ = writer.Write([]byte("not-json"))
		}
	}))
	defer server.Close()
	executor := newHTTPExecutorForTest(t, []Grant{{AgentID: "agent", Capability: "external.call"}}, server.Client())
	for _, manifest := range []HTTPManifest{
		{ID: "invalid", Capability: "external.call", Method: http.MethodPost, URL: server.URL + "/invalid", MaxRequestBytes: 64, MaxResponseBytes: 64, Timeout: time.Second},
		{ID: "large", Capability: "external.call", Method: http.MethodPost, URL: server.URL + "/large", MaxRequestBytes: 64, MaxResponseBytes: 2, Timeout: time.Second},
		{ID: "request", Capability: "external.call", Method: http.MethodPost, URL: server.URL + "/invalid", MaxRequestBytes: 2, MaxResponseBytes: 64, Timeout: time.Second},
	} {
		if err := executor.Register(manifest); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	_, err := executor.Execute(context.Background(), HTTPInvocation{AgentID: "agent", Executable: "invalid", Purpose: "test", Body: []byte(`{}`)})
	if !errors.Is(err, ErrInvalidHTTPResponse) {
		t.Fatalf("invalid response error = %v", err)
	}
	_, err = executor.Execute(context.Background(), HTTPInvocation{AgentID: "agent", Executable: "large", Purpose: "test", Body: []byte(`{}`)})
	if !errors.Is(err, ErrHTTPResponseTooLarge) {
		t.Fatalf("large response error = %v", err)
	}
	_, err = executor.Execute(context.Background(), HTTPInvocation{AgentID: "agent", Executable: "request", Purpose: "test", Body: []byte(`{"a":1}`)})
	if !errors.Is(err, ErrHTTPRequestTooLarge) {
		t.Fatalf("large request error = %v", err)
	}
}

func TestHTTPExecutorDoesNotFollowRedirects(t *testing.T) {
	var redirectedTo atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectedTo.Store(true) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	executor := newHTTPExecutorForTest(t, []Grant{{AgentID: "agent", Capability: "external.call"}}, redirector.Client())
	if err := executor.Register(HTTPManifest{ID: "redirect", Capability: "external.call", Method: http.MethodGet, URL: redirector.URL, MaxRequestBytes: 0, MaxResponseBytes: 64, Timeout: time.Second}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	_, err := executor.Execute(context.Background(), HTTPInvocation{AgentID: "agent", Executable: "redirect", Purpose: "test"})
	if !errors.Is(err, ErrUnexpectedHTTPStatus) || redirectedTo.Load() {
		t.Fatalf("Execute() error = %v, redirect target called=%v", err, redirectedTo.Load())
	}
}

func TestHTTPExecutorPropagatesParentCancellation(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer server.Close()
	executor := newHTTPExecutorForTest(t, []Grant{{AgentID: "agent", Capability: "external.call"}}, server.Client())
	if err := executor.Register(HTTPManifest{ID: "cancel", Capability: "external.call", Method: http.MethodGet, URL: server.URL, MaxRequestBytes: 0, MaxResponseBytes: 64, Timeout: time.Second}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := executor.Execute(ctx, HTTPInvocation{AgentID: "agent", Executable: "cancel", Purpose: "test"})
	if !errors.Is(err, context.Canceled) || called.Load() {
		t.Fatalf("Execute() error = %v, called=%v", err, called.Load())
	}
}

func newHTTPExecutorForTest(t *testing.T, grants []Grant, client *http.Client) *HTTPExecutor {
	t.Helper()
	policy, err := NewCapabilityPolicy(grants)
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	executor, err := NewHTTPExecutor(policy, client)
	if err != nil {
		t.Fatalf("NewHTTPExecutor() error = %v", err)
	}
	return executor
}
