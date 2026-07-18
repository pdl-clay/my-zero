package stepfun

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Gitlawb/zero/internal/providers/openai"
	"github.com/Gitlawb/zero/internal/zeroruntime"
)

// Options mirrors the OpenAI-compatible options with StepFun-specific defaults.
type Options struct {
	APIKey            string
	BaseURL           string
	Model             string
	HTTPClient        *http.Client
	UserAgent         string
	MaxTokens         int
	StreamIdleTimeout interface{}
	ParseThinkTags    bool
	AuthHeader        string
	AuthScheme        string
	AuthHeaderValue   string
	CustomHeaders     map[string]string
}

// Provider wraps the OpenAI-compatible provider with StepFun-specific request
// shaping (reasoning_effort mapping) and defaults.
type Provider struct {
	inner *openai.Provider
}

// New creates a StepFun provider. When no BaseURL is supplied it defaults to
// the public StepFun API; the StepPlan product uses a different base
// (https://api.stepfun.ai/step_plan/v1) and is reached either via the
// "stepfun-plan" catalog preset or by setting BaseURL explicitly in the
// profile.
func New(options Options) (*Provider, error) {
	model := strings.TrimSpace(options.Model)
	if model == "" {
		return nil, fmt.Errorf("stepfun provider requires a model")
	}

	baseURL := strings.TrimSpace(options.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.stepfun.ai/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	inner, err := openai.New(openai.Options{
		APIKey:                options.APIKey,
		BaseURL:               baseURL,
		Model:                 model,
		HTTPClient:            options.HTTPClient,
		UserAgent:             options.UserAgent,
		MaxTokens:             options.MaxTokens,
		ParseThinkTags:        options.ParseThinkTags,
		DisablePromptCacheKey: true,
		AuthHeader:            options.AuthHeader,
		AuthScheme:            options.AuthScheme,
		AuthHeaderValue:       options.AuthHeaderValue,
		CustomHeaders:         options.CustomHeaders,
	})
	if err != nil {
		return nil, err
	}

	return &Provider{inner: inner}, nil
}

// StreamCompletion sends one streaming chat completion request with StepFun
// request shaping applied (notably reasoning_effort mapping).
func (p *Provider) StreamCompletion(ctx context.Context, request zeroruntime.CompletionRequest) (<-chan zeroruntime.StreamEvent, error) {
	request = stepfunRequest(request)
	return p.inner.StreamCompletion(ctx, request)
}

// stepfunRequest translates Zero's generic reasoning effort to the StepFun
// "reasoning_effort" enum. StepFun uses the same OpenAI-compatible shape for
// everything else, so only the effort field needs provider-specific mapping.
func stepfunRequest(request zeroruntime.CompletionRequest) zeroruntime.CompletionRequest {
	if request.ReasoningEffort == "" {
		return request
	}
	request.ReasoningEffort = stepfunReasoningEffort(request.ReasoningEffort)
	return request
}

// stepfunReasoningEffort maps Zero's normalized effort to StepFun's accepted
// values. StepFun recognizes "minimal"/"low"/"medium"/"high"; anything else is
// dropped so strict endpoints do not reject the request.
func stepfunReasoningEffort(requested string) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(requested))
	case "none":
		return ""
	default:
		return ""
	}
}
