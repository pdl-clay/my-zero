package modelregistry

import (
	"fmt"
	"strings"
)

// ForwardedReasoningEffort returns the effort to send on the provider request.
// A model recognized as supporting reasoning-effort tiers — via the curated
// catalog, the embedded models.dev snapshot, or the curated gateway table —
// is gated to its supported set (an unsupported request degrades to the
// model's effective default/first tier, or "" for a non-reasoning model). A
// model recognized by none of those sources forwards the requested value
// unchanged (e.g. a fully custom OpenAI-compatible endpoint), since no
// support claim can be made for it. providerSlug is the caller's own provider
// identifier (e.g. a config.ProviderProfile.CatalogID/Provider string),
// passed through verbatim — see ReasoningEffortsForProvider's doc for why.
func ForwardedReasoningEffort(registry Registry, providerSlug, modelID, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}
	trimmedModel := strings.TrimSpace(modelID)
	if entry, ok := registry.Get(trimmedModel); ok {
		effective := EffectiveReasoningEffort(entry, ReasoningEffort(strings.ToLower(requested)))
		if effective == ReasoningEffortNone {
			return ""
		}
		return string(effective)
	}
	efforts := registry.ReasoningEffortsForProvider(providerSlug, trimmedModel)
	if len(efforts) == 0 {
		return requested
	}
	want := ReasoningEffort(strings.ToLower(requested))
	for _, effort := range efforts {
		if effort == want {
			return requested
		}
	}
	return string(efforts[0])
}

// ReasoningEffortNotice mirrors ForwardedReasoningEffort's gating but returns a
// short user-facing advisory when the requested effort is unsupported (empty
// when the request is honored as-is or no support claim can be made for the
// model at all).
func ReasoningEffortNotice(registry Registry, providerSlug, modelID, requested string) string {
	trimmedModel := strings.TrimSpace(modelID)
	if trimmedModel == "" {
		return ""
	}
	want := ReasoningEffort(strings.TrimSpace(strings.ToLower(requested)))
	if entry, ok := registry.Get(trimmedModel); ok {
		effective := EffectiveReasoningEffort(entry, want)
		if effective == ReasoningEffortNone {
			return fmt.Sprintf("%s does not support reasoning effort; ignoring --reasoning-effort %s", entry.ID, requested)
		}
		if want != "" && effective != want {
			return fmt.Sprintf("reasoning effort %q is not supported by %s; using %s instead", requested, entry.ID, effective)
		}
		return ""
	}
	efforts := registry.ReasoningEffortsForProvider(providerSlug, trimmedModel)
	if len(efforts) == 0 {
		// No support claim can be made for this model - matches
		// ForwardedReasoningEffort's blind-forward behavior for the same case.
		return ""
	}
	for _, effort := range efforts {
		if effort == want {
			return ""
		}
	}
	return fmt.Sprintf("reasoning effort %q is not supported by %s; using %s instead", requested, trimmedModel, efforts[0])
}
