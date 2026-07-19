package specialist

import (
	"testing"

	"github.com/Gitlawb/zero/internal/tools"
)

func executorWithReadOnlyAndWriteSpecialists() Executor {
	return Executor{
		Load: func(LoadOptions) (LoadResult, error) {
			return LoadResult{Specialists: []Manifest{
				{
					Metadata:      Metadata{Name: "explorer", Description: "read-only", Tools: []string{"read-only"}},
					ResolvedTools: []string{"read_file", "list_directory", "grep", "glob"},
				},
				{
					Metadata:      Metadata{Name: "worker", Description: "writes + runs", Tools: []string{"read-only", "edit", "execute", "plan"}},
					ResolvedTools: []string{"read_file", "grep", "write_file", "edit_file", "bash", "update_plan"},
				},
			}}, nil
		},
	}
}

func TestIsReadOnlySpecialist(t *testing.T) {
	ex := executorWithReadOnlyAndWriteSpecialists()
	if !ex.IsReadOnlySpecialist("explorer") {
		t.Fatal("explorer (reads only) should be read-only")
	}
	if ex.IsReadOnlySpecialist("worker") {
		t.Fatal("worker (write_file/edit_file/bash) must NOT be read-only")
	}
	if ex.IsReadOnlySpecialist("does-not-exist") {
		t.Fatal("an unknown specialist must not be treated as read-only")
	}
}

func TestIsNetworkSafeTools(t *testing.T) {
	cases := []struct {
		name  string
		tools []string
		want  bool
	}{
		{"read-only tools are network-safe (subset)", []string{"read_file", "grep", "glob"}, true},
		{"read-only plus web_fetch is network-safe", []string{"read_file", "glob", "grep", "web_fetch"}, true},
		{"read-only plus web_search is network-safe", []string{"read_file", "web_search"}, true},
		{"write_file makes it unsafe", []string{"read_file", "write_file"}, false},
		{"bash makes it unsafe", []string{"web_fetch", "bash"}, false},
		{"empty tool list is unsafe", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNetworkSafeTools(tc.tools); got != tc.want {
				t.Fatalf("IsNetworkSafeTools(%v) = %v, want %v", tc.tools, got, tc.want)
			}
		})
	}
}

func TestTaskToolPermissionForArgs(t *testing.T) {
	tool := NewTaskTool(executorWithReadOnlyAndWriteSpecialists())
	cases := []struct {
		name string
		args map[string]any
		want tools.Permission
	}{
		{"read-only specialist auto-approves", map[string]any{"name": "explorer", "prompt": "look around"}, tools.PermissionAllow},
		{"write-capable specialist still prompts", map[string]any{"name": "worker", "prompt": "edit a file"}, tools.PermissionPrompt},
		{"unknown specialist prompts", map[string]any{"name": "ghost", "prompt": "x"}, tools.PermissionPrompt},
		{"resume always prompts", map[string]any{"resume": "sess_123", "name": "explorer", "prompt": "x"}, tools.PermissionPrompt},
		{"malformed args prompt", map[string]any{}, tools.PermissionPrompt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tool.PermissionForArgs(tc.args); got != tc.want {
				t.Fatalf("PermissionForArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
