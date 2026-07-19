package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Gitlawb/zero/internal/background"
)

type taskOptions struct {
	json bool
}

func runTask(args []string, stdout io.Writer, stderr io.Writer, deps appDeps) int {
	command, remaining, options, help, err := parseTaskArgs(args)
	if err != nil {
		return writeExecUsageError(stderr, err.Error())
	}
	if help {
		if err := writeTaskHelp(stdout); err != nil {
			return exitCrash
		}
		return exitSuccess
	}
	if err := validateTaskCommand(command, remaining); err != nil {
		return writeExecUsageError(stderr, err.Error())
	}

	env := make(map[string]string, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		env[key] = value
	}

	switch command {
	case "list":
		return runTaskList(env, options, stdout, stderr)
	default:
		return writeExecUsageError(stderr, fmt.Sprintf("unknown task command %q", command))
	}
}

func parseTaskArgs(args []string) (string, []string, taskOptions, bool, error) {
	command := "list"
	commandExplicit := false
	remaining := []string{}
	options := taskOptions{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-h", "--help", "help":
			return command, remaining, options, true, nil
		case "--json":
			options.json = true
		default:
			if strings.HasPrefix(arg, "-") {
				return command, remaining, options, false, fmt.Errorf("unknown task flag %q", arg)
			}
			if !commandExplicit {
				command = arg
				commandExplicit = true
			} else {
				remaining = append(remaining, arg)
			}
		}
	}
	return command, remaining, options, false, nil
}

func validateTaskCommand(command string, remaining []string) error {
	switch command {
	case "list":
		if len(remaining) != 0 {
			return fmt.Errorf("task list does not accept positional arguments")
		}
	default:
		return fmt.Errorf("unknown task command %q", command)
	}
	return nil
}

func runTaskList(env map[string]string, options taskOptions, stdout io.Writer, stderr io.Writer) int {
	manager, err := background.NewManagerWithOptions(background.ManagerOptions{RootDir: "", Env: env})
	if err != nil {
		return writeAppError(stderr, err.Error(), exitCrash)
	}
	tasks := manager.List()
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].StartedAt.Before(tasks[j].StartedAt)
	})
	if options.json {
		if err := writePrettyJSON(stdout, tasks); err != nil {
			return exitCrash
		}
		return exitSuccess
	}
	if len(tasks) == 0 {
		if _, err := fmt.Fprintln(stdout, "No background tasks."); err != nil {
			return exitCrash
		}
		return exitSuccess
	}
	lines := make([]string, 0, len(tasks)+1)
	lines = append(lines, "ID              STATUS       SPECIALIST     STARTED AT")
	lines = append(lines, strings.Repeat("-", 72))
	for _, task := range tasks {
		lines = append(lines, formatTaskRow(task))
	}
	if _, err := fmt.Fprintln(stdout, strings.Join(lines, "\n")); err != nil {
		return exitCrash
	}
	return exitSuccess
}

func formatTaskRow(task background.Task) string {
	id := task.ID
	if len(id) > 18 {
		id = id[:18]
	}
	status := strings.ToUpper(string(task.Status))
	specialist := task.SpecialistName
	if len(specialist) > 14 {
		specialist = specialist[:14]
	}
	started := task.StartedAt.Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%-18s %-12s %-14s %s", id, status, specialist, started)
}

func writeTaskHelp(w io.Writer) error {
	_, err := fmt.Fprint(w, `Usage:
  zero task [command]

Commands:
  list       List background specialist tasks

Flags:
  --json     Print JSON output
  -h, --help Show this help
`)
	return err
}
