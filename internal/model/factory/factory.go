package factory

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"gophermind/internal/core/model"
	"gophermind/internal/core/service"
)

// ModelFactory routes model providers and handles fallback policy.
type ModelFactory struct {
	providers map[string]service.ModelProvider
	logger    *zap.Logger
}

func NewModelFactory(openai, kimi, qwen, ollama, bge service.ModelProvider, logger *zap.Logger) *ModelFactory {
	return &ModelFactory{
		providers: map[string]service.ModelProvider{
			"openai": openai,
			"kimi":   kimi,
			"qwen":   qwen,
			"ollama": ollama,
			"bge":    bge,
			"auto":   qwen,
		},
		logger: logger,
	}
}

func (f *ModelFactory) Get(modelType string) (service.ModelProvider, error) {
	if modelType == "" {
		modelType = "auto"
	}
	p, ok := f.providers[modelType]
	if !ok {
		return nil, fmt.Errorf("unknown model type: %s", modelType)
	}
	return p, nil
}

func (f *ModelFactory) GenerateWithFallback(ctx context.Context, modelType string, prompt string) (string, model.Usage, error) {
	first, err := f.Get(modelType)
	if err != nil {
		return "", model.Usage{}, err
	}
	answer, usage, err := first.Generate(ctx, prompt)
	if err == nil {
		return answer, usage, nil
	}
	if !isCloudProvider(first.Name()) {
		return "", model.Usage{}, err
	}
	f.logger.Warn("primary model failed, fallback to ollama", zap.String("provider", first.Name()), zap.Error(err))
	second, getErr := f.Get("ollama")
	if getErr != nil {
		return "", model.Usage{}, err
	}
	return second.Generate(ctx, prompt)
}

func (f *ModelFactory) GenerateStreamWithFallback(ctx context.Context, modelType string, prompt string, onToken func(string) error) (string, model.Usage, error) {
	first, err := f.Get(modelType)
	if err != nil {
		return "", model.Usage{}, err
	}
	answer, usage, err := first.GenerateStream(ctx, prompt, onToken)
	if err == nil {
		return answer, usage, nil
	}
	if !isCloudProvider(first.Name()) {
		return "", model.Usage{}, err
	}
	f.logger.Warn("primary stream model failed, fallback to ollama", zap.String("provider", first.Name()), zap.Error(err))
	second, getErr := f.Get("ollama")
	if getErr != nil {
		return "", model.Usage{}, err
	}
	return second.GenerateStream(ctx, prompt, onToken)
}

func isCloudProvider(name string) bool {
	return name == "openai" || name == "kimi" || name == "qwen"
}
