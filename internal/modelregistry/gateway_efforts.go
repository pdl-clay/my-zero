package modelregistry

// gatewayModelEfforts is the explicit, hand-curated mapping from a gateway- or
// vendor-renamed model's bare id (see bareModelID) to the reasoning-effort
// tiers it supports. This is deliberately NOT a substring/heuristic match
// (see internal/providers/factory.go's modelMayEmitThinkTags for that
// different, broader-tolerance use case: whether to parse raw <think> tags —
// false positives there are cheap, false positives here would advertise a
// request param a model may reject) — every key here is an exact, verified
// model id. Sourced from internal/providermodelcatalog/catalog.go's
// `Reasoning: true` flags and "reasoning model"/"fast reasoning model"
// description tags (that package already imports modelregistry, so the
// mapping can't live there without an import cycle — keep the two tables in
// sync by hand). Update this table whenever a new reasoning model shows up in
// a curated provider model list there.
var gatewayModelEfforts = map[string][]ReasoningEffort{
	// opencode-go-anthropic-compatible curated list.
	"minimax-m3":   {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},
	"minimax-m2.7": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},
	"qwen3.7-plus": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},
	"qwen3.7-max":  {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// opencode / opencode-go default models. Not reachable through the
	// embedded models.dev snapshot: that lookup is keyed on the CALLING
	// profile's own provider id ("opencode"/"opencode-go"), which isn't a
	// models.dev provider slug, even though these particular ids happen to
	// also appear under the snapshot's "deepseek" slug.
	"deepseek-v4-pro":   {ReasoningEffortHigh, ReasoningEffortMax},
	"deepseek-v4-flash": {ReasoningEffortHigh, ReasoningEffortMax},

	// groq curated list — not covered by the embedded snapshot (verified: the
	// groq slug there has no "deepseek-r1-distill-llama-70b" entry).
	"deepseek-r1-distill-llama-70b": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// zai / zai-cn curated list.
	"glm-z1-air": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// venice curated list.
	"deepseek-r1-671b": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// mistral curated list.
	"magistral-medium-latest": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// xai curated list — not covered by the embedded snapshot (verified: the
	// xai slug there only has newer grok-4.x ids, no grok-3-mini).
	"grok-3-mini": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},

	// together curated list — the raw id is "deepseek-ai/DeepSeek-R1";
	// bareModelID strips the vendor/ prefix down to "deepseek-r1" (distinct
	// from "deepseek-r1-671b"/"deepseek-r1-distill-llama-70b" above — exact
	// match only, no substring collision risk).
	"deepseek-r1": {ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},
}

// gatewayReasoningEfforts returns the curated reasoning-effort tiers for a
// gateway- or vendor-renamed model id, or nil when modelID isn't in
// gatewayModelEfforts.
func gatewayReasoningEfforts(modelID string) []ReasoningEffort {
	efforts, ok := gatewayModelEfforts[bareModelID(modelID)]
	if !ok {
		return nil
	}
	return append([]ReasoningEffort{}, efforts...)
}
