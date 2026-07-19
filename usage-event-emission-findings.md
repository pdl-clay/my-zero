# Findings: Usage Event Emission Paths — `zero exec` (JSON / stream-json)

Scope: paths that carry usage token counts from the agent runtime to the CLI
writer and out through JSON or stream-json output.  All references are
`file:line` and type names only.

---

## 1. Canonical usage field source

| Type | File:line | Fields used |
|---|---|---|
| `zeroruntime.Usage` | `internal/zeroruntime/types.go:134` | `PromptTokens`, `CompletionTokens`, `CachedInputTokens`, `CacheWriteTokens`, `ReasoningTokens` |
| `agent.Usage` | `internal/agent/types.go:17` | type alias to `zeroruntime.Usage` |
| Helper | `internal/zeroruntime/types.go:145` | `TotalTokens()` = `EffectiveInputTokens() + EffectiveOutputTokens()` |
| Helper | `internal/zeroruntime/types.go:149` | `EffectiveInputTokens()` = `PromptTokens` |
| Helper | `internal/zeroruntime/types.go:160` | `EffectiveOutputTokens()` = `CompletionTokens` |

`PromptTokens` / `CompletionTokens` are the primary counters used in JSON keys;
`TotalTokens()` is the derived sum used only in the stream-json `Event` path.

---

## 2. Callback plumbing in `runExec`

| Item | File:line | Notes |
|---|---|---|
| `runExec` entry | `internal/cli/exec.go:148` | Top-level orchestrator for `zero exec`. |
| `currentModel` | `internal/cli/exec.go:398` | Tracks the effective model ID for usage attribution. |
| `OnUsage` callback | `internal/cli/exec.go:654` | `w.OnUsage = func(u agent.Usage) { writer.usage(u) }`. |
| Session record call | `internal/cli/exec.go:661` | `usage.EventUsagePayload(u)` then writes `"model"` key only when `options.allowEscalation` is true. |
| Model key rule | `internal/cli/exec.go:661` | `"model"` is omitted unless `allowEscalation` is set. |

---

## 3. JSON output writer path

### 3.1 `execOutputJSON` (single JSON blob)

| Item | File:line | Notes |
|---|---|---|
| `execOutputJSON` struct | `internal/cli/exec_writer.go:49` | `Output` and `Err` fields; no usage token fields. |
| `writer.usage()` impl (JSON) | `internal/cli/exec_writer.go:261` | Emits top-level `prompt_tokens` / `completion_tokens` / `total_tokens` with `PromptTokens`, `CompletionTokens`, `TotalTokens`. |
| Tag style | `internal/cli/exec_writer.go:261` | `map[string]any` literal — no dedicated Go struct with JSON tags for the JSON blob usage payload. |

### 3.2 `execOutputStreamJSON` (stream-json)

| Item | File:line | Notes |
|---|---|---|
| `execOutputStreamJSON` struct | `internal/cli/exec_writer.go:72` | `Usage` sub-struct typed `streamjson.Event`. |
| `streamjson.Event` | `internal/streamjson/streamjson.go:62` | JSON-tagged fields: `PromptTokens int`, `CompletionTokens int`, `TotalTokens int`. |
| `FormatEvent` | `internal/streamjson/streamjson.go:138` | Serializes an `EventType` to JSON. |
| Event types | `internal/streamjson/streamjson.go:22–37` | Includes `RunEnd` event used for the usage line. |

### 3.3 Usage payload builder (session persistence)

| Item | File:line | Notes |
|---|---|---|
| `EventUsagePayload` | `internal/usage/report.go:38` | Builds `map[string]any` with `promptTokens`, `completionTokens`, `totalTokens` from `EffectiveInputTokens`/`EffectiveOutputTokens`/`TotalTokens`. |
| Session record call site | `internal/cli/exec.go:661` | Caller adds `"model"` only when `options.allowEscalation`. |

---

## 4. Test coverage of the emission paths

| Test | File:line | What it exercises |
|---|---|---|
| `TestRunExecJSONOutputsNDJSONEvents` | `internal/cli/exec_test.go:789` | Verifies the JSON output writer path emits NDJSON events with token counts. |
| `TestRunExecAttributesUsageToEscalatedModel` | `internal/cli/exec_test.go:1476` | Verifies `"model"` is present in the usage event when escalation is allowed. |
| `TestRunExecUsageOmitsModelKeyWithoutEscalationFlag` | `internal/cli/exec_test.go:1707` | Verifies `"model"` is absent when escalation is not allowed. |
| `TestRunExecStreamJSONOutputsRunEndAndRecordsSession` | `internal/cli/exec_protocol_test.go:304` | Verifies stream-json `RunEnd` event records usage tokens and persists the session. |

---

## 5. Model-ID / provider catalog (availability for attribution)

| Type / symbol | File:line | Notes |
|---|---|---|
| `modelregistry.Registry` | `internal/modelregistry/catalog.go` | Default provider catalog. |
| `modelregistry.ModelEntry` | `internal/modelregistry/models.go` | Holds provider kind, capabilities, deprecation / upgrade rules. |
| `modelregistry.DefaultRegistry` | `internal/modelregistry/catalog.go` | Default singleton. |
| `modelregistry.ResolveID` | `internal/modelregistry/catalog.go` | Resolves a model ID to its entry. |
| Provider kinds | `internal/modelregistry/models.go` | `openai`, `stepfun`, etc. |
| Capabilities / rules | `internal/modelregistry/models.go` | Capabilities, deprecation, upgrade aliases. |

`currentModel` in `runExec` is the authoritative value for usage attribution;
`EventUsagePayload` does not set `"model"` itself — the caller at
`internal/cli/exec.go:661` is responsible.

---

## 6. Open gaps / ambiguities

- The JSON blob (`execOutputJSON`) and stream-json (`execOutputStreamJSON`)
  paths both expose `prompt_tokens` / `completion_tokens` / `total_tokens`, but
  the JSON blob uses an ad-hoc `map[string]any` while stream-json uses the
  dedicated `streamjson.Event` struct.  There is no single canonical "usage
  event struct with JSON tags" shared by both outputs — callers must keep the
  two representations in sync manually.
- `EventUsagePayload` (session persistence) and `writer.usage()` (output) both
  derive token counts from `zeroruntime.Usage`, so the JSON key names must not
  drift; the current implementation keeps them aligned via the helper method
  names (`EffectiveInputTokens` / `EffectiveOutputTokens` / `TotalTokens`).
