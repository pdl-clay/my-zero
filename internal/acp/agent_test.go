package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/agent"
	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/sandbox"
	"github.com/Gitlawb/zero/internal/sessions"
	"github.com/Gitlawb/zero/internal/specmode"
	"github.com/Gitlawb/zero/internal/tools"
	"github.com/Gitlawb/zero/internal/zeroruntime"
)

// fakeProvider streams a canned assistant message and ends the turn — enough to
// drive the real agent.Run loop without a live model.
type fakeProvider struct{ text string }

func (f fakeProvider) StreamCompletion(_ context.Context, _ zeroruntime.CompletionRequest) (<-chan zeroruntime.StreamEvent, error) {
	ch := make(chan zeroruntime.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventText, Content: f.text}
		ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventDone}
	}()
	return ch, nil
}

func testDeps(t *testing.T) Deps {
	t.Helper()
	store := sessions.NewStore(sessions.StoreOptions{RootDir: t.TempDir()})
	return Deps{
		ResolveConfig: func(_ string, o config.Overrides) (config.ResolvedConfig, error) {
			model := "fake-model"
			if o.Provider.Model != "" {
				model = o.Provider.Model
			}
			return config.ResolvedConfig{
				Provider: config.ProviderProfile{Name: "fake", Model: model},
				MaxTurns: 4,
			}, nil
		},
		NewProvider: func(config.ProviderProfile) (zeroruntime.Provider, error) {
			return fakeProvider{text: "Hello from ZERO"}, nil
		},
		RunAgent: agent.Run,
		BuildWorkspace: func(string, config.ResolvedConfig) (*tools.Registry, *sandbox.Engine, error) {
			r := tools.NewRegistry()
			r.Register(tools.NewUpdatePlanTool())
			return r, nil, nil
		},
		ResolveWorkspaceRoot: func(cwd string) (string, error) { return cwd, nil },
		Store:                store,
		AgentInfo:            Implementation{Name: "zero", Version: "test"},
	}
}

// clientHarness wires a client Conn to an Agent over in-memory pipes and collects
// session/update text chunks (updates) plus every raw session/update payload
// (rawUpdates), for tests that need to inspect update kinds other than
// agent_message_chunk (e.g. the spec-draft review-required notification).
type clientHarness struct {
	client     *Conn
	updates    chan string
	rawUpdates chan json.RawMessage
	stop       func()
}

func newHarness(t *testing.T, deps Deps) *clientHarness {
	t.Helper()
	ar, bw := io.Pipe() // agent -> client
	br, aw := io.Pipe() // client -> agent
	agentConn := NewConn(ar, aw)
	client := NewConn(br, bw)
	a := NewAgent(agentConn, deps)

	h := &clientHarness{client: client, updates: make(chan string, 128), rawUpdates: make(chan json.RawMessage, 128)}
	client.HandleNotify(MethodSessionUpdate, func(_ context.Context, params json.RawMessage) {
		var probe struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(params, &probe) != nil {
			return
		}
		select {
		case h.rawUpdates <- params:
		default:
		}
		if probe.Update.SessionUpdate == UpdateAgentMessageChunk {
			h.updates <- probe.Update.Content.Text
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = a.Serve(ctx) }()
	go func() { _ = client.Serve(ctx) }()
	h.stop = func() {
		cancel()
		_ = aw.Close()
		_ = bw.Close()
	}
	return h
}

func TestACPEndToEndPrompt(t *testing.T) {
	h := newHarness(t, testDeps(t))
	defer h.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// initialize
	var initRes InitializeResult
	if err := h.client.Call(ctx, MethodInitialize, InitializeParams{ProtocolVersion: ProtocolVersion}, &initRes); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initRes.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d", initRes.ProtocolVersion)
	}
	if !initRes.AgentCapabilities.LoadSession || !initRes.AgentCapabilities.PromptCapabilities.Image {
		t.Fatalf("unexpected capabilities: %+v", initRes.AgentCapabilities)
	}

	// session/new
	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	if newRes.SessionID == "" {
		t.Fatal("session/new returned empty sessionId")
	}
	if newRes.Modes == nil || newRes.Modes.CurrentModeID != string(agent.PermissionModeAuto) {
		t.Fatalf("expected auto mode, got %+v", newRes.Modes)
	}
	foundSpecDraft := false
	for _, m := range newRes.Modes.AvailableModes {
		if m.ID == string(agent.PermissionModeSpecDraft) {
			foundSpecDraft = true
		}
	}
	if !foundSpecDraft {
		t.Fatalf("expected spec-draft to be an available mode, got %+v", newRes.Modes.AvailableModes)
	}

	// session/prompt
	var promptRes PromptResult
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{
		SessionID: newRes.SessionID,
		Prompt:    []ContentBlock{TextBlock("hi")},
	}, &promptRes); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if promptRes.StopReason != StopEndTurn {
		t.Fatalf("stopReason = %q, want %q", promptRes.StopReason, StopEndTurn)
	}

	// The streamed agent_message_chunk(s) should carry the assistant text.
	if got := drainText(t, h.updates); !strings.Contains(got, "Hello from ZERO") {
		t.Fatalf("streamed text = %q, want it to contain the assistant message", got)
	}
}

