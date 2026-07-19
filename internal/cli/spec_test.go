package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/sessions"
	"github.com/Gitlawb/zero/internal/specmode"
)

func TestRunSpecApproveCreatesImplementationSession(t *testing.T) {
	store, deps, draft := seedSpecDraft(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"spec", "approve", draft.SpecID, "--comment", "Keep changes focused.", "--json"}, &stdout, &stderr, deps)

	if exitCode != exitSuccess {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	var payload specCommandResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode approve json: %v\n%s", err, stdout.String())
	}
	if payload.Status != string(sessions.SpecStatusApproved) || payload.ImplementationSessionID == "" {
		t.Fatalf("unexpected approve payload: %#v", payload)
	}
	updatedDraft, err := store.Get(draft.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedDraft.SpecStatus != sessions.SpecStatusApproved || updatedDraft.SpecImplSessionID != payload.ImplementationSessionID {
		t.Fatalf("draft metadata not updated: %#v", updatedDraft)
	}
	impl, err := store.Get(payload.ImplementationSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if impl == nil || impl.SessionKind != sessions.SessionKindSpecImpl || impl.SpecSourceSessionID != draft.SessionID {
		t.Fatalf("unexpected impl session: %#v", impl)
	}
	events, err := store.ReadEvents(payload.ImplementationSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != sessions.EventMessage {
		t.Fatalf("implementation events = %#v", events)
	}
	if !strings.Contains(string(events[0].Payload), "Keep changes focused.") || !strings.Contains(string(events[0].Payload), "# Goal") {
		t.Fatalf("implementation prompt missing approved spec context: %s", string(events[0].Payload))
	}
}

func TestRunSpecApproveIsIdempotent(t *testing.T) {
	store, deps, draft := seedSpecDraft(t)

	var first bytes.Buffer
	if exitCode := runWithDeps([]string{"spec", "approve", draft.SessionID, "--json"}, &first, &bytes.Buffer{}, deps); exitCode != exitSuccess {
		t.Fatalf("first approve exit = %d", exitCode)
	}
	var firstPayload specCommandResult
	if err := json.Unmarshal(first.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	if exitCode := runWithDeps([]string{"spec", "approve", draft.SpecID, "--json"}, &second, &bytes.Buffer{}, deps); exitCode != exitSuccess {
		t.Fatalf("second approve exit = %d", exitCode)
	}
	var secondPayload specCommandResult
	if err := json.Unmarshal(second.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if secondPayload.ImplementationSessionID != firstPayload.ImplementationSessionID {
		t.Fatalf("approve created a second implementation session: first=%q second=%q", firstPayload.ImplementationSessionID, secondPayload.ImplementationSessionID)
	}
	items, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	implCount := 0
	for _, item := range items {
		if item.SessionKind == sessions.SessionKindSpecImpl {
			implCount++
		}
	}
	if implCount != 1 {
		t.Fatalf("implementation session count = %d, want 1", implCount)
	}
}

func TestRunSpecRejectMarksDraftRejected(t *testing.T) {
	store, deps, draft := seedSpecDraft(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"spec", "reject", draft.SpecID, "--reason=Needs narrower scope"}, &stdout, &stderr, deps)

	if exitCode != exitSuccess {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	updated, err := store.Get(draft.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SpecStatus != sessions.SpecStatusRejected || updated.SpecRejectReason != "Needs narrower scope" {
		t.Fatalf("draft metadata not rejected: %#v", updated)
	}
	if !strings.Contains(stdout.String(), "Spec rejected.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSpecShowDiffPrintsUnifiedDiff(t *testing.T) {
	store, _, draft := seedSpecDraft(t)
	rejectDraft(t, store, draft)

	rejected, err := store.Get(draft.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	redraft, err := store.Create(sessions.CreateInput{
		SessionKind:         sessions.SessionKindSpecDraft,
		Title:               "Review flow",
		Cwd:                 draft.Cwd,
		ModelID:             "test-model",
		Provider:            "test",
		SpecID:              rejected.SpecID + "-v2",
		SpecFilePath:        rejected.SpecFilePath + ".v2",
		SpecStatus:          sessions.SpecStatusDraft,
		SpecDraftModelID:    "test-model",
		SpecDraftReasoning:  "high",
		SpecSourceSessionID: rejected.SessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	redraftPath, err := specmode.ResolveSpecFilePath(redraft.Cwd, redraft.SpecFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(redraftPath, []byte("# Goal\n\nAdd review flow.\nWith new detail."), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := specCommandOptions{diff: true}
	exitCode := runSpecShowDiff(store, redraft, options, &stdout, &stderr)

	if exitCode != exitSuccess {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "@@") || !strings.Contains(stdout.String(), "With new detail.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSpecShowDiffRequiresDraft(t *testing.T) {
	// runSpecShowDiff's first check is purely on SessionKind, not SpecStatus -
	// approving a draft keeps it SessionKindSpecDraft (only a separate
	// SessionKindSpecImpl session is created), so this must construct a
	// genuinely different session kind to exercise the check at all.
	_, _, draft := seedSpecDraft(t)
	implSession := draft
	implSession.SessionKind = sessions.SessionKindSpecImpl

	var stderr bytes.Buffer
	options := specCommandOptions{diff: true}
	exitCode := runSpecShowDiff(nil, implSession, options, &bytes.Buffer{}, &stderr)

	if exitCode != exitUsage {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--diff is only valid for a spec-draft session") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSpecShowDiffRequiresRejectedPredecessor(t *testing.T) {
	store, _, draft := seedSpecDraft(t)

	options := specCommandOptions{diff: true}
	var stderr bytes.Buffer
	exitCode := runSpecShowDiff(store, draft, options, &bytes.Buffer{}, &stderr)

	if exitCode != exitUsage {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no previous rejected draft found for spec") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSpecShowDiffEmptyDiff(t *testing.T) {
	store, _, draft := seedSpecDraft(t)
	rejectDraft(t, store, draft)

	rejected, err := store.Get(draft.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	redraft, err := store.Create(sessions.CreateInput{
		SessionKind:         sessions.SessionKindSpecDraft,
		Title:               "Review flow",
		Cwd:                 draft.Cwd,
		ModelID:             "test-model",
		Provider:            "test",
		SpecID:              rejected.SpecID + "-v2",
		SpecFilePath:        rejected.SpecFilePath + ".v2",
		SpecStatus:          sessions.SpecStatusDraft,
		SpecDraftModelID:    "test-model",
		SpecDraftReasoning:  "high",
		SpecSourceSessionID: rejected.SessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	redraftPath, err := specmode.ResolveSpecFilePath(redraft.Cwd, redraft.SpecFilePath)
	if err != nil {
		t.Fatal(err)
	}
	// specmode.SaveDraft (used by seedSpecDraft for the "previous" file below)
	// always appends a trailing newline - match it exactly here so identical
	// spec content really is byte-identical and yields an empty diff.
	if err := os.WriteFile(redraftPath, []byte("# Goal\n\nAdd review flow.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	options := specCommandOptions{diff: true}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runSpecShowDiff(store, redraft, options, &stdout, &stderr)

	if exitCode != exitSuccess {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func rejectDraft(t *testing.T, store *sessions.Store, draft sessions.Metadata) {
	t.Helper()
	_, _, err := store.RecordSpec(draft.SessionID, sessions.RecordSpecInput{
		SpecID:              draft.SpecID,
		SpecFilePath:        draft.SpecFilePath,
		SpecStatus:          sessions.SpecStatusRejected,
		SpecDraftModelID:    draft.SpecDraftModelID,
		SpecDraftReasoning:  draft.SpecDraftReasoning,
		SpecRejectReason:    "needs work",
		SpecSourceSessionID: draft.SessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseSpecRejectsCommandSpecificFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "comment on show",
			args: []string{"show", "draft", "--comment", "ship it"},
			want: "--comment is only valid for zero spec approve",
		},
		{
			name: "reason on approve",
			args: []string{"approve", "draft", "--reason", "too broad"},
			want: "--reason is only valid for zero spec reject",
		},
		{
			name: "diff on approve",
			args: []string{"approve", "draft", "--diff"},
			want: "--diff is only valid for zero spec show",
		},
		{
			name: "diff on reject",
			args: []string{"reject", "draft", "--diff"},
			want: "--diff is only valid for zero spec show",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := parseSpecArgs(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunSpecShowPrintsSavedDraft(t *testing.T) {
	_, deps, draft := seedSpecDraft(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"spec", "show", draft.SessionID}, &stdout, &stderr, deps)

	if exitCode != exitSuccess {
		t.Fatalf("exit = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# Goal") || !strings.Contains(stdout.String(), "Add review flow.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func seedSpecDraft(t *testing.T) (*sessions.Store, appDeps, sessions.Metadata) {
	t.Helper()
	workspaceRoot := t.TempDir()
	saved, err := specmode.SaveDraft(specmode.SaveOptions{
		WorkspaceRoot: workspaceRoot,
		Title:         "Review Flow",
		Plan:          "# Goal\n\nAdd review flow.",
		Now:           fixedCLISpecTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	storeRoot := t.TempDir()
	store := sessions.NewStore(sessions.StoreOptions{RootDir: storeRoot, Now: fixedCLISpecTime})
	draft, err := store.Create(sessions.CreateInput{
		SessionKind:        sessions.SessionKindSpecDraft,
		Title:              "Review flow",
		Cwd:                workspaceRoot,
		ModelID:            "test-model",
		Provider:           "test",
		SpecID:             saved.ID,
		SpecFilePath:       saved.Path,
		SpecStatus:         sessions.SpecStatusDraft,
		SpecDraftModelID:   "test-model",
		SpecDraftReasoning: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := appDeps{
		newSessionStore: func() *sessions.Store {
			return store
		},
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(storeRoot)
	})
	return store, deps, draft
}

func fixedCLISpecTime() time.Time {
	return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
}
