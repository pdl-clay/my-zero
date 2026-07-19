package agent

import (
	"context"
	"testing"

	"github.com/Gitlawb/zero/internal/tools"
	"github.com/Gitlawb/zero/internal/zeroruntime"
)

// fakeTaskTool stands in for the real Task tool: specialist.Builtins()
// registers spec-compliance-checker generically, so a model can reach it via
// Task/TaskOutput instead of swarm_spawn/swarm_collect. It always returns a
// canned result naming the specialist, mirroring what a real Task/TaskOutput
// completion looks like.
type fakeTaskTool struct{ result string }

func (f fakeTaskTool) Name() string             { return "Task" }
func (f fakeTaskTool) Description() string      { return "fake" }
func (f fakeTaskTool) Parameters() tools.Schema { return tools.Schema{Type: "object"} }
func (f fakeTaskTool) Safety() tools.Safety     { return tools.Safety{Permission: tools.PermissionAllow} }
func (f fakeTaskTool) Run(context.Context, map[string]any) tools.Result {
	return tools.Result{Status: tools.StatusOK, Output: f.result}
}

// fakeComplianceGate scripts a sequence of TeamVerdict responses, consumed one
// per call regardless of the requested team name (the last response repeats
// once exhausted). calls records every requested team name so tests can
// assert the "new team name per retry round" behavior.
type fakeComplianceGate struct {
	responses []fakeComplianceResponse
	calls     []string
	idx       int
}

type fakeComplianceResponse struct {
	text      string
	collected bool
}

func (g *fakeComplianceGate) TeamVerdict(team string) (string, bool) {
	g.calls = append(g.calls, team)
	if len(g.responses) == 0 {
		return "", false
	}
	i := g.idx
	if i >= len(g.responses) {
		i = len(g.responses) - 1
	} else {
		g.idx++
	}
	r := g.responses[i]
	return r.text, r.collected
}

// A spec-impl session whose compliance checker is spawned, collected, and
// reports COMPLIANCE: PASS on the first look must succeed without ever
// needing a fix-and-recheck round.
func TestSpecComplianceGateAcceptsOnPass(t *testing.T) {
	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		textTurn("Implemented and checked compliance."),
	}}
	gate := &fakeComplianceGate{responses: []fakeComplianceResponse{
		{text: "All items resolved.\nCOMPLIANCE: PASS", collected: true},
	}}

	result, err := Run(context.Background(), "implement the approved spec", provider, Options{
		Registry:                tools.NewRegistry(),
		MaxTurns:                10,
		RequireCompletionSignal: true,
		SpecComplianceGate:      gate,
		SpecFilePath:            "/work/.zero/specs/foo.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("a passing compliance check must succeed, got Incomplete (%q)", result.IncompleteReason)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("a pass on the first look must not consume an extra turn, got %d requests", len(provider.requests))
	}
	if len(gate.calls) != 1 || gate.calls[0] != "compliance" {
		t.Fatalf("expected exactly one TeamVerdict(compliance) call, got %v", gate.calls)
	}
}

// Nothing collected yet: the gate must nudge the model to spawn the checker
// and swarm_collect it, quoting the spec file path, rather than accepting the
// model's own claim of completion.
func TestSpecComplianceGateNudgesToSpawnChecker(t *testing.T) {
	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		textTurn("Implemented."),
		textTurn("Spawned the checker, collected, it passed."),
	}}
	gate := &fakeComplianceGate{responses: []fakeComplianceResponse{
		{text: "", collected: false},
		{text: "COMPLIANCE: PASS", collected: true},
	}}

	result, err := Run(context.Background(), "implement the approved spec", provider, Options{
		Registry:                tools.NewRegistry(),
		MaxTurns:                10,
		RequireCompletionSignal: true,
		SpecComplianceGate:      gate,
		SpecFilePath:            "/work/.zero/specs/foo.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("must succeed once the checker passes, got Incomplete (%q)", result.IncompleteReason)
	}
	if !someRequestContains(provider.requests, specComplianceNudgeMarker) {
		t.Fatalf("expected the spec-compliance-checker spawn nudge to be injected")
	}
	if !someRequestContains(provider.requests, "/work/.zero/specs/foo.md") {
		t.Fatalf("expected the nudge to quote the spec file path")
	}
}

