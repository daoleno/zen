package classifier

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateKeepsUTF8WithinExistingByteBudget(t *testing.T) {
	for _, value := range []string{"ASCII", "中文", "🙂", "e\u0301", "𠀀中abc"} {
		input := strings.Repeat(value, 100)
		for limit := 0; limit <= 165; limit++ {
			got := truncate(input, limit)
			if !utf8.ValidString(got) || len(got) > limit {
				t.Fatalf("limit=%d result=%q bytes=%d", limit, got, len(got))
			}
			if limit >= 3 && !strings.HasPrefix(input, strings.TrimSuffix(got, "...")) {
				t.Fatalf("truncation invented content: %q", got)
			}
		}
	}
	if got := truncate(strings.Repeat("a", 200), 160); got != strings.Repeat("a", 157)+"..." {
		t.Fatalf("ASCII contract changed: %q", got)
	}
	progress, err := ValidateProgress(WorkerProgress{Status: "done", Phase: "reporting", Attention: "done", Summary: strings.Repeat("中文", 40)})
	if err != nil || !utf8.ValidString(progress.Summary) {
		t.Fatalf("progress=%+v err=%v", progress, err)
	}
}
