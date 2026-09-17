package langfuse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
)

// Client reports traces and scores to Langfuse, with safe no-op fallback.
type Client struct {
	cfg        config.LangfuseConfig
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient builds a Langfuse reporter.
func NewClient(cfg config.LangfuseConfig, logger *zap.Logger) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// ReportQuery sends a lightweight trace payload.
func (c *Client) ReportQuery(ctx context.Context, trace service.QueryTrace) error {
	if !c.cfg.Enabled || c.cfg.PublicKey == "" || c.cfg.SecretKey == "" {
		return nil
	}
	body := map[string]any{
		"batch": []map[string]any{
			{
				"id":        trace.TraceID,
				"type":      "trace-create",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"body": map[string]any{
					"id":          trace.TraceID,
					"name":        "gophermind.query",
					"userId":      trace.UserID,
					"sessionId":   trace.SessionID,
					"input":       trace.Question,
					"output":      trace.Answer,
					"environment": c.cfg.Environment,
					"metadata": map[string]any{
						"request_id":      trace.RequestID,
						"model":           trace.Model,
						"rewritten_query": trace.RewrittenQuery,
						"status":          trace.Status,
						"latency_ms":      trace.Latency.Milliseconds(),
						"citations":       trace.Citations,
						"retrieved_docs":  trace.RetrievedDocs,
					},
				},
			},
		},
	}
	return c.postIngestion(ctx, body)
}

// ReportScore sends one score payload.
func (c *Client) ReportScore(ctx context.Context, requestID string, name string, value float64, comment string) error {
	if !c.cfg.Enabled || c.cfg.PublicKey == "" || c.cfg.SecretKey == "" {
		return nil
	}
	body := map[string]any{
		"batch": []map[string]any{
			{
				"id":        requestID + ":" + name,
				"type":      "score-create",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"body": map[string]any{
					"traceId": requestID,
					"name":    name,
					"value":   value,
					"comment": comment,
				},
			},
		},
	}
	return c.postIngestion(ctx, body)
}

func (c *Client) postIngestion(ctx context.Context, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(c.cfg.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/public/ingestion", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	token := base64.StdEncoding.EncodeToString([]byte(c.cfg.PublicKey + ":" + c.cfg.SecretKey))
	req.Header.Set("Authorization", "Basic "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("langfuse ingest failed", zap.Error(err))
		}
		return nil
	}
	defer resp.Body.Close()
	return nil
}
