package usage

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"
)

func makeTurnState(t *testing.T, blocks int) string {
	t.Helper()
	raw := make([]byte, 0, fernetFixedBytes+blocks*16)
	raw = append(raw, fernetVersion)
	seconds := make([]byte, 8)
	binary.BigEndian.PutUint64(seconds, uint64(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix()))
	raw = append(raw, seconds...)
	raw = append(raw, make([]byte, 16)...)
	raw = append(raw, make([]byte, blocks*16)...)
	raw = append(raw, make([]byte, 32)...)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestClassifyTurnState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		value      string
		wantClass  string
		wantPrefix string
	}{
		{name: "normal ten blocks", value: makeTurnState(t, 10), wantClass: "normal", wantPrefix: "blocks=10;"},
		{name: "normal twelve blocks", value: makeTurnState(t, 12), wantClass: "normal", wantPrefix: "blocks=12;"},
		{name: "unexpected blocks", value: makeTurnState(t, 7), wantClass: "suspected", wantPrefix: "blocks=7;"},
		{name: "garbage", value: "not-a-token", wantClass: "suspected", wantPrefix: "parse_error:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, evidence := classifyTurnState(tc.value)
			if class != tc.wantClass {
				t.Fatalf("class = %q, want %q", class, tc.wantClass)
			}
			if len(evidence) < len(tc.wantPrefix) || evidence[:len(tc.wantPrefix)] != tc.wantPrefix {
				t.Fatalf("evidence = %q, want prefix %q", evidence, tc.wantPrefix)
			}
		})
	}
}

func TestReadTurnStateScope(t *testing.T) {
	t.Parallel()
	if class, _ := readTurnState(map[string]any{"provider": "openai-compatible"}); class != "" {
		t.Fatalf("non-codex provider class = %q, want empty", class)
	}
	codexNoHeaders := map[string]any{"provider": "codex"}
	if class, _ := readTurnState(codexNoHeaders); class != "" {
		t.Fatalf("missing response headers class = %q, want empty", class)
	}
	codexMissing := map[string]any{
		"provider":         "codex",
		"response_headers": map[string]any{"Content-Type": []any{"text/event-stream"}},
	}
	if class, evidence := readTurnState(codexMissing); class != "missing" || evidence == "" {
		t.Fatalf("missing turn state = (%q, %q)", class, evidence)
	}
	codexFailed := map[string]any{
		"provider":         "codex",
		"failed":           true,
		"response_headers": map[string]any{},
	}
	if class, _ := readTurnState(codexFailed); class != "" {
		t.Fatalf("failed request class = %q, want empty", class)
	}
	codexPresent := map[string]any{
		"provider":         "codex",
		"response_headers": map[string]any{"X-Codex-Turn-State": []any{makeTurnState(t, 10)}},
	}
	if class, _ := readTurnState(codexPresent); class != "normal" {
		t.Fatalf("present turn state class = %q, want normal", class)
	}
}