// A checker that reports COMPLIANCE: FAIL must force a fix-and-recheck round
// under a NEW team name (team names are not scoped per round in this swarm),
// not be accepted as done.
func TestSpecComplianceGateRequestsFixOnFail(t *testing.T) {
	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		textTurn("Implemented."),
		textTurn("Checked - found issues, fixed them."),
		textTurn("Rechecked, now passing."),
	}}
	gate := &fakeComplianceGate{responses: []fakeComplianceResponse{
		{text: "", collected: false},
		{text: "Used raw HTTP instead of the mandated library (app/foo.rb:12).\nCOMPLIANCE: FAIL", collected: true},
		{text: "COMPLIANCE: PASS", collected: true},
	}}

	result, err := Run(context.Background(), "implement the approved spec", provider, Options{
		Registry:                tools.NewRegistry(),
		MaxTurns:                10,
		RequireCompletionSignal: true,
		SpecComplianceGate:      gate,
		SpecFilePath:            "/work/.zero/specs/foo.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("must succeed once the recheck passes, got Incomplete (%q)", result.IncompleteReason)
	}
	if !someRequestContains(provider.requests, "raw HTTP instead of the mandated library") {
		t.Fatalf("expected the fix nudge to quote the checker's own findings")
	}
	if !someRequestContains(provider.requests, "compliance-2") {
		t.Fatalf("expected the recheck nudge to name a NEW team, got requests: %#v", provider.requests)
	}
	if len(gate.calls) != 3 || gate.calls[0] != "compliance" || gate.calls[1] != "compliance" || gate.calls[2] != "compliance-2" {
		t.Fatalf("expected calls [compliance compliance compliance-2], got %v", gate.calls)
	}
}

// If the budget (maxSpecComplianceRounds) is spent without ever reaching
// COMPLIANCE: PASS, the run must finalize as INCOMPLETE — never a silent
// success, and never an infinite nudge loop.
func TestSpecComplianceGateIncompleteWhenBudgetExhausted(t *testing.T) {
	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		textTurn("Implemented."),
		textTurn("Still checking."),
		textTurn("Still not resolved."),
	}}
	gate := &fakeComplianceGate{responses: []fakeComplianceResponse{
		{text: "", collected: false},
		{text: "COMPLIANCE: FAIL", collected: true},
		{text: "COMPLIANCE: FAIL", collected: true},
	}}

	result, err := Run(context.Background(), "implement the approved spec", provider, Options{
		Registry:                tools.NewRegistry(),
		MaxTurns:                10,
		RequireCompletionSignal: true,
		SpecComplianceGate:      gate,
		SpecFilePath:            "/work/.zero/specs/foo.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Incomplete {
		t.Fatalf("an exhausted budget without a pass must be Incomplete, got success; final=%q", result.FinalAnswer)
	}
	if result.IncompleteReason != "spec compliance check never passed" {
		t.Fatalf("IncompleteReason = %q", result.IncompleteReason)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("expected exactly 3 turns (2 nudges + the final failing look), got %d", len(provider.requests))
	}
}

// The compliance checker may be invoked through the generic Task/TaskOutput
// tools instead of swarm_spawn/swarm_collect (specialist.Builtins() registers
// it generically, not only into the swarm roster) - the gate must recognize
// that path too by scanning the conversation for the specialist's own
// tool-result output, not just the swarm's team-collected state.
func TestSpecComplianceGateAcceptsTaskToolFallback(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(fakeTaskTool{result: "specialist: spec-compliance-checker\nAll items resolved.\nCOMPLIANCE: PASS"})

	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		{
			{Type: zeroruntime.StreamEventToolCallStart, ToolCallID: "call-1", ToolName: "Task"},
			{Type: zeroruntime.StreamEventToolCallDelta, ToolCallID: "call-1", ArgumentsFragment: `{}`},
			{Type: zeroruntime.StreamEventToolCallEnd, ToolCallID: "call-1"},
			{Type: zeroruntime.StreamEventDone},
		},
		{
			{Type: zeroruntime.StreamEventText, Content: "Checked via Task - it passed."},
			{Type: zeroruntime.StreamEventDone},
		},
	}}

	// The swarm gate never sees anything collected (the model never called
	// swarm_spawn/swarm_collect at all) - the run must still succeed via the
	// Task-tool fallback.
	gate := &fakeComplianceGate{responses: []fakeComplianceResponse{{text: "", collected: false}}}

	result, err := Run(context.Background(), "implement the approved spec", provider, Options{
		Registry:                registry,
		MaxTurns:                10,
		RequireCompletionSignal: true,
		SpecComplianceGate:      gate,
		SpecFilePath:            "/work/.zero/specs/foo.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("a Task-tool compliance pass must succeed, got Incomplete (%q)", result.IncompleteReason)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("expected exactly 2 turns (the Task call + its confirmed completion), got %d", len(provider.requests))
	}
}

// With SpecComplianceGate nil (the default for every non-spec-impl session),
// the gate must be completely inert — no behavior change, no extra turns.
func TestSpecComplianceGateInertWhenNil(t *testing.T) {
	provider := &mockProvider{turns: [][]zeroruntime.StreamEvent{
		textTurn("Done. Implemented and it works."),
	}}

	result, err := Run(context.Background(), "implement it", provider, Options{
		Registry:                tools.NewRegistry(),
		MaxTurns:                10,
		RequireCompletionSignal: true,
		// SpecComplianceGate deliberately nil.
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("nil gate must never force Incomplete; reason=%q", result.IncompleteReason)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("nil gate must not add a turn, got %d requests", len(provider.requests))
	}
}
