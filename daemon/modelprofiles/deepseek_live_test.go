package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Live official DeepSeek Responses gate. Skips unless both are set:
//
//	ZEN_DEEPSEEK_LIVE=1 DEEPSEEK_API_KEY=… go test ./modelprofiles -run TestDeepSeekCodexLiveOfficialAPI -count=1 -timeout 120s -v
func TestDeepSeekCodexLiveOfficialAPI(t *testing.T) {
	if os.Getenv("ZEN_DEEPSEEK_LIVE") == "" {
		t.Skip("credential-only live gate: set ZEN_DEEPSEEK_LIVE=1 with DEEPSEEK_API_KEY")
	}
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Fatal("ZEN_DEEPSEEK_LIVE=1 requires DEEPSEEK_API_KEY (live acceptance not claimed without it)")
	}

	body := map[string]any{
		"model": "deepseek-v4-flash",
		"input": []any{
			map[string]any{
				"type": "message", "role": "user",
				"content": []any{map[string]string{"type": "input_text", "text": "Reply with exactly: zen-live-ok"}},
			},
		},
		"tools": []any{
			map[string]any{
				"type": "function", "name": "echo_ok",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		"stream": false,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/responses", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("live DeepSeek request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		t.Fatalf("live DeepSeek status=%d body=%s", resp.StatusCode, snippet)
	}
	text := string(respBody)
	if !strings.Contains(text, "zen-live-ok") && !strings.Contains(text, "output_text") && !strings.Contains(text, "function_call") {
		snippet := text
		if len(snippet) > 600 {
			snippet = snippet[:600]
		}
		t.Fatalf("live response lacked useful content: %s", snippet)
	}
	t.Logf("live DeepSeek Responses ok: bytes=%d status=%d", len(respBody), resp.StatusCode)
}
