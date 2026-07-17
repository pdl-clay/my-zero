package modelregistry

import (
	"fmt"
	"strings"

	"github.com/Gitlawb/zero/internal/reasoning"
)

// Resolve maps user input to a model: exact id/api-model/alias first, then a
// regex MatchPattern (e.g. "sonnet 4.5" -> the canonical id). It does NOT apply
// deprecation fallbacks — use ResolveWithFallback for that.
func (registry Registry) Resolve(input string) (ModelEntry, bool) {
	if model, ok := registry.Get(input); ok {
		return model, true
	}
	trimmed := strings.TrimSpace(input)
	for _, pattern := range registry.patterns {
		if pattern.re.MatchString(trimmed) {
			return registry.Get(pattern.modelID)
		}
	}
	return ModelEntry{}, false
}

// ResolveWithFallback resolves input (exact/alias/pattern) and, when the resolved
// model is deprecated and declares a fallback, redirects to the replacement. The
// returned notice is non-empty when a redirect happened or a soft-deprecation
// warning applies, so callers can surface it to the user.
func (registry Registry) ResolveWithFallback(input string) (ModelEntry, string, bool) {
	model, ok := registry.Resolve(input)
	if !ok {
		return ModelEntry{}, "", false
	}
	if model.Status == ModelStatusDeprecated && model.Deprecation != nil && strings.TrimSpace(model.Deprecation.FallbackID) != "" {
		if fallback, ok := registry.Get(model.Deprecation.FallbackID); ok {
			notice := strings.TrimSpace(model.Deprecation.WarningMsg)
			if notice == "" {
				notice = fmt.Sprintf("%s is deprecated; using %s instead", model.ID, fallback.ID)
			}
			return fallback, notice, true
		}
	}
	if model.Deprecation != nil && strings.TrimSpace(model.Deprecation.WarningMsg) != "" {
		return model, strings.TrimSpace(model.Deprecation.WarningMsg), true
	}
	return model, "", true
}

// EffectiveReasoningEffort returns the effort to use for a model: the requested
// value if the model supports it, otherwise the model's default (or first
// supported, or none). It resolves the supported set through
// effectiveReasoningEfforts so it sees the same name-based fallback the /effort
// picker uses — the two must never disagree about which tiers a model supports.
func EffectiveReasoningEffort(model ModelEntry, requested ReasoningEffort) ReasoningEffort {
	efforts := effectiveReasoningEfforts(model)
	if requested != "" {
		for _, effort := range efforts {
			if effort == requested {
				return requested
			}
		}
	}
	if model.DefaultReasoningEffort != "" {
		return model.DefaultReasoningEffort
	}
	if len(efforts) > 0 {
		return efforts[0]
	}
	return ReasoningEffortNone
}

// effectiveReasoningEfforts returns a model's supported reasoning efforts,
// trying progressively broader/weaker sources when a more specific one has
// nothing to say: (1) the catalog entry's own explicit ReasoningEfforts, (2)
// the embedded models.dev capability snapshot keyed on this model's own
// provider + api id, (3) the curated gateway-model-id table
// (gatewayReasoningEfforts) for gateway-renamed models the snapshot can't
// recognize, (4) name-based inference (reasoningEffortsForModelName) as a last
// resort. Both the /effort picker (Registry.ReasoningEffortsForProvider) and
// the run-time resolver (EffectiveReasoningEffort) read efforts through this
// single helper, so the picker can never advertise a tier the resolver drops.
func effectiveReasoningEfforts(model ModelEntry) []ReasoningEffort {
	if len(model.ReasoningEfforts) > 0 {
		return model.ReasoningEfforts
	}
	if efforts := reasoningEffortsFromEmbedded(string(model.Provider), model.APIModel); len(efforts) > 0 {
		return efforts
	}
	if efforts := gatewayReasoningEfforts(model.ID); len(efforts) > 0 {
		return efforts
	}
	if efforts := reasoningEffortsForModelName(model.ID); len(efforts) > 0 {
		return efforts
	}
	return reasoningEffortsForModelName(model.APIModel)
}

// reasoningEffortsFromEmbedded looks up a model's reasoning capability in the
// embedded models.dev snapshot (internal/reasoning), keyed by the CALLER'S OWN
// provider identifier — passed through verbatim, never mapped or guessed
// across providers (a gateway's profile id, e.g. "opencode-go", is simply not
// a models.dev provider slug, so this naturally no-ops for gateways/proxies
// and only activates for direct first-party-ish provider profiles). Returns
// nil when the snapshot doesn't cover (providerSlug, apiModel), the model
// doesn't reason, or its only control is a token-budget/toggle rather than a
// discrete effort enum Zero can request.
func reasoningEffortsFromEmbedded(providerSlug, apiModel string) []ReasoningEffort {
	capability, ok := reasoning.Embedded().Lookup(providerSlug, apiModel)
	if !ok || !capability.Supported() {
		return nil
	}
	values := capability.EffortValues()
	if len(values) == 0 {
		return nil
	}
	efforts := make([]ReasoningEffort, 0, len(values))
	for _, value := range values {
		effort := ReasoningEffort(strings.ToLower(strings.TrimSpace(value)))
		if effort != ReasoningEffortNone && ValidReasoningEffort(effort) {
			efforts = append(efforts, effort)
		}
	}
	if len(efforts) == 0 {
		// e.g. groq's qwen/qwen3-32b reports ["none","default"] - neither is a
		// requestable tier, so this model correctly ends up with no controls.
		return nil
	}
	return efforts
}
