package cli

import (
	"testing"

	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/specialist"
	"github.com/Gitlawb/zero/internal/tools"
)

// TestBuildACPSpecialistToolingRegisterIntoAndClose exercises the real
// (non-faked) implementation buildACPSpecialistTooling/agentToolRuntime
// gives internal/acp: build once, re-register into a fresh registry
// (mirroring what a second turn does), then close cleanly.
func TestBuildACPSpecialistToolingRegisterIntoAndClose(t *testing.T) {
	workspaceRoot := t.TempDir()

	tooling, err := buildACPSpecialistTooling("session-1", workspaceRoot, config.ResolvedConfig{})
	if err != nil {
		t.Fatalf("buildACPSpecialistTooling: %v", err)
	}
	if tooling == nil {
		t.Fatal("buildACPSpecialistTooling returned a nil tooling with no error")
	}

	// A second turn's fresh registry - RegisterInto must not rebuild the
	// runtime/swarm, just bind their tool objects into this new registry.
	registry := tools.NewRegistry()
	tooling.RegisterInto(registry)
	if _, ok := registry.Get(specialist.TaskToolName); !ok {
		t.Fatalf("expected %q to be registered after RegisterInto", specialist.TaskToolName)
	}

	if len(tooling.Specialists()) == 0 {
		t.Fatal("expected at least the built-in specialists to be summarized")
	}

	if err := tooling.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestBuildACPSpecialistToolingBuildsIndependentRuntimesPerSession confirms
// two sessions rooted at the same workspace each get their OWN runtime (not
// a shared/cached one keyed only by workspace), which is what makes it safe
// for buildACPSpecialistTooling to pass a session-namespaced swarm mailbox
// dir (`.zero/swarm/<sessionID>`, see its source) - the concurrency
// concern flagged during design (a workspace-only mailbox dir would let
// concurrent sessions collide on the same files) that
// registerSpecialistTools alone (one runtime per process, exec.go's use
// case, no session concept at all) never had to handle. Swarm.Mailbox's
// directory is created lazily on first write, not at construction, so this
// checks independence of the built runtimes rather than asserting
// filesystem paths that may not exist yet.
func TestBuildACPSpecialistToolingBuildsIndependentRuntimesPerSession(t *testing.T) {
	workspaceRoot := t.TempDir()

	a, err := buildACPSpecialistTooling("session-a", workspaceRoot, config.ResolvedConfig{})
	if err != nil {
		t.Fatalf("build session-a: %v", err)
	}
	// Closed explicitly below (not deferred) - the test asserts behavior
	// after that close, and Swarm.Close/specialist.Runtime.Close are
	// documented safe to call more than once, so this isn't a double-close
	// bug, just deliberate ordering.
	b, err := buildACPSpecialistTooling("session-b", workspaceRoot, config.ResolvedConfig{})
	if err != nil {
		t.Fatalf("build session-b: %v", err)
	}
	defer b.Close()

	if a == b {
		t.Fatal("expected two sessions at the same workspace to get independent tooling instances, got the same one")
	}
	// Closing one must not affect the other's ability to register tools.
	if err := a.Close(); err != nil {
		t.Fatalf("close session-a: %v", err)
	}
	registry := tools.NewRegistry()
	b.RegisterInto(registry)
	if _, ok := registry.Get(specialist.TaskToolName); !ok {
		t.Fatal("expected session-b's tooling to still work after session-a was closed")
	}
}
