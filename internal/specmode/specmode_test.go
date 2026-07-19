package specmode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/tools"
)

func TestSaveDraftWritesPlainMarkdownWithCollisionSuffix(t *testing.T) {
	root := t.TempDir()
	now := fixedSpecTime("2026-06-08T10:00:00Z")

	first, err := SaveDraft(SaveOptions{
		WorkspaceRoot: root,
		Title:         "OAuth Redirect",
		Plan:          "# Goal\n\nImplement redirect handling.",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("SaveDraft first returned error: %v", err)
	}
	second, err := SaveDraft(SaveOptions{
		WorkspaceRoot: root,
		Title:         "OAuth Redirect",
		Plan:          "# Goal\n\nImplement redirect handling again.",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("SaveDraft second returned error: %v", err)
	}

	if first.ID != "2026-06-08-oauth-redirect" || second.ID != "2026-06-08-oauth-redirect-2" {
		t.Fatalf("unexpected ids: first=%q second=%q", first.ID, second.ID)
	}
	if first.RelativePath != ".zero/specs/2026-06-08-oauth-redirect.md" {
		t.Fatalf("relative path = %q", first.RelativePath)
	}
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.RelativePath)))
	if err != nil {
		t.Fatalf("read saved spec: %v", err)
	}
	if got := string(content); got != "# Goal\n\nImplement redirect handling.\n" {
		t.Fatalf("saved content = %q", got)
	}
}

func TestSaveDraftContainsAdversarialTitles(t *testing.T) {
	now := fixedSpecTime("2026-06-08T10:00:00Z")
	for _, title := range []string{
		"../../etc/passwd",
		"..",
		"...",
		"/abs",
		"a/../../b",
	} {
		t.Run(title, func(t *testing.T) {
			root := t.TempDir()
			saved, err := SaveDraft(SaveOptions{
				WorkspaceRoot: root,
				Title:         title,
				Plan:          "# Goal\n\nStay contained.",
				Now:           now,
			})
			if err != nil {
				t.Fatalf("SaveDraft returned error: %v", err)
			}
			relative, err := filepath.Rel(root, saved.Path)
			if err != nil {
				t.Fatalf("Rel(%q, %q): %v", root, saved.Path, err)
			}
			relative = filepath.ToSlash(relative)
			if filepath.IsAbs(saved.RelativePath) || !strings.HasPrefix(saved.RelativePath, ".zero/specs/") {
				t.Fatalf("RelativePath escaped spec dir: %q", saved.RelativePath)
			}
			if relative != saved.RelativePath {
				t.Fatalf("saved.Path relative to root = %q, want %q", relative, saved.RelativePath)
			}
			if strings.HasPrefix(relative, "../") || relative == ".." || strings.Contains(relative, "/../") {
				t.Fatalf("saved path contains traversal: %q", relative)
			}
		})
	}
}

func TestSubmitToolSavesSpecAndReturnsReviewControl(t *testing.T) {
	root := t.TempDir()
	tool := NewSubmitTool(root, fixedSpecTime("2026-06-08T11:00:00Z"))

	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  "# Goal\n\nAdd implementation plan.",
	})

	if result.Status != tools.StatusOK {
		t.Fatalf("submit_spec status = %s output=%s", result.Status, result.Output)
	}
	if result.Meta["control"] != ControlSpecReviewRequired {
		t.Fatalf("control meta = %#v", result.Meta)
	}
	if result.Meta["specId"] != "2026-06-08-implementation-plan" {
		t.Fatalf("specId meta = %#v", result.Meta)
	}
	if len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != ".zero/specs/2026-06-08-implementation-plan.md" {
		t.Fatalf("changed files = %#v", result.ChangedFiles)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.ChangedFiles[0]))); err != nil {
		t.Fatalf("expected spec file to exist: %v", err)
	}
	if !strings.Contains(result.Output, result.ChangedFiles[0]) {
		t.Fatalf("output should mention relative path, got %q", result.Output)
	}
}

func TestSubmitToolRejectsInvalidArgs(t *testing.T) {
	result := NewSubmitTool(t.TempDir(), nil).Run(context.Background(), map[string]any{
		"title": "Missing plan",
	})
	if result.Status != tools.StatusError || !strings.Contains(result.Output, "plan is required") {
		t.Fatalf("unexpected invalid arg result: %#v", result)
	}
}

// fakeReviewGate is a minimal ReviewGate for testing NewDeepPlanSubmitTool's
// gate without depending on internal/swarm.
type fakeReviewGate struct {
	collected map[string]bool
}

func (g fakeReviewGate) HasCollected(team string) bool { return g.collected[team] }

