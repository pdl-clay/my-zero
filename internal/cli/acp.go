package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Gitlawb/zero/internal/acp"
	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/sandbox"
	"github.com/Gitlawb/zero/internal/tools"
)

const acpUsage = `zero acp — serve the Agent Client Protocol (ACP) over stdio

Editors that speak ACP (Zed, JetBrains, Neovim, ...) spawn this command and drive
ZERO as a backend over JSON-RPC 2.0 on stdin/stdout. ZERO keeps your provider,
model, and API keys (BYOK); the editor only hosts the conversation thread.

Usage:
  zero acp

Not meant to be run interactively — point your editor's ACP / external-agent
setting at "zero acp".`

// runACP serves ACP over stdio so an editor can drive ZERO's agent core. It
// speaks JSON-RPC 2.0 (newline-delimited JSON) on stdin/stdout; stderr stays free
// for human-readable diagnostics. The session lifecycle maps onto ZERO's own
// session store, and provider/model/keys remain owned by ZERO.
func runACP(args []string, stdout io.Writer, stderr io.Writer, deps appDeps) int {
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "help":
			fmt.Fprintln(stdout, acpUsage)
			return exitSuccess
		default:
			return writeExecUsageError(stderr, fmt.Sprintf("unknown acp flag %q", arg))
		}
	}

	conn := acp.NewConn(deps.stdin, stdout)
	acp.NewAgent(conn, acp.Deps{
		ResolveConfig: deps.resolveConfig,
		// deps.newProvider is wrapped in fillAppDeps to apply the stored API key,
		// so ACP is authenticated for apiKeyStored profiles like every other
		// surface — no ACP-specific credential handling needed.
		NewProvider: deps.newProvider,
		RunAgent:    agent.Run,
		// Build the SCOPED registry + sandbox engine per workspace, exactly like the
		// exec surface, so ACP shell/file tools are confined — never run unconfined.
		BuildWorkspace: func(workspaceRoot string, resolved config.ResolvedConfig) (*tools.Registry, *sandbox.Engine, error) {
			scope, err := sandbox.NewScope(workspaceRoot, resolved.Sandbox.AdditionalWriteRoots)
			if err != nil {
				return nil, nil, err
			}
			engine, err := buildExecSandboxEngine(workspaceRoot, resolved, deps, scope)
			if err != nil {
				return nil, nil, err
			}
			registry := newCoreRegistryScoped(workspaceRoot, scope)
			registerLocalControlTools(registry, workspaceRoot, resolved.LocalControl)
			return registry, engine, nil
		},
		// Gives ACP sessions the same Task-tool/specialist delegation `zero
		// exec` already has (registerSpecialistTools). Built once per
		// session (internal/acp/agent.go's ensureSpecialists), not once per
		// turn like BuildWorkspace above - see buildACPSpecialistTooling.
		BuildSpecialists:     buildACPSpecialistTooling,
		ResolveWorkspaceRoot: acpWorkspaceRootResolver(deps),
		Store:                deps.newSessionStore(),
		AgentInfo:            acp.Implementation{Name: "zero", Version: version},
	})

	ctx, stop := signalContext()
	defer stop()
	if err := conn.Serve(ctx); err != nil && ctx.Err() == nil {
		return writeAppError(stderr, "acp: "+err.Error(), exitCrash)
	}
	return exitSuccess
}

// buildACPSpecialistTooling implements acp.Deps.BuildSpecialists: builds the
// same specialist/Task/swarm runtime `zero exec` registers
// (registerSpecialistTools), namespacing the swarm mailbox directory by
// session id so concurrent ACP sessions rooted at the same workspace don't
// share mailbox files - exec.go doesn't need this since it only ever builds
// one runtime per process. The registry passed to
// registerSpecialistToolsWithBaseDir here is a throwaway: the returned
// *agentToolRuntime's RegisterInto is what actually populates each turn's
// real registry (see internal/acp/agent.go's ensureSpecialists/runTurn) -
// this call only needs *some* registry to satisfy
// specialist.RegisterTools/swarm.RegisterTools' signatures while building
// the runtime the first time.
func buildACPSpecialistTooling(sessionID, workspaceRoot string, resolved config.ResolvedConfig) (acp.SpecialistTooling, error) {
	registry := tools.NewRegistry()
	baseDir := filepath.Join(workspaceRoot, ".zero", "swarm", sessionID)
	runtime, err := registerSpecialistToolsWithBaseDir(registry, workspaceRoot, resolved.Swarm.MaxTeamSize, baseDir)
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

// acpWorkspaceRootResolver validates a client-supplied cwd into a confinement
// root. It reuses exec's resolveWorkspaceRoot (abs+clean, must be an existing
// dir) and additionally rejects the filesystem root and the home directory — an
// editor must not be able to point ZERO's file/shell tools at the whole disk.
func acpWorkspaceRootResolver(deps appDeps) func(string) (string, error) {
	return func(cwd string) (string, error) {
		root, err := resolveWorkspaceRoot(cwd, deps)
		if err != nil {
			return "", err
		}
		if root == filepath.Dir(root) {
			return "", fmt.Errorf("cwd must not be the filesystem root: %s", root)
		}
		if home, herr := os.UserHomeDir(); herr == nil && home != "" && filepath.Clean(home) == root {
			return "", fmt.Errorf("cwd must not be the home directory: %s", root)
		}
		return root, nil
	}
}
