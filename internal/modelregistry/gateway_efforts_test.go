package modelregistry

import "testing"

func TestGatewayReasoningEfforts(t *testing.T) {
	cases := []struct {
		name string
		want []ReasoningEffort
	}{
		{"minimax-m3", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"MiniMax-M3", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}}, // case-insensitive
		{"qwen3.7-max", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"deepseek-v4-pro", []ReasoningEffort{ReasoningEffortHigh, ReasoningEffortMax}},
		{"deepseek-v4-flash", []ReasoningEffort{ReasoningEffortHigh, ReasoningEffortMax}},
		{"deepseek-r1-distill-llama-70b", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"glm-z1-air", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		// together's raw id carries a vendor/ prefix; bareModelID strips it.
		{"deepseek-ai/DeepSeek-R1", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"unknown-model-xyz", nil},
		{"glm-4.5", nil}, // sibling zai model, not a reasoning entry
	}
	for _, c := range cases {
		got := gatewayReasoningEfforts(c.name)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestReasoningEffortsForProviderCoversGatewayModels(t *testing.T) {
	reg, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		providerSlug string
		modelID      string
		want         []ReasoningEffort
	}{
		// opencode-go: gateway-renamed models, not in modelregistry's curated
		// catalog and not resolvable via the embedded models.dev snapshot
		// (that lookup is keyed on this exact provider slug, which isn't a
		// models.dev slug) - must come from the curated gateway table.
		{"opencode-go", "minimax-m3", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"opencode-go", "deepseek-v4-pro", []ReasoningEffort{ReasoningEffortHigh, ReasoningEffortMax}},
		{"opencode", "deepseek-v4-flash", []ReasoningEffort{ReasoningEffortHigh, ReasoningEffortMax}},
		// groq: direct provider, real api id covered by the embedded snapshot.
		{"groq", "openai/gpt-oss-120b", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		// groq: real api id covered by the embedded snapshot but NOT in the
		// gateway table - not reachable without wiring the embedded lookup in.
		{"groq", "deepseek-r1-distill-llama-70b", []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		// deepseek: direct provider, reasoning:true but no reasoning_options in
		// the snapshot (always-on, no discrete tiers to request) - stays empty.
		{"deepseek", "deepseek-reasoner", nil},
		// Completely unrecognized model/provider combination - stays empty,
		// same as today.
		{"custom-openai-compatible", "custom-model", nil},
	}
	for _, c := range cases {
		got := reg.ReasoningEffortsForProvider(c.providerSlug, c.modelID)
		if len(got) != len(c.want) {
			t.Fatalf("%s/%s: got %v, want %v", c.providerSlug, c.modelID, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s/%s: got %v, want %v", c.providerSlug, c.modelID, got, c.want)
			}
		}
	}
}