func TestACPUnknownSessionPromptErrors(t *testing.T) {
	h := newHarness(t, testDeps(t))
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{SessionID: "nope", Prompt: []ContentBlock{TextBlock("x")}}, &PromptResult{})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestACPSetModeUpdatesSession(t *testing.T) {
	h := newHarness(t, testDeps(t))
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	// auto/ask/spec-draft are accepted.
	if err := h.client.Call(ctx, MethodSessionSetMode, SetSessionModeParams{SessionID: newRes.SessionID, ModeID: string(agent.PermissionModeAsk)}, &SetSessionModeResult{}); err != nil {
		t.Fatalf("set_mode ask: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionSetMode, SetSessionModeParams{SessionID: newRes.SessionID, ModeID: string(agent.PermissionModeSpecDraft)}, &SetSessionModeResult{}); err != nil {
		t.Fatalf("set_mode spec-draft: %v", err)
	}
	// Unsafe must be rejected over ACP — a client can't self-grant no-prompt host access.
	if err := h.client.Call(ctx, MethodSessionSetMode, SetSessionModeParams{SessionID: newRes.SessionID, ModeID: string(agent.PermissionModeUnsafe)}, &SetSessionModeResult{}); err == nil {
		t.Fatal("expected Unsafe mode to be rejected over ACP")
	}
	// An unknown mode must be rejected.
	if err := h.client.Call(ctx, MethodSessionSetMode, SetSessionModeParams{SessionID: newRes.SessionID, ModeID: "bogus"}, &SetSessionModeResult{}); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestACPSetEffortUpdatesSession(t *testing.T) {
	h := newHarness(t, testDeps(t))
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	var setRes ZeroSetEffortResult
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: newRes.SessionID, Effort: "high"}, &setRes); err != nil {
		t.Fatalf("_zero/set_effort high: %v", err)
	}
	if setRes.Effort != "high" {
		t.Fatalf("set_effort result = %q, want %q", setRes.Effort, "high")
	}
	// "auto" clears back to "" (model/provider default).
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: newRes.SessionID, Effort: "auto"}, &setRes); err != nil {
		t.Fatalf("_zero/set_effort auto: %v", err)
	}
	if setRes.Effort != "" {
		t.Fatalf("set_effort auto result = %q, want empty", setRes.Effort)
	}
	// An unknown effort value must be rejected.
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: newRes.SessionID, Effort: "bogus"}, &setRes); err == nil {
		t.Fatal("expected error for unknown reasoning effort")
	}
	// An unknown session must be rejected.
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: "nope", Effort: "high"}, &setRes); err == nil {
		t.Fatal("expected error for unknown session")
	}
}

