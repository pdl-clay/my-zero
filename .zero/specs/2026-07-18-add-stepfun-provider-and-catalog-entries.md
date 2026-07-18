# Goal
Add **StepFun** as a first-class provider in `my-zero`, reusing the existing OpenAI-compatible transport, and register its models in the catalog with the correct `thinking_effort` mapping.

# Relevant files/components
- `internal/config/types.go` — provider kind enum/registry
- `internal/providers/factory.go` — provider factory/creation
- `internal/providers/openai_compat/` — existing OpenAI-compatible adapter to reuse
- `internal/providers/stepfun/provider.go` — new StepFun-specific provider
- `internal/providers/stepfun/provider_test.go` — tests
- `internal/modelregistry/catalog.go` — model catalog entries

# Proposed implementation steps
1. **Add provider kind**
   - In `internal/config/types.go`, add `ProviderKindStepFun` and map it to the StepFun provider in the factory/config switch.

2. **Create StepFun provider**
   - Create `internal/providers/stepfun/provider.go`.
   - Reuse the existing OpenAI-compatible client/transport rather than duplicating HTTP logic.
   - Set `BaseURL()` to `https://api.stepfun.com/v1`.
   - In `Complete()`, translate the generic `thinking_effort` option into StepFun’s request field `reasoning_effort` (`low | `medium` | `high`).
   - Support API key auth via `STEPFUN_API_KEY` env/config; do **not** implement unsupported OAuth flows for StepFun.

3. **Register provider**
   - Update `internal/providers/factory.go` so `ProviderKindStepFun` constructs the StepFun provider using the shared OpenAI-compatible machinery.

4. **Catalog entries**
   - In `internal/modelregistry/catalog.go`, add the following StepFun models with consistent metadata:
     - `stepfun-step-2-16k`
     - `stepfun-step-2-flash`
     - `stepfun-step-3`
     - `stepfun-step-3-flash`
     - `stepfun-step-3.5-flash`
   - For each entry set:
     - `context_window` per StepFun docs
     - `max_output` where documented
     - `pricing` placeholders if exact rates are not yet finalized
     - `thinking_effort` as one of `low`, `medium`, `high` consistent with StepFun’s `reasoning_effort` support
   - Ensure unsupported `thinking_effort` values for a given model are rejected by the catalog/provider layer.

5. **Tests**
   - Add `provider_test.go` covering:
     - factory returns StepFun provider for `ProviderKindStepFun`
     - `BaseURL()` resolves to StepFun API
     - `Complete()` sends `reasoning_effort` when thinking effort is configured
     - invalid/unsupported thinking effort returns a clear error
     - 4xx reasoning-related errors are surfaced without crashing the provider
   - Add catalog tests ensuring all new StepFun models are discoverable and their metadata is valid.

# Tests and verification
- Run `go test ./...`
- Run `make lint`
- Run format/vet: `go fmt ./...` and `go vet ./...`
- Optional: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --enable-only unused,ineffassign,staticcheck ./...`

# Risks and edge cases
- StepFun API drift: `reasoning_effort` behavior may differ across models; catalog must reflect supported values per model.
- Auth mismatch: StepFun uses API keys, not generic OAuth; keep auth handling explicit to avoid leaking unsupported flows into the new provider.
- Empty/uninitialized config: provider must fail fast with a clear message when API key is missing.

# Out of scope
- Streaming / tool use / function calling extensions unless explicitly requested.
- UI changes, docs, or release automation.
- Changes under `third_party/` if any; follow repo rule to leave vendored code untouched.
