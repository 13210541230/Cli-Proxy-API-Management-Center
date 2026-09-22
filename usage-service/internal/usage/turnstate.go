package usage

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Turn-state observation for Codex upstream responses. The upstream may return
// an X-Codex-Turn-State header whose value is a Fernet token. The token header
// can be parsed without the key: version byte, 8-byte issue timestamp, and the
// AES ciphertext length, which yields the block count. Normal ChatGPT plans
// observed in the wild use 10 blocks (Pro/Plus) or 12 blocks (Team). This is
// observational evidence for the monitoring panel, not a verified compute-tier
// claim; calibration against quality probes is deferred to a later phase.
const turnStateHeader = "X-Codex-Turn-State"

const (
	fernetFixedBytes = 1 + 8 + 16 + 32
	fernetVersion    = 0x80
)

var (
	turnStateMinTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	turnStateMaxTime = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	// turnStateNormalBlocks is the [10,12] baseline shared with the community
	// turn-state tooling (accepted_blocks defaults).
	turnStateNormalBlocks = map[int]struct{}{10: {}, 12: {}}
)

// readTurnState derives a compact class/evidence pair from a queued usage
// record. An empty class means "not applicable": the provider is not codex, no
// upstream response headers were captured, or the request failed (downgrade
// marking only applies to successful inference responses).
func readTurnState(record map[string]any) (string, string) {
	provider := strings.ToLower(strings.TrimSpace(readString(record, "provider", "type", "auth_type", "authType")))
	if provider != "codex" {
		return "", ""
	}
	if readFailed(record) {
		return "", ""
	}
	rawHeaders := first(record, "response_headers", "responseHeaders")
	headers, ok := rawHeaders.(map[string]any)
	if !ok || len(headers) == 0 {
		return "", ""
	}
	value := ""
	for key, raw := range headers {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"))
		if normalized != "x-codex-turn-state" {
			continue
		}
		if candidate := readHeaderValue(raw); candidate != "" {
			value = candidate
		}
	}
	if value == "" {
		return "missing", "response_without_turn_state"
	}
	return classifyTurnState(value)
}

// classifyTurnState parses the Fernet header and buckets it against the
// baseline block counts.
func classifyTurnState(value string) (string, string) {
	blocks, length, issuedAt, errParse := parseTurnState(value)
	if errParse != nil {
		return "suspected", "parse_error: " + errParse.Error()
	}
	evidence := fmt.Sprintf("blocks=%d;length=%d;issued_at=%s", blocks, length, issuedAt.Format(time.RFC3339))
	if _, normal := turnStateNormalBlocks[blocks]; !normal {
		return "suspected", evidence
	}
	return "normal", evidence
}

// parseTurnState decodes the unencrypted Fernet header fields of a turn-state
// token without requiring the encryption key.
func parseTurnState(value string) (blocks int, length int, issuedAt time.Time, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, 0, time.Time{}, fmt.Errorf("empty turn state")
	}
	if len(trimmed) > 4096 {
		return 0, 0, time.Time{}, fmt.Errorf("turn state too long")
	}
	for index := 0; index < len(trimmed); index++ {
		if trimmed[index] < 0x20 || trimmed[index] > 0x7e {
			return 0, 0, time.Time{}, fmt.Errorf("turn state contains a non-printable character")
		}
	}
	raw, errDecode := base64.RawURLEncoding.DecodeString(strings.TrimRight(trimmed, "="))
	if errDecode != nil {
		return 0, 0, time.Time{}, fmt.Errorf("base64 decode: %w", errDecode)
	}
	if len(raw) < fernetFixedBytes+16 {
		return 0, 0, time.Time{}, fmt.Errorf("fernet token too short")
	}
	if raw[0] != fernetVersion {
		return 0, 0, time.Time{}, fmt.Errorf("unexpected fernet version 0x%02x", raw[0])
	}
	cipherBytes := len(raw) - fernetFixedBytes
	if cipherBytes < 16 || cipherBytes%16 != 0 {
		return 0, 0, time.Time{}, fmt.Errorf("ciphertext not AES-block aligned")
	}
	seconds := binary.BigEndian.Uint64(raw[1:9])
	issuedAt = time.Unix(int64(seconds), 0).UTC()
	if issuedAt.Before(turnStateMinTime) || !issuedAt.Before(turnStateMaxTime) {
		return 0, 0, time.Time{}, fmt.Errorf("fernet timestamp out of range")
	}
	return cipherBytes / 16, len(trimmed), issuedAt, nil
}
