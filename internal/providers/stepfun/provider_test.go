package stepfun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Gitlawb/zero/internal/zeroruntime"
)

type captureTransport struct {
	request      *http.Request
	requestBody  string
	responseBody string
}

func (transport *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.request = request
	if request.Body != nil {
		body, _ := io.ReadAll(request.Body)
		transport.requestBody = string(body)
		request.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(transport.responseBody)),
		Request:    request,
	}, nil
}

func (transport *captureTransport) body() io.Reader {
	return strings.NewReader(transport.requestBody)
}

func TestNewDefaultsToPublicStepFunAPI(t *testing.T) {
	transport := &captureTransport{responseBody: "data: [DONE]\n\n"}
	provider, err := New(Options{
		APIKey:     "sk-stepfun",
		Model:      "step-3",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := provider.StreamCompletion(context.Background(), zeroruntime.CompletionRequest{
		Messages: []zeroruntime.Message{{Role: zeroruntime.MessageRoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamCompletion() error = %v", err)
	}
	for range stream {
	}

	if transport.request == nil {
		t.Fatal("HTTP client was not used")
	}
	if transport.request.URL.String() != "https://api.stepfun.ai/v1/chat/completions" {
		t.Fatalf("request URL = %q, want https://api.stepfun.ai/v1/chat/completions", transport.request.URL.String())
	}
	if transport.request.Header.Get("Authorization") != "Bearer sk-stepfun" {
		t.Fatalf("Authorization = %q, want bearer token", transport.request.Header.Get("Authorization"))
	}
}

func TestNewUsesCustomBaseURL(t *testing.T) {
	transport := &captureTransport{responseBody: "data: [DONE]\n\n"}
	provider, err := New(Options{
		APIKey:     "sk-stepfun",
		BaseURL:    "https://api.stepfun.ai/step_plan/v1",
		Model:      "step-3",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stream, err := provider.StreamCompletion(context.Background(), zeroruntime.CompletionRequest{
		Messages: []zeroruntime.Message{{Role: zeroruntime.MessageRoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamCompletion() error = %v", err)
	}
	for range stream {
	}

	if transport.request == nil {
		t.Fatal("HTTP client was not used")
	}
	if transport.request.URL.String() != "https://api.stepfun.ai/step_plan/v1/chat/completions" {
		t.Fatalf("request URL = %q, want https://api.stepfun.ai/step_plan/v1/chat/completions", transport.request.URL.String())
	}
}

func TestNewRequiresModel(t *testing.T) {
	_, err := New(Options{APIKey: "sk-stepfun"})
	if err == nil {
		t.Fatal("New() error = nil, want missing model error")
	}
	if !strings.Contains(err.Error(), "stepfun provider requires a model") {
		t.Fatalf("error = %q, want missing model error", err.Error())
	}
}

func TestStreamCompletionMapsReasoningEffort(t *testing.T) {
	cases := []struct {
		input    string
		wantBody string
	}{
		{"low", `"reasoning_effort":"low"`},
		{"medium", `"reasoning_effort":"medium"`},
		{"high", `"reasoning_effort":"high"`},
		{"minimal", `"reasoning_effort":"minimal"`},
		{"none", ""},
		{"bogus", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			transport := &captureTransport{responseBody: "data: [DONE]\n\n"}
			provider, err := New(Options{
				APIKey:     "sk-stepfun",
				Model:      "step-3",
				HTTPClient: &http.Client{Transport: transport},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			stream, err := provider.StreamCompletion(context.Background(), zeroruntime.CompletionRequest{
				Messages:        []zeroruntime.Message{{Role: zeroruntime.MessageRoleUser, Content: "hi"}},
				ReasoningEffort: tc.input,
			})
			if err != nil {
				t.Fatalf("StreamCompletion() error = %v", err)
			}
			for range stream {
			}

			var body map[string]any
			if err := json.NewDecoder(transport.body()).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if tc.wantBody == "" {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort should be omitted for %q: %#v", tc.input, body)
				}
				return
			}
			if body["reasoning_effort"] != tc.input {
				t.Fatalf("reasoning_effort mapping = %#v, want %q for %q", body["reasoning_effort"], tc.input, tc.input)
			}
		})
	}
}

func TestStreamCompletionOmitsPromptCacheKey(t *testing.T) {
	transport := &captureTransport{responseBody: "data: [DONE]\n\n"}
	provider, err := New(Options{
		APIKey:     "sk-stepfun",
		Model:      "step-3",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stream, err := provider.StreamCompletion(context.Background(), zeroruntime.CompletionRequest{
		Messages:       []zeroruntime.Message{{Role: zeroruntime.MessageRoleUser, Content: "hi"}},
		PromptCacheKey: "sess_123",
	})
	if err != nil {
		t.Fatalf("StreamCompletion() error = %v", err)
	}
	for range stream {
	}

	var body map[string]any
	if err := json.NewDecoder(transport.body()).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should be omitted for StepFun: %#v", body)
	}
}
