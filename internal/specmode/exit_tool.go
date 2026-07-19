package specmode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Gitlawb/zero/internal/tools"
)

const (
	SubmitToolName            = "submit_spec"
	ControlSpecReviewRequired = "spec_review_required"
)

// ReviewGate reports whether a swarm team's results have actually been
// retrieved (via swarm_collect) at least once in the current run. *swarm.Swarm
// satisfies this structurally; the interface lives here instead so specmode
// does not need to import internal/swarm.
type ReviewGate interface {
	HasCollected(team string) bool
}

// DeepPlanReviewTeam is the swarm_spawn/swarm_collect team name
// DeepPlanSystemPrompt instructs the orchestrator to use for its adversarial
// review round (critics + checker). Kept as a constant so the enforced gate
// below and the prompt text never drift apart.
const DeepPlanReviewTeam = "review"

// DeepPlanReviewSectionHeading is the exact section heading
// DeepPlanSystemPrompt requires the submitted plan to contain, documenting
// what each critic/checker found and how it was resolved (see
// requireReviewSection below). Kept as a constant for the same reason as
// DeepPlanReviewTeam - the prompt text and the enforced check must not drift.
const DeepPlanReviewSectionHeading = "Reviewer findings and resolutions"

type SubmitTool struct {
	workspaceRoot string
	now           func() time.Time
	// requireTeam + gate, when both set, block Run until gate.HasCollected
	// (requireTeam) is true. requireTeam alone (regardless of gate) also
	// enables the DeepPlanReviewSectionHeading content check below - both
	// checks are deep-plan-only concerns, so one field marks "this is the
	// deep-plan submit tool" for both. Empty requireTeam (the plain
	// spec-draft tool) disables both checks — see NewSubmitTool vs
	// NewDeepPlanSubmitTool.
	requireTeam string
	gate        ReviewGate
}

func NewSubmitTool(workspaceRoot string, now func() time.Time) SubmitTool {
	return SubmitTool{workspaceRoot: workspaceRoot, now: now}
}

// NewDeepPlanSubmitTool is NewSubmitTool plus a hard gate: Run refuses to save
// the spec until swarm_collect has actually returned the review team's
// results at least once in this run. This exists because the deep-plan system
// prompt's instruction to always run the review round before submit_spec is
// advisory only - observed in practice to be skippable by a model that
// decides mid-draft it "has enough" and calls submit_spec early. A nil gate
// (e.g. specialist/swarm tools failed to register) disables the check rather
// than deadlocking the run with no way to satisfy it.
func NewDeepPlanSubmitTool(workspaceRoot string, now func() time.Time, gate ReviewGate) SubmitTool {
	return SubmitTool{workspaceRoot: workspaceRoot, now: now, requireTeam: DeepPlanReviewTeam, gate: gate}
}

func (tool SubmitTool) Name() string {
	return SubmitToolName
}

func (tool SubmitTool) Description() string {
	return "Save the completed implementation spec and stop for user review before implementation."
}

func (tool SubmitTool) Parameters() tools.Schema {
	return tools.Schema{
		Type: "object",
		Properties: map[string]tools.PropertySchema{
			"title": {
				Type:        "string",
				Description: "Short 3-6 word title for the spec.",
			},
			"plan": {
				Type:        "string",
				Description: "Complete markdown implementation spec.",
			},
		},
		Required:             []string{"title", "plan"},
		AdditionalProperties: false,
	}
}

func (tool SubmitTool) Safety() tools.Safety {
	return tools.Safety{
		SideEffect: tools.SideEffectWrite,
		Permission: tools.PermissionAllow,
		Reason:     "Writes a spec markdown file under the workspace .zero/specs directory and stops for review.",
	}
}

func (tool SubmitTool) Run(_ context.Context, args map[string]any) tools.Result {
	if tool.requireTeam != "" && tool.gate != nil && !tool.gate.HasCollected(tool.requireTeam) {
		return tools.Result{
			Status: tools.StatusError,
			Output: fmt.Sprintf("Error: submit_spec is blocked until swarm_collect has returned results for team %q. Spawn the review specialists (critic-logic, critic-security, checker) into that team and call swarm_collect(team=%q) before submitting.", tool.requireTeam, tool.requireTeam),
		}
	}
	title, err := requiredString(args, "title")
	if err != nil {
		return tools.Result{Status: tools.StatusError, Output: "Error: Invalid arguments for submit_spec: " + err.Error()}
	}
	plan, err := requiredString(args, "plan")
	if err != nil {
		return tools.Result{Status: tools.StatusError, Output: "Error: Invalid arguments for submit_spec: " + err.Error()}
	}
	// requireTeam doubles as "this is the deep-plan submit tool" (only
	// NewDeepPlanSubmitTool sets it) - deep-plan mode observed in practice to
	// sometimes fold review feedback into the plan silently, without the
	// required section documenting it, even when the review round genuinely
	// ran (the gate above passed). This is a plain substring check, not a
	// judgment of content quality - it only catches the section being absent
	// entirely, matching the exact heading DeepPlanSystemPrompt instructs the
	// model to write.
	if tool.requireTeam != "" && !strings.Contains(strings.ToLower(plan), strings.ToLower(DeepPlanReviewSectionHeading)) {
		return tools.Result{
			Status: tools.StatusError,
			Output: fmt.Sprintf("Error: submit_spec is blocked - the plan is missing a %q section. Add one entry per critic/checker (name each specialist) stating what it found and how you resolved it, or that it found no issues, then submit again.", DeepPlanReviewSectionHeading),
		}
	}
	saved, err := SaveDraft(SaveOptions{
		WorkspaceRoot: tool.workspaceRoot,
		Title:         title,
		Plan:          plan,
		Now:           tool.now,
	})
	if err != nil {
		return tools.Result{Status: tools.StatusError, Output: "Error: Failed to save spec: " + err.Error()}
	}
	output := fmt.Sprintf("Spec saved for review: %s", saved.RelativePath)
	return tools.Result{
		Status: tools.StatusOK,
		Output: output,
		Meta: map[string]string{
			"control":      ControlSpecReviewRequired,
			"specId":       saved.ID,
			"specTitle":    saved.Title,
			"specFilePath": saved.Path,
			"relativePath": saved.RelativePath,
		},
		ChangedFiles: []string{saved.RelativePath},
		Display:      tools.Display{Summary: output, Kind: "file"},
	}
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return text, nil
}