// TestACPRunTurnForwardsReasoningEffort proves _zero/set_effort actually
// reaches agent.Options.ReasoningEffort on the next turn — not just the
// session's in-memory field.
func TestACPRunTurnForwardsReasoningEffort(t *testing.T) {
	deps := testDeps(t)
	var captured agent.Options
	deps.RunAgent = func(_ context.Context, _ string, _ zeroruntime.Provider, opts agent.Options) (agent.Result, error) {
		captured = opts
		return agent.Result{FinalAnswer: "ok"}, nil
	}

	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}

	// An unrecognized model (testDeps' "fake-model") makes no support claim,
	// so a requested effort forwards as-is.
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: newRes.SessionID, Effort: "high"}, &ZeroSetEffortResult{}); err != nil {
		t.Fatalf("_zero/set_effort: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{SessionID: newRes.SessionID, Prompt: []ContentBlock{TextBlock("hi")}}, &PromptResult{}); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if captured.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want %q (unrecognized model forwards as-is)", captured.ReasoningEffort, "high")
	}

	// Switching to a known reasoning model with an unsupported requested tier
	// must coerce down to that model's effective default, proving the gating
	// (not just a blind pass-through) reaches the provider request.
	if err := h.client.Call(ctx, MethodZeroSetModel, ZeroSetModelParams{SessionID: newRes.SessionID, Model: "claude-sonnet-4.5"}, &ZeroSetModelResult{}); err != nil {
		t.Fatalf("_zero/set_model: %v", err)
	}
	if err := h.client.Call(ctx, MethodZeroSetEffort, ZeroSetEffortParams{SessionID: newRes.SessionID, Effort: "xhigh"}, &ZeroSetEffortResult{}); err != nil {
		t.Fatalf("_zero/set_effort xhigh: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{SessionID: newRes.SessionID, Prompt: []ContentBlock{TextBlock("hi again")}}, &PromptResult{}); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if captured.ReasoningEffort != "medium" {
		t.Fatalf("ReasoningEffort = %q, want %q (claude-sonnet-4.5's default, xhigh unsupported)", captured.ReasoningEffort, "medium")
	}
}

// TestACPRunTurnWiresSandboxAndScopedRegistry proves the sandbox engine and the
// scoped registry from BuildWorkspace actually reach agent.Options — i.e. ACP
// shell tools run confined, not unconfined on the host.
func TestACPRunTurnWiresSandboxAndScopedRegistry(t *testing.T) {
	deps := testDeps(t)
	reg := tools.NewRegistry()
	reg.Register(tools.NewUpdatePlanTool())
	engine := sandbox.NewEngine(sandbox.EngineOptions{WorkspaceRoot: t.TempDir()})
	deps.BuildWorkspace = func(string, config.ResolvedConfig) (*tools.Registry, *sandbox.Engine, error) {
		return reg, engine, nil
	}
	var captured agent.Options
	deps.RunAgent = func(_ context.Context, _ string, _ zeroruntime.Provider, opts agent.Options) (agent.Result, error) {
		captured = opts
		return agent.Result{FinalAnswer: "ok"}, nil
	}

	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{SessionID: newRes.SessionID, Prompt: []ContentBlock{TextBlock("hi")}}, &PromptResult{}); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if captured.Sandbox != engine {
		t.Fatal("sandbox engine was not wired into agent.Options (shell tools would run unconfined)")
	}
	if captured.Registry != reg {
		t.Fatal("scoped registry was not wired into agent.Options")
	}
}

// TestACPRejectsInvalidCwd confirms session/new fails when the workspace root
// resolver rejects the client cwd (e.g. filesystem root).
func TestACPRejectsInvalidCwd(t *testing.T) {
	deps := testDeps(t)
	deps.ResolveWorkspaceRoot = func(string) (string, error) {
		return "", fmt.Errorf("cwd must not be the filesystem root")
	}
	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: "/", McpServers: []McpServer{}}, &NewSessionResult{}); err == nil {
		t.Fatal("expected session/new to reject an invalid cwd")
	}
}

func TestACPPromptWarnsWhenTurnPersistenceFails(t *testing.T) {
	deps := testDeps(t)
	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	metadataPath := filepath.Join(deps.Store.RootDir, newRes.SessionID, sessions.MetadataFile)
	if err := os.Remove(metadataPath); err != nil {
		t.Fatalf("remove metadata: %v", err)
	}

	var promptRes PromptResult
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{
		SessionID: newRes.SessionID,
		Prompt:    []ContentBlock{TextBlock("hi")},
	}, &promptRes); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if promptRes.StopReason != StopEndTurn {
		t.Fatalf("stopReason = %q, want %q", promptRes.StopReason, StopEndTurn)
	}
	got := drainTextUntil(t, h.updates, func(text string) bool {
		return strings.Contains(text, "Hello from ZERO") &&
			strings.Contains(text, "Could not save session history")
	})
	if !strings.Contains(got, "Could not save session history") {
		t.Fatalf("streamed text = %q, want persistence warning", got)
	}
}

