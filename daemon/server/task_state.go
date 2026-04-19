package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/daoleno/zen/daemon/task"
)

type taskStateSnapshot struct {
	Available     bool                   `json:"available"`
	Path          string                 `json:"path,omitempty"`
	Title         string                 `json:"title,omitempty"`
	Goal          taskStateSection       `json:"goal"`
	MachineStatus taskStateMachineStatus `json:"machine_status"`
	Completed     taskStateSection       `json:"completed"`
	Blockers      taskStateSection       `json:"blockers"`
	NextStep      taskStateSection       `json:"next_step"`
}

type taskStateSection struct {
	Body  string   `json:"body,omitempty"`
	Items []string `json:"items,omitempty"`
}

type taskStateMachineStatus struct {
	Updated    string           `json:"updated,omitempty"`
	TaskStatus string           `json:"task_status,omitempty"`
	RunStatus  string           `json:"run_status,omitempty"`
	RunAttempt int              `json:"run_attempt,omitempty"`
	Workspace  string           `json:"workspace,omitempty"`
	Session    string           `json:"session,omitempty"`
	Summary    string           `json:"summary,omitempty"`
	Fields     []taskStateField `json:"fields,omitempty"`
}

type taskStateField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s *Server) readTaskStateSnapshot(taskID string) (taskStateSnapshot, error) {
	snapshot := emptyTaskStateSnapshot()

	if s.tasks == nil {
		return snapshot, fmt.Errorf("task store is not configured")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return snapshot, fmt.Errorf("task_id is required")
	}

	currentTask := s.tasks.Get(taskID)
	if currentTask == nil {
		return snapshot, fmt.Errorf("task not found")
	}

	cwd := strings.TrimSpace(currentTask.Cwd)
	if cwd == "" {
		return snapshot, nil
	}

	path := taskStateFilePath(cwd)
	snapshot.Path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}

	snapshot = parseTaskStateContent(string(data))
	snapshot.Available = true
	snapshot.Path = path

	if strings.TrimSpace(snapshot.Title) == "" {
		snapshot.Title = fmt.Sprintf("%s %s", task.DisplayID(currentTask), strings.TrimSpace(currentTask.Title))
	}

	return snapshot, nil
}

func emptyTaskStateSnapshot() taskStateSnapshot {
	return taskStateSnapshot{
		Goal:          taskStateSection{},
		MachineStatus: taskStateMachineStatus{},
		Completed:     taskStateSection{},
		Blockers:      taskStateSection{},
		NextStep:      taskStateSection{},
	}
}

func parseTaskStateContent(content string) taskStateSnapshot {
	snapshot := emptyTaskStateSnapshot()

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	sections := make(map[string][]string)
	currentSection := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# ") && snapshot.Title == "":
			snapshot.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "## "):
			currentSection = normalizeTaskStateHeading(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			if currentSection != "" {
				sections[currentSection] = []string{}
			}
		case currentSection != "":
			sections[currentSection] = append(sections[currentSection], line)
		}
	}

	snapshot.Goal = parseTaskStateSection(sections["goal"])
	snapshot.MachineStatus = parseTaskStateMachineStatus(sections["machine_status"])
	snapshot.Completed = parseTaskStateSection(sections["completed"])
	snapshot.Blockers = parseTaskStateSection(sections["blockers"])
	snapshot.NextStep = parseTaskStateSection(sections["next_step"])

	return snapshot
}

func normalizeTaskStateHeading(heading string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(heading)), " "))

	switch normalized {
	case "goal":
		return "goal"
	case "machine status":
		return "machine_status"
	case "completed":
		return "completed"
	case "known pitfalls / blockers", "known pitfalls", "known blockers", "blockers", "pitfalls / blockers":
		return "blockers"
	case "next step", "next steps":
		return "next_step"
	default:
		return ""
	}
}

func parseTaskStateSection(lines []string) taskStateSection {
	trimmedLines := trimTaskStateLines(lines)
	if len(trimmedLines) == 0 {
		return taskStateSection{}
	}

	bodyLines := make([]string, 0, len(trimmedLines))
	items := make([]string, 0, len(trimmedLines))

	for _, line := range trimmedLines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			bodyLines = append(bodyLines, "")
		case strings.HasPrefix(trimmed, "- "):
			if item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")); item != "" {
				items = append(items, item)
			}
		case strings.HasPrefix(trimmed, "* "):
			if item := strings.TrimSpace(strings.TrimPrefix(trimmed, "* ")); item != "" {
				items = append(items, item)
			}
		default:
			bodyLines = append(bodyLines, strings.TrimRight(line, " \t"))
		}
	}

	return taskStateSection{
		Body:  strings.TrimSpace(strings.Join(trimTaskStateLines(bodyLines), "\n")),
		Items: items,
	}
}

func parseTaskStateMachineStatus(lines []string) taskStateMachineStatus {
	status := taskStateMachineStatus{}

	for _, line := range trimTaskStateLines(lines) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == taskStateMachineStatusStart || trimmed == taskStateMachineStatusEnd {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		label, value, ok := strings.Cut(payload, ":")
		if !ok {
			continue
		}

		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		if label == "" || value == "" {
			continue
		}

		status.Fields = append(status.Fields, taskStateField{
			Label: label,
			Value: value,
		})

		switch strings.ToLower(label) {
		case "updated":
			status.Updated = value
		case "task status":
			status.TaskStatus = value
		case "run status":
			status.RunStatus = value
		case "run attempt":
			if attempt, err := strconv.Atoi(value); err == nil {
				status.RunAttempt = attempt
			}
		case "workspace":
			status.Workspace = value
		case "session":
			status.Session = value
		case "summary":
			status.Summary = value
		}
	}

	return status
}

func trimTaskStateLines(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return lines[start:end]
}
