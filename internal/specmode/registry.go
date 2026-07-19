package specmode

import (
	"time"

	"github.com/Gitlawb/zero/internal/tools"
)

func RegisterDraftTools(registry *tools.Registry, workspaceRoot string, now func() time.Time) {
	if registry == nil {
		return
	}
	registry.Register(NewSubmitTool(workspaceRoot, now))
}

// RegisterDeepPlanTools is RegisterDraftTools for deep-plan mode: submit_spec
// additionally refuses to run until the review team has been collected at
// least once (see NewDeepPlanSubmitTool). A nil gate disables that check.
func RegisterDeepPlanTools(registry *tools.Registry, workspaceRoot string, now func() time.Time, gate ReviewGate) {
	if registry == nil {
		return
	}
	registry.Register(NewDeepPlanSubmitTool(workspaceRoot, now, gate))
}
