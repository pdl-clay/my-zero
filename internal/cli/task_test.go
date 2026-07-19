package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gitlawb/zero/internal/background"
)

func TestRunTaskListEmpty(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"task", "list"}, &stdout, &stderr, appDeps{})
	if exitCode != exitSuccess {
		t.Fatalf("task list exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "No background tasks.") {
		t.Fatalf("task list output = %q, want empty message", got)
	}
}

func TestRunTaskListText(t *testing.T) {
	dataHome := t.TempDir()
	manager, err := background.NewManager(filepath.Join(dataHome, "zero", "background"))
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	const taskID = "task-1"
	// Register returns the task's output file path (for wiring up the
	// subprocess's stdout redirect), not the task ID - callers already know
	// the ID, since they're the ones who chose it via RegisterInput.TaskID.
	if _, err := manager.Register(background.RegisterInput{
		TaskID:         taskID,
		Type:           "specialist",
		SpecialistName: "worker",
		Description:    "Run audit",
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := manager.SetPID(taskID, 1234); err != nil {
		t.Fatalf("SetPID returned error: %v", err)
	}
	if err := manager.UpdateStatus(taskID, background.StatusRunning, 0); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)

	// `zero task list` builds its own fresh Manager (runTaskList ->
	// background.NewManagerWithOptions), which reloads task-1 from disk.
	// loadTasks/normalizeLoadedTask unconditionally reaps any persisted
	// "running" status on load, since a freshly constructed Manager has no
	// in-memory continuity with whatever process originally registered it -
	// it can't tell a genuinely still-running task from an orphaned one, so
	// it conservatively assumes the latter. The task therefore shows ERROR
	// here, not RUNNING, even though we just set it to running above.
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"task", "list"}, &stdout, &stderr, appDeps{})
	if exitCode != exitSuccess {
		t.Fatalf("task list exit = %d, stderr = %q", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "task-1") {
		t.Fatalf("task list output missing task id: %s", output)
	}
	if !strings.Contains(output, "ERROR") {
		t.Fatalf("task list output missing reaped status: %s", output)
	}
	if !strings.Contains(output, "worker") {
		t.Fatalf("task list output missing specialist: %s", output)
	}
}

func TestRunTaskListJSON(t *testing.T) {
	dataHome := t.TempDir()
	manager, err := background.NewManager(filepath.Join(dataHome, "zero", "background"))
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	const taskID = "task-1"
	if _, err := manager.Register(background.RegisterInput{
		TaskID:         taskID,
		Type:           "specialist",
		SpecialistName: "worker",
		Description:    "Run audit",
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := manager.SetPID(taskID, 1234); err != nil {
		t.Fatalf("SetPID returned error: %v", err)
	}
	if err := manager.UpdateStatus(taskID, background.StatusRunning, 0); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"task", "list", "--json"}, &stdout, &stderr, appDeps{})
	if exitCode != exitSuccess {
		t.Fatalf("task list --json exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var tasks []background.Task
	if err := json.Unmarshal(stdout.Bytes(), &tasks); err != nil {
		t.Fatalf("failed to decode JSON: %v\n%s", err, stdout.String())
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	// See TestRunTaskListText: a fresh Manager (built inside runTaskList)
	// always reaps a persisted "running" task to "error" on load.
	if tasks[0].ID != "task-1" || tasks[0].Status != background.StatusError || tasks[0].SpecialistName != "worker" {
		t.Fatalf("unexpected task payload: %#v", tasks[0])
	}
}

func TestRunTaskHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"task", "--help"}, &stdout, &stderr, appDeps{})
	if exitCode != exitSuccess {
		t.Fatalf("task --help exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Commands:") || !strings.Contains(stdout.String(), "list") {
		t.Fatalf("task --help missing usage: %s", stdout.String())
	}
}

func TestRunTaskUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps([]string{"task", "unknown"}, &stdout, &stderr, appDeps{})
	if exitCode == exitSuccess {
		t.Fatalf("expected non-zero exit for unknown task command")
	}
	if !strings.Contains(stderr.String(), "unknown task command") {
		t.Fatalf("stderr = %q, want unknown command error", stderr.String())
	}
}
