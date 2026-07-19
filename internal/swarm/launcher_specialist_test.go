package swarm

import (
	"context"
	"strings"
	"testing"

	"github.com/Gitlawb/zero/internal/specialist"
	"github.com/Gitlawb/zero/internal/streamjson"
)

// TestSpecialistLauncherRunsUnregisteredSwarmAgent guards the fix for the swarm
// catch-22: swarm_spawn only accepts agent types "subagent"/"teammate", but the
// launcher previously looked those up as specialist NAMES (registry has only
// worker/explorer/code-review), so every member failed with "specialist ... not
// found". The launcher now runs the member from an inline manifest built from its
// swarm definition, so an unregistered agent type executes end-to-end.
func TestSpecialistLauncherRunsUnregisteredSwarmAgent(t *testing.T) {
	zero := 0
	var ran bool
	var gotArgs []string
	executor := specialist.Executor{
		BinaryPath:   "/usr/local/bin/zero",
		NewSessionID: func() (string, error) { return "member_task", nil },
		// No "subagent" specialist is registered — the old name-lookup path failed.
		Load: func(specialist.LoadOptions) (specialist.LoadResult, error) {
			return specialist.LoadResult{}, nil
		},
		RunChild: func(ctx context.Context, binaryPath string, args []string, progress func(streamjson.Event)) (specialist.ChildRunResult, error) {
			ran = true
			gotArgs = append([]string(nil), args...)
			return specialist.ChildRunResult{Events: []streamjson.Event{
				{Type: streamjson.EventRunStart, SessionID: "member_task"},
				{Type: streamjson.EventFinal, Text: "member done"},
				{Type: streamjson.EventRunEnd, Status: "success", ExitCode: &zero},
			}}, nil
		},
	}

	handle, err := NewSpecialistLauncher(executor).Launch(context.Background(), MemberSpec{
		ID:           "m1",
		TaskID:       "m1",
		AgentType:    "subagent",
		Team:         "probe",
		Task:         "count files",
		SystemPrompt: "You are a subagent spawned to complete a specific task.\n\nTask: count files",
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	res, err := handle.Wait()
	if err != nil {
		t.Fatalf("member run failed: %v", err)
	}
	if !ran {
		t.Fatal("member never executed (RunChild not called) — the catch-22 is back")
	}
	if !strings.Contains(res.Result, "member done") {
		t.Fatalf("unexpected member result: %q", res.Result)
	}
	if res.SessionID != "member_task" {
		t.Fatalf("session id = %q", res.SessionID)
	}
	// The unregistered swarm agent type titled the child session (the inline
	// manifest, not a registry lookup, drove the run).
	if !strings.Contains(strings.Join(gotArgs, " "), "subagent") {
		t.Fatalf("member args missing agent type: %#v", gotArgs)
	}
}

// TestSpecialistManifestForMemberHonorsDeclaredTools guards a regression found
// while validating the deep-plan checker: specialistManifestForMember used to
// ignore spec.Tools entirely and always pass the generic
// read/write/execute/plan swarmMemberToolGroups to the child, so a Definition
// that declared a narrower grant (e.g. deep-plan-checker's read-only+web_fetch)
// never actually reached the child's --enabled-tools - web_fetch calls failed
// with "not enabled for this run" even though the Definition explicitly
// granted it, and every "restricted" swarm agent type was in practice exactly
// as capable as a normal teammate/subagent. It also ignored spec.NetworkUnsafe
// entirely, so even once Tools correctly included web_fetch, the call still
// hit a sandbox network-approval prompt (denied headlessly) instead of the
// intended autonomy escalation - both halves are required together.
func TestSpecialistManifestForMemberHonorsDeclaredTools(t *testing.T) {
	restricted := specialistManifestForMember(MemberSpec{AgentType: "deep-plan-checker", Tools: []string{"read_file", "web_fetch"}, NetworkUnsafe: true})
	got := strings.Join(restricted.Metadata.Tools, ",")
	if !strings.Contains(got, "web_fetch") {
		t.Fatalf("declared Tools must win over the generic group, got Metadata.Tools=%v", restricted.Metadata.Tools)
	}
	if strings.Contains(got, "write_file") || strings.Contains(got, "bash") {
		t.Fatalf("declared narrow Tools must not be widened with the generic group's write/shell tools, got %v", restricted.Metadata.Tools)
	}
	if !restricted.Metadata.NetworkUnsafe {
		t.Fatal("spec.NetworkUnsafe must reach Metadata.NetworkUnsafe, got false")
	}

	generic := specialistManifestForMember(MemberSpec{AgentType: "teammate"})
	if len(generic.Metadata.Tools) == 0 {
		t.Fatal("a member with no declared Tools must still fall back to swarmMemberToolGroups, got empty")
	}
	if strings.Join(generic.Metadata.Tools, ",") != strings.Join(swarmMemberToolGroups, ",") {
		t.Fatalf("fallback must be exactly swarmMemberToolGroups, got %v", generic.Metadata.Tools)
	}
	if generic.Metadata.NetworkUnsafe {
		t.Fatal("a member with NetworkUnsafe unset must not escalate, got true")
	}
}

// A member whose child exits non-zero (e.g. exit 4 / max-turns) must be reported as
// a FAILURE — otherwise the swarm marks it [done] and the orchestrator trusts
// incomplete work. The failed member keeps its session id (drill-in) and the child
// report rides along as the failure message.
func TestSpecialistLauncherMarksNonZeroExitAsFailed(t *testing.T) {
	four := 4
	executor := specialist.Executor{
		BinaryPath:   "/usr/local/bin/zero",
		NewSessionID: func() (string, error) { return "member_task", nil },
		Load: func(specialist.LoadOptions) (specialist.LoadResult, error) {
			return specialist.LoadResult{}, nil
		},
		RunChild: func(ctx context.Context, binaryPath string, args []string, progress func(streamjson.Event)) (specialist.ChildRunResult, error) {
			return specialist.ChildRunResult{
				Events: []streamjson.Event{
					{Type: streamjson.EventRunStart, SessionID: "member_task"},
					{Type: streamjson.EventFinal, Text: "i could not finish the objective"},
					{Type: streamjson.EventRunEnd, Status: "error", ExitCode: &four},
				},
				ExitCode: 4,
			}, nil
		},
	}

	handle, err := NewSpecialistLauncher(executor).Launch(context.Background(), MemberSpec{
		ID:           "m1",
		TaskID:       "m1",
		AgentType:    "subagent",
		Team:         "probe",
		Task:         "a task too big for the budget",
		SystemPrompt: "You are a subagent spawned to complete a specific task.",
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	res, err := handle.Wait()
	if err == nil {
		t.Fatal("a member that exited non-zero must be reported as FAILED, got nil error")
	}
	if !strings.Contains(err.Error(), "exit 4") {
		t.Fatalf("failure should carry the child report (exit 4), got %q", err.Error())
	}
	if res.SessionID != "member_task" {
		t.Fatalf("a failed member must keep its session id for drill-in, got %q", res.SessionID)
	}
}