func TestDeepPlanSubmitToolBlocksUntilReviewCollected(t *testing.T) {
	gate := fakeReviewGate{collected: map[string]bool{}}
	tool := NewDeepPlanSubmitTool(t.TempDir(), fixedSpecTime("2026-06-08T11:00:00Z"), gate)

	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  "# Goal\n\nAdd implementation plan.",
	})
	if result.Status != tools.StatusError {
		t.Fatalf("expected submit_spec to be blocked before review collected, got status=%s output=%s", result.Status, result.Output)
	}
	if !strings.Contains(result.Output, DeepPlanReviewTeam) {
		t.Fatalf("blocked output should name the required team %q, got %q", DeepPlanReviewTeam, result.Output)
	}
}

const testPlanWithReviewerFindings = "# Goal\n\nAdd implementation plan.\n\n## Reviewer findings and resolutions\n- deep-plan-critic-logic found no issues.\n"

func TestDeepPlanSubmitToolAllowsAfterReviewCollected(t *testing.T) {
	gate := fakeReviewGate{collected: map[string]bool{DeepPlanReviewTeam: true}}
	root := t.TempDir()
	tool := NewDeepPlanSubmitTool(root, fixedSpecTime("2026-06-08T11:00:00Z"), gate)

	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  testPlanWithReviewerFindings,
	})
	if result.Status != tools.StatusOK {
		t.Fatalf("expected submit_spec to succeed once review was collected, got status=%s output=%s", result.Status, result.Output)
	}
}

func TestDeepPlanSubmitToolNilGateDisablesCollectCheck(t *testing.T) {
	root := t.TempDir()
	tool := NewDeepPlanSubmitTool(root, fixedSpecTime("2026-06-08T11:00:00Z"), nil)

	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  testPlanWithReviewerFindings,
	})
	if result.Status != tools.StatusOK {
		t.Fatalf("a nil gate must disable the collect check rather than block forever, got status=%s output=%s", result.Status, result.Output)
	}
}

func TestDeepPlanSubmitToolRequiresReviewerFindingsSection(t *testing.T) {
	gate := fakeReviewGate{collected: map[string]bool{DeepPlanReviewTeam: true}}
	tool := NewDeepPlanSubmitTool(t.TempDir(), fixedSpecTime("2026-06-08T11:00:00Z"), gate)

	// The review round genuinely ran (gate satisfied), but the plan never
	// documents it - this is the exact silent-fold gap found in production
	// (2/10 deep-plan runs did this even though swarm_collect(review) happened).
	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  "# Goal\n\nAdd implementation plan.\n\n## Risks and edge cases\nNone.\n",
	})
	if result.Status != tools.StatusError {
		t.Fatalf("expected submit_spec to be blocked without a reviewer findings section, got status=%s output=%s", result.Status, result.Output)
	}
	if !strings.Contains(result.Output, DeepPlanReviewSectionHeading) {
		t.Fatalf("blocked output should name the required section %q, got %q", DeepPlanReviewSectionHeading, result.Output)
	}
}

func TestDeepPlanSubmitToolReviewerFindingsSectionIsCaseInsensitive(t *testing.T) {
	gate := fakeReviewGate{collected: map[string]bool{DeepPlanReviewTeam: true}}
	tool := NewDeepPlanSubmitTool(t.TempDir(), fixedSpecTime("2026-06-08T11:00:00Z"), gate)

	result := tool.Run(context.Background(), map[string]any{
		"title": "Implementation Plan",
		"plan":  "# Goal\n\n## REVIEWER FINDINGS AND RESOLUTIONS\n- none.\n",
	})
	if result.Status != tools.StatusOK {
		t.Fatalf("heading match must be case-insensitive, got status=%s output=%s", result.Status, result.Output)
	}
}

func TestLoadSpecFileRejectsPathsOutsideSpecDirectory(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "notes.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadSpecFile(root, outside)
	if err == nil {
		t.Fatal("expected LoadSpecFile to reject a path outside .zero/specs")
	}
}

func TestLoadSpecFileRejectsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(".zero/specs/not-a-file.md"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadSpecFile(root, dir)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected non-regular file error, got %v", err)
	}
}

func TestImplementationPromptIncludesReviewContext(t *testing.T) {
	prompt := ImplementationPrompt("# Goal\n\nShip it.", "/repo/.zero/specs/plan.md", "zero_1", "Keep tests focused.")

	for _, want := range []string{
		"Implement the following approved spec:",
		"User note: Keep tests focused.",
		"# Goal\n\nShip it.",
		"Spec file: /repo/.zero/specs/plan.md",
		"Planning session: zero_1",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func fixedSpecTime(value string) func() time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return parsed }
}