func TestACPLoadWarnsWhenHistoryReadFails(t *testing.T) {
	deps := testDeps(t)
	cwd := t.TempDir()
	meta, err := deps.Store.Create(sessions.CreateInput{Title: "ACP session", Cwd: cwd})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	eventsPath := filepath.Join(deps.Store.RootDir, meta.SessionID, sessions.EventsFile)
	if err := os.WriteFile(eventsPath, []byte("{bad json}\n"), 0o600); err != nil {
		t.Fatalf("write corrupt events: %v", err)
	}
	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.client.Call(ctx, MethodSessionLoad, LoadSessionParams{SessionID: meta.SessionID, Cwd: cwd, McpServers: []McpServer{}}, &LoadSessionResult{}); err != nil {
		t.Fatalf("session/load: %v", err)
	}
	got := drainTextUntil(t, h.updates, func(text string) bool {
		return strings.Contains(text, "Could not load session history")
	})
	if !strings.Contains(got, "Could not load session history") {
		t.Fatalf("streamed text = %q, want load warning", got)
	}
}

// submitSpecProvider always calls submit_spec on its first (only) turn,
// mirroring internal/cli/exec_spec_test.go's submitSpecExecProvider, and
// records the tool names offered in the request so a test can confirm
// write/exec tools were NOT advertised in spec-draft mode.
type submitSpecProvider struct {
	requests []zeroruntime.CompletionRequest
}

func (p *submitSpecProvider) StreamCompletion(ctx context.Context, request zeroruntime.CompletionRequest) (<-chan zeroruntime.StreamEvent, error) {
	p.requests = append(p.requests, request)
	arguments, _ := json.Marshal(map[string]string{
		"title": "Add caching layer",
		"plan":  "# Goal\n\nAdd a caching layer in front of the DB.",
	})
	ch := make(chan zeroruntime.StreamEvent, 4)
	select {
	case <-ctx.Done():
		close(ch)
		return ch, ctx.Err()
	case ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventToolCallStart, ToolCallID: "call-1", ToolName: specmode.SubmitToolName}:
	}
	ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventToolCallDelta, ToolCallID: "call-1", ArgumentsFragment: string(arguments)}
	ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventToolCallEnd, ToolCallID: "call-1"}
	ch <- zeroruntime.StreamEvent{Type: zeroruntime.StreamEventDone}
	close(ch)
	return ch, nil
}

func (p *submitSpecProvider) toolNames() []string {
	if len(p.requests) == 0 {
		return nil
	}
	var names []string
	for _, tool := range p.requests[0].Tools {
		names = append(names, tool.Name)
	}
	return names
}

// fakeWriteTool is a stand-in write-capable tool so the test can prove it is
// filtered out of what the model sees in spec-draft mode (only read-only +
// ask_user + submit_spec should be advertised - see
// agent.toolAdvertisedInSpecDraft).
type fakeWriteTool struct{}

func (fakeWriteTool) Name() string             { return "write_file" }
func (fakeWriteTool) Description() string      { return "writes a file" }
func (fakeWriteTool) Parameters() tools.Schema { return tools.Schema{Type: "object"} }
func (fakeWriteTool) Safety() tools.Safety {
	return tools.Safety{SideEffect: tools.SideEffectWrite, Permission: tools.PermissionPrompt}
}
func (fakeWriteTool) Run(context.Context, map[string]any) tools.Result { return tools.Result{} }

