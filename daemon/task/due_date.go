package task

import (
	"fmt"
	"strings"
	"time"
)

const dueDateLayout = "2006-01-02"

// NormalizeDueDate validates a date-only due date in YYYY-MM-DD form.
func NormalizeDueDate(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	if _, err := time.Parse(dueDateLayout, trimmed); err != nil {
		return "", fmt.Errorf("invalid due date %q: use YYYY-MM-DD", value)
	}

	return trimmed, nil
}
