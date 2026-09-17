package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
)

// OpenAIProvider implements OpenAI-compatible chat completion calls.
type OpenAIProvider struct {
	providerName string
	baseURL      string
	apiKey       string
	model        string
	httpClient   *http.Client
	logger       *zap.Logger

	mu          sync.Mutex
	failures    int
	lastFailure time.Time
}

func NewOpenAIProvider(cfg config.ModelConfig, logger *zap.Logger) *OpenAIProvider {
	return newOpenAICompatibleProvider("openai", cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, logger)
}

// NewKimiProvider reuses the same OpenAI-compatible protocol with Kimi endpoint/model.
func NewKimiProvider(cfg config.ModelConfig, logger *zap.Logger) *OpenAIProvider {
	return newOpenAICompatibleProvider("kimi", cfg.KimiBaseURL, cfg.KimiAPIKey, cfg.KimiModel, logger)
}

// NewQwenProvider reuses the same OpenAI-compatible protocol with DashScope endpoint/model.
func NewQwenProvider(cfg config.ModelConfig, logger *zap.Logger) *OpenAIProvider {
	return newOpenAICompatibleProvider("qwen", cfg.QwenBaseURL, cfg.QwenAPIKey, cfg.QwenModel, logger)
}

func newOpenAICompatibleProvider(name, baseURL, apiKey, modelName string, logger *zap.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		providerName: name,
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		model:        modelName,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
		logger: logger,
	}
}

func (p *OpenAIProvider) Name() string { return p.providerName }

func (p *OpenAIProvider) Generate(ctx context.Context, prompt string) (string, model.Usage, error) {
	if p.isCircuitOpen() {
		return "", model.Usage{}, errCircuitOpen
	}

	if p.apiKey == "" {
		answer := fmt.Sprintf("[%s-mock] %s", p.Name(), prompt)
		return answer, model.Usage{Provider: p.Name(), InputTokens: len(prompt) / 4, OutputTokens: len(answer) / 4}, nil
	}

	var answer string
	err := withRetry(ctx, 2, 500*time.Millisecond, func(callCtx context.Context) error {
		reqBody := map[string]any{
			"model": p.model,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		}
		// Kimi k2.5 does not allow overriding temperature.
		if p.shouldSendTemperature() {
			reqBody["temperature"] = 0.2
		}
		// Keep latency predictable for kimi-k2.5 in sync query path.
		if p.shouldDisableKimiThinking() {
			reqBody["thinking"] = map[string]string{"type": "disabled"}
		}
		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.baseURL+"/chat/completions", buf)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			p.markFailure()
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			p.markFailure()
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			msg := strings.TrimSpace(string(errBody))
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			return fmt.Errorf("%s status %d: %s", p.Name(), resp.StatusCode, msg)
		}

		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			p.markFailure()
			return err
		}
		if len(parsed.Choices) == 0 {
			p.markFailure()
			return fmt.Errorf("%s empty choices", p.Name())
		}
		answer = parsed.Choices[0].Message.Content
		return nil
	})
	if err != nil {
		return "", model.Usage{}, err
	}
	p.resetFailure()
	return answer, model.Usage{Provider: p.Name(), InputTokens: len(prompt) / 4, OutputTokens: len(answer) / 4}, nil
}

func (p *OpenAIProvider) GenerateStream(ctx context.Context, prompt string, onToken func(string) error) (string, model.Usage, error) {
	answer, usage, err := p.Generate(ctx, prompt)
	if err != nil {
		return "", model.Usage{}, err
	}
	for _, token := range splitToTokens(answer) {
		if err := onToken(token); err != nil {
			return "", model.Usage{}, err
		}
	}
	return answer, usage, nil
}

func (p *OpenAIProvider) isCircuitOpen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failures >= 5 && time.Since(p.lastFailure) < 60*time.Second
}

func (p *OpenAIProvider) markFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	p.lastFailure = time.Now()
}

func (p *OpenAIProvider) resetFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures = 0
}

func (p *OpenAIProvider) shouldSendTemperature() bool {
	return !(p.Name() == "kimi" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.model)), "kimi-k2.5"))
}

func (p *OpenAIProvider) shouldDisableKimiThinking() bool {
	return p.Name() == "kimi" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.model)), "kimi-k2.5")
}