// TestACPSpecDraftModeRegistersSubmitSpecAndEmitsReviewUpdate proves the ACP
// surface now matches `zero exec --use-spec`: switching a session to
// spec-draft mode registers submit_spec, hides write-capable tools from what
// the model is offered, and a submit_spec call emits the
// "_zero/spec_review_required" session/update (not just a silent end_turn)
// carrying the saved spec's identity.
func TestACPSpecDraftModeRegistersSubmitSpecAndEmitsReviewUpdate(t *testing.T) {
	deps := testDeps(t)
	provider := &submitSpecProvider{}
	deps.NewProvider = func(config.ProviderProfile) (zeroruntime.Provider, error) { return provider, nil }
	deps.BuildWorkspace = func(string, config.ResolvedConfig) (*tools.Registry, *sandbox.Engine, error) {
		r := tools.NewRegistry()
		r.Register(tools.NewUpdatePlanTool())
		r.Register(fakeWriteTool{})
		return r, nil, nil
	}

	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionSetMode, SetSessionModeParams{SessionID: newRes.SessionID, ModeID: string(agent.PermissionModeSpecDraft)}, &SetSessionModeResult{}); err != nil {
		t.Fatalf("set_mode spec-draft: %v", err)
	}

	var promptRes PromptResult
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{
		SessionID: newRes.SessionID,
		Prompt:    []ContentBlock{TextBlock("plan the caching layer")},
	}, &promptRes); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}

	names := provider.toolNames()
	hasSubmitSpec, hasWriteFile := false, false
	for _, n := range names {
		if n == specmode.SubmitToolName {
			hasSubmitSpec = true
		}
		if n == "write_file" {
			hasWriteFile = true
		}
	}
	if !hasSubmitSpec {
		t.Fatalf("expected submit_spec to be advertised in spec-draft mode, got tools %v", names)
	}
	if hasWriteFile {
		t.Fatalf("expected write_file to be hidden in spec-draft mode, got tools %v", names)
	}

	update := findRawUpdate(t, h.rawUpdates, UpdateSpecReviewRequired)
	var review SpecReviewRequiredUpdate
	if err := json.Unmarshal(update, &review); err != nil {
		t.Fatalf("unmarshal spec review update: %v", err)
	}
	if review.Title != "Add caching layer" {
		t.Fatalf("review title = %q, want %q", review.Title, "Add caching layer")
	}
	if review.SpecID == "" || review.FilePath == "" || review.RelativePath == "" {
		t.Fatalf("expected spec identity fields to be populated, got %+v", review)
	}
}

// findRawUpdate drains rawUpdates for a session/update whose "update.sessionUpdate"
// matches kind, returning its "update" field. Fails the test if none arrives.
func findRawUpdate(t *testing.T, ch <-chan json.RawMessage, kind string) json.RawMessage {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-ch:
			var envelope struct {
				Update json.RawMessage `json:"update"`
			}
			if json.Unmarshal(raw, &envelope) != nil {
				continue
			}
			var probe struct {
				SessionUpdate string `json:"sessionUpdate"`
			}
			if json.Unmarshal(envelope.Update, &probe) != nil {
				continue
			}
			if probe.SessionUpdate == kind {
				return envelope.Update
			}
		case <-deadline:
			t.Fatalf("timed out waiting for session/update kind %q", kind)
			return nil
		}
	}
}

// fakeSpecialistTooling is a minimal SpecialistTooling used to prove
// ensureSpecialists builds it ONCE per session, not once per turn (the
// resource/goroutine leak Patch 2 fixes - see BuildWorkspace's per-turn doc
// comment), and that Close() runs once when Agent.Serve returns.
type fakeSpecialistTooling struct {
	mu            sync.Mutex
	registerCount int
	closed        bool
}

func (f *fakeSpecialistTooling) RegisterInto(*tools.Registry) {
	f.mu.Lock()
	f.registerCount++
	f.mu.Unlock()
}

func (f *fakeSpecialistTooling) Specialists() []agent.SpecialistInfo {
	return []agent.SpecialistInfo{{Name: "reviewer", WhenToUse: "code review"}}
}

func (f *fakeSpecialistTooling) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeSpecialistTooling) ReviewGate() specmode.ReviewGate {
	return nil
}

func (f *fakeSpecialistTooling) ComplianceGate() agent.ComplianceGate {
	return nil
}

func (f *fakeSpecialistTooling) snapshot() (registerCount int, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registerCount, f.closed
}

