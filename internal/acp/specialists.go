package acp

import (
	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/tools"
)

// SpecialistTooling is a per-session, lazily-built bundle of specialist
// (Task tool) and swarm tools, built once via Deps.BuildSpecialists and
// cached on the session (see acpSession.ensureSpecialists), then
// re-registered into each turn's freshly-scoped registry via RegisterInto.
//
// This exists because `zero acp` never registered these tools at all before
// - unlike `zero exec`, which calls registerSpecialistTools once per
// invocation (internal/cli/exec.go) - so ACP sessions had no way to
// delegate to a specialist via the Task tool. The interface (rather than a
// concrete type from internal/cli) exists because internal/cli already
// imports internal/acp for the `zero acp` command; internal/acp importing
// internal/cli back would cycle. The concrete implementation
// (*agentToolRuntime) and its builder (buildACPSpecialistTooling) live in
// internal/cli instead and satisfy this interface structurally.
type SpecialistTooling interface {
	// RegisterInto adds this session's specialist/Task/swarm tools to a
	// freshly built registry. Cheap and safe to call every turn - it binds
	// existing runtime state to new tool objects, it does not rebuild that
	// state (no new background manager or swarm goroutines).
	RegisterInto(registry *tools.Registry)
	// Specialists summarizes the available specialists for the
	// orchestrator's system-prompt delegation section (see
	// agent.Options.Specialists / specialistDelegationContext in
	// internal/agent/system_prompt.go).
	Specialists() []agent.SpecialistInfo
	// Close releases the runtime's background manager/swarm goroutines and
	// any tracked temp files. Called once per session, at process shutdown
	// (see Agent.closeSessions) - correct specifically because
	// zero-desktop (the primary ACP client this was built for) spawns one
	// `zero acp` process per session, so "process shutdown" and "session
	// end" coincide. A client that shares one long-lived process across
	// many sessions would need an earlier per-session teardown hook, which
	// does not exist anywhere else in this file either (sessions are never
	// evicted from Agent.sessions before process exit).
	Close() error
}
