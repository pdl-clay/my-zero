package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/usage"
	"github.com/Gitlawb/zero/internal/zeroruntime"
)

// The context-fill gauge is empty before any request, then shows
// used/window · pct from the last turn's input tokens against the model window.
func TestContextWindowSegment(t *testing.T) {
	m := newModel(t.Context(), Options{ModelName: "claude-sonnet-4.5"})
	if got := m.contextWindowSegment(); got != "" {
		t.Fatalf("expected empty gauge before any request, got %q", got)
	}
	// 161k latest-step tokens against the 200k window = 81%.
	if _, err := m.usageTracker.Record(usage.RecordInput{
		ModelID: "claude-sonnet-4.5",
		Usage:   zeroruntime.Usage{InputTokens: 160_000, OutputTokens: 1000},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got := plainRender(t, m.contextWindowSegment())
	if !strings.Contains(got, "161K/200K") || !strings.Contains(got, "81%") {
		t.Fatalf("gauge = %q, want 161K/200K · 81%%", got)
	}
}

// TestStatusLineDoesNotDuplicateTokenFigureNextToGauge: the gauge's "used"
// figure and the plain usage segment's token count both read the exact same
// latestUsageTokens value — once the gauge is showing it, the usage segment
// must fall back to cost-only, or the same number renders twice side by
// side in the footer (e.g. "◔ 161K/200K · 81% │ 161K tok").
func TestStatusLineDoesNotDuplicateTokenFigureNextToGauge(t *testing.T) {
	m := newModel(t.Context(), Options{ModelName: "claude-sonnet-4.5"})
	if _, err := m.usageTracker.Record(usage.RecordInput{
		ModelID: "claude-sonnet-4.5",
		Usage:   zeroruntime.Usage{InputTokens: 160_000, OutputTokens: 1000},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status := plainRender(t, m.statusLine(110))
	if !strings.Contains(status, "161K/200K") {
		t.Fatalf("status line = %q, expected the context gauge to render", status)
	}
	if strings.Count(status, "161K") != 1 {
		t.Fatalf("status line = %q, the 161K token figure must appear exactly once, not duplicated next to the gauge", status)
	}
	if strings.Contains(status, "tok") {
		t.Fatalf("status line = %q, the plain token segment should be suppressed once the gauge shows the same figure", status)
	}
}

func TestTokPerSecSegmentEmptyWhenNoMeasuredGeneration(t *testing.T) {
	m := newModel(t.Context(), Options{ModelName: "gpt-4.1"})
	if got := m.tokPerSecSegment(); got != "" {
		t.Fatalf("expected empty tok/s segment before any measured generation, got %q", got)
	}
}

func TestTokPerSecSegmentRendersMeasuredThroughput(t *testing.T) {
	m := newModel(t.Context(), Options{ModelName: "gpt-4.1"})
	m.lastTokensPerSecond = 42
	got := plainRender(t, m.tokPerSecSegment())
	if !strings.Contains(got, "42") || !strings.Contains(got, "tok/s") {
		t.Fatalf("tokPerSecSegment = %q, expected it to contain 42 tok/s", got)
	}
}

func TestAgentUsageMessageAccumulatesTokensPerSecond(t *testing.T) {
	m := newModel(t.Context(), Options{ModelName: "gpt-4.1"})
	m.activeRunID = 1

	updated, _ := m.Update(agentUsageMsg{runID: 1, modelID: "gpt-4.1", usage: zeroruntime.Usage{
		OutputTokens:       100,
		GenerationDuration: 2 * time.Second,
	}})
	m = updated.(model)
	if m.lastTokensPerSecond == 0 {
		t.Fatalf("expected lastTokensPerSecond to be set after first measured generation, got 0")
	}
	first := m.lastTokensPerSecond

	updated, _ = m.Update(agentUsageMsg{runID: 1, modelID: "gpt-4.1", usage: zeroruntime.Usage{
		OutputTokens:       200,
		GenerationDuration: 2 * time.Second,
	}})
	m = updated.(model)
	want := 300.0 / 4.0
	if m.lastTokensPerSecond != want {
		t.Fatalf("after second generation expected %f tok/s, got %f", want, m.lastTokensPerSecond)
	}
	if m.lastTokensPerSecond == first {
		t.Fatalf("expected accumulated value to differ from first measured value")
	}
}