// TestACPBuildsSpecialistToolingOnceReusesAcrossTurnsAndClosesOnShutdown is
// the core regression test for Patch 2: BuildSpecialists must run at most
// once per session (multiple turns must reuse the cached tooling via
// RegisterInto, not rebuild the goroutine-bearing runtime each time), the
// summary must reach agent.Options.Specialists so the system prompt's
// delegation section appears, and Close() must run once Agent.Serve
// returns - the only per-session teardown hook that exists.
func TestACPBuildsSpecialistToolingOnceReusesAcrossTurnsAndClosesOnShutdown(t *testing.T) {
	deps := testDeps(t)
	tooling := &fakeSpecialistTooling{}
	var buildMu sync.Mutex
	buildCalls := 0
	deps.BuildSpecialists = func(sessionID, workspaceRoot string, resolved config.ResolvedConfig) (SpecialistTooling, error) {
		buildMu.Lock()
		buildCalls++
		buildMu.Unlock()
		return tooling, nil
	}
	var lastOpts agent.Options
	deps.RunAgent = func(_ context.Context, _ string, _ zeroruntime.Provider, opts agent.Options) (agent.Result, error) {
		lastOpts = opts
		return agent.Result{FinalAnswer: "ok"}, nil
	}

	h := newHarness(t, deps)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{
			SessionID: newRes.SessionID,
			Prompt:    []ContentBlock{TextBlock("hi")},
		}, &PromptResult{}); err != nil {
			t.Fatalf("session/prompt %d: %v", i, err)
		}
	}

	buildMu.Lock()
	gotBuildCalls := buildCalls
	buildMu.Unlock()
	if gotBuildCalls != 1 {
		t.Fatalf("BuildSpecialists called %d times across 2 turns, want exactly 1 (rebuilding a goroutine-bearing runtime every turn is the leak this patch fixes)", gotBuildCalls)
	}
	if registerCount, _ := tooling.snapshot(); registerCount != 2 {
		t.Fatalf("RegisterInto called %d times across 2 turns, want 2 (once per turn, reusing the cached runtime)", registerCount)
	}
	if len(lastOpts.Specialists) != 1 || lastOpts.Specialists[0].Name != "reviewer" {
		t.Fatalf("agent.Options.Specialists = %+v, want the fake tooling's summary", lastOpts.Specialists)
	}

	// Triggers ctx cancel + pipe close -> conn.Serve returns -> Agent.Serve's
	// deferred closeSessions runs.
	h.stop()
	deadline := time.After(2 * time.Second)
	for {
		if _, closed := tooling.snapshot(); closed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("specialist tooling was not closed after Agent.Serve returned")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestACPNoBuildSpecialistsMeansNoSpecialists confirms a nil
// Deps.BuildSpecialists (the default before Patch 2, and still valid for a
// deps literal that doesn't want Task-tool support) degrades cleanly: no
// specialists are advertised, and no panic/error occurs.
func TestACPNoBuildSpecialistsMeansNoSpecialists(t *testing.T) {
	deps := testDeps(t) // testDeps leaves BuildSpecialists nil
	var lastOpts agent.Options
	deps.RunAgent = func(_ context.Context, _ string, _ zeroruntime.Provider, opts agent.Options) (agent.Result, error) {
		lastOpts = opts
		return agent.Result{FinalAnswer: "ok"}, nil
	}
	h := newHarness(t, deps)
	defer h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newRes NewSessionResult
	if err := h.client.Call(ctx, MethodSessionNew, NewSessionParams{Cwd: t.TempDir(), McpServers: []McpServer{}}, &newRes); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	if err := h.client.Call(ctx, MethodSessionPrompt, PromptParams{SessionID: newRes.SessionID, Prompt: []ContentBlock{TextBlock("hi")}}, &PromptResult{}); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if len(lastOpts.Specialists) != 0 {
		t.Fatalf("expected no specialists with a nil BuildSpecialists, got %+v", lastOpts.Specialists)
	}
}

// drainText collects streamed chunks for a short window and concatenates them.
func drainText(t *testing.T, ch <-chan string) string {
	t.Helper()
	return drainTextUntil(t, ch, func(text string) bool {
		return strings.Contains(text, "Hello from ZERO")
	})
}

func drainTextUntil(t *testing.T, ch <-chan string, done func(string) bool) string {
	t.Helper()
	var b strings.Builder
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-ch:
			b.WriteString(s)
			if done(b.String()) {
				return b.String()
			}
		case <-deadline:
			return b.String()
		}
	}
}
