package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const SecuritySignalCyberPolicy = "cyber_policy"

type Event struct {
	RequestID   string `json:"request_id,omitempty"`
	EventHash   string `json:"event_hash"`
	TimestampMS int64  `json:"timestamp_ms"`
	Timestamp   string `json:"timestamp"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model"`
	// ReasoningEffort is the request-side model reasoning setting. It is
	// separate from response-side ReasoningTokens and may be absent in legacy events.
	ReasoningEffort      string `json:"reasoning_effort,omitempty"`
	TTFTMS               *int64 `json:"ttft_ms,omitempty"`
	ServiceTier          string `json:"service_tier,omitempty"`
	RequestServiceTier   string `json:"request_service_tier,omitempty"`
	ResponseServiceTier  string `json:"response_service_tier,omitempty"`
	ExecutorType         string `json:"executor_type,omitempty"`
	FailStatusCode       *int64 `json:"fail_status_code,omitempty"`
	FailSummary          string `json:"fail_summary,omitempty"`
	SecuritySignal       string `json:"security_signal,omitempty"`
	Endpoint             string `json:"endpoint,omitempty"`
	Method               string `json:"method,omitempty"`
	Path                 string `json:"path,omitempty"`
	AuthType             string `json:"auth_type,omitempty"`
	AuthIndex            string `json:"auth_index,omitempty"`
	Source               string `json:"source,omitempty"`
	SourceHash           string `json:"source_hash,omitempty"`
	APIKeyHash           string `json:"api_key_hash,omitempty"`
	AccountSnapshot      string `json:"account_snapshot,omitempty"`
	AuthLabelSnapshot    string `json:"auth_label_snapshot,omitempty"`
	AuthFileSnapshot     string `json:"auth_file_snapshot,omitempty"`
	AuthProviderSnapshot string `json:"auth_provider_snapshot,omitempty"`
	AuthSnapshotAtMS     int64  `json:"auth_snapshot_at_ms,omitempty"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	ReasoningTokens      int64  `json:"reasoning_tokens"`
	CachedTokens         int64  `json:"cached_tokens"`
	CacheTokens          int64  `json:"cache_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	LatencyMS            *int64 `json:"latency_ms,omitempty"`
	Failed               bool   `json:"failed"`
	RawJSON              string `json:"raw_json,omitempty"`
	CreatedAtMS          int64  `json:"created_at_ms"`
}

type Tokens struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	CacheTokens     int64 `json:"cache_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type Detail struct {
	Timestamp            string `json:"timestamp"`
	Source               string `json:"source"`
	AuthIndex            string `json:"auth_index,omitempty"`
	APIKeyHash           string `json:"api_key_hash,omitempty"`
	AccountSnapshot      string `json:"account_snapshot,omitempty"`
	AuthLabelSnapshot    string `json:"auth_label_snapshot,omitempty"`
	AuthFileSnapshot     string `json:"auth_file_snapshot,omitempty"`
	AuthProviderSnapshot string `json:"auth_provider_snapshot,omitempty"`
	AuthSnapshotAtMS     int64  `json:"auth_snapshot_at_ms,omitempty"`
	ReasoningEffort      string `json:"reasoning_effort,omitempty"`
	TTFTMS               *int64 `json:"ttft_ms,omitempty"`
	ServiceTier          string `json:"service_tier,omitempty"`
	RequestServiceTier   string `json:"request_service_tier,omitempty"`
	ResponseServiceTier  string `json:"response_service_tier,omitempty"`
	ExecutorType         string `json:"executor_type,omitempty"`
	FailStatusCode       *int64 `json:"fail_status_code,omitempty"`
	FailSummary          string `json:"fail_summary,omitempty"`
	SecuritySignal       string `json:"security_signal,omitempty"`
	LatencyMS            *int64 `json:"latency_ms,omitempty"`
	Tokens               Tokens `json:"tokens"`
	Failed               bool   `json:"failed"`
}

type ModelAggregate struct {
	Details []Detail `json:"details"`
}

type APIAggregate struct {
	Models map[string]*ModelAggregate `json:"models"`
}

type Payload struct {
	TotalRequests       int64                    `json:"total_requests"`
	SuccessCount        int64                    `json:"success_count"`
	FailureCount        int64                    `json:"failure_count"`
	SecuritySignalCount int64                    `json:"security_signal_count,omitempty"`
	TotalTokens         int64                    `json:"total_tokens"`
	APIs                map[string]*APIAggregate `json:"apis"`
}

var endpointPattern = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s+(\S+)`)

func NormalizeRaw(raw []byte) (Event, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Event{}, err
	}
	record, ok := payload.(map[string]any)
	if !ok {
		return Event{}, fmt.Errorf("usage payload is not a JSON object")
	}

	redacted := sanitizeStoredValue(payload)
	redactedJSON, _ := json.Marshal(redacted)

	timestampMS, timestamp := readTimestamp(record)
	method := strings.ToUpper(readString(record, "method", "http_method", "httpMethod"))
	path := readString(record, "path", "url_path", "urlPath", "route")
	endpoint := readString(record, "endpoint", "api", "request", "operation")
	if endpoint == "" && method != "" && path != "" {
		endpoint = method + " " + path
	}
	if endpoint != "" {
		if match := endpointPattern.FindStringSubmatch(endpoint); len(match) == 3 {
			if method == "" {
				method = strings.ToUpper(match[1])
			}
			if path == "" {
				path = match[2]
			}
		}
	}
	if endpoint == "" {
		endpoint = "-"
	}

	inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheTokens, totalTokens := readTokenFields(record)
	if totalTokens <= 0 {
		totalTokens = inputTokens + outputTokens + reasoningTokens + maxInt64(cachedTokens, cacheTokens)
	}

	latencyMS := readOptionalInt(record, "latency_ms", "latencyMs", "duration_ms", "durationMs", "elapsed_ms", "elapsedMs")
	ttftMS := readOptionalInt(record, "ttft_ms", "ttftMs", "time_to_first_token_ms", "timeToFirstTokenMs")
	failStatusCode := readOptionalPositiveInt(record, "fail_status_code", "failStatusCode", "status_code", "statusCode", "http_status", "httpStatus")
	requestServiceTier := readString(record, "request_service_tier", "requestServiceTier")
	responseServiceTier := readString(record, "response_service_tier", "responseServiceTier")
	serviceTier := readString(record, "service_tier", "serviceTier")
	if serviceTier == "" {
		serviceTier = requestServiceTier
	}
	if serviceTier == "" {
		serviceTier = responseServiceTier
	}
	failed := readFailed(record)
	sourceRaw := readString(record, "source", "api_key", "apiKey", "key", "account", "email")
	source := maskSource(sourceRaw)
	apiKey := readString(record, "api_key", "apiKey", "key")
	authIndex := readString(record, "auth_index", "authIndex", "AuthIndex")

	event := Event{
		RequestID:            readString(record, "request_id", "requestId", "id"),
		TimestampMS:          timestampMS,
		Timestamp:            timestamp,
		Provider:             readString(record, "provider", "type", "auth_type", "authType"),
		Model:                readString(record, "model", "model_name", "modelName"),
		ReasoningEffort:      readString(record, "reasoning_effort", "reasoningEffort", "thinking_level", "thinkingLevel"),
		TTFTMS:               ttftMS,
		ServiceTier:          serviceTier,
		RequestServiceTier:   requestServiceTier,
		ResponseServiceTier:  responseServiceTier,
		ExecutorType:         readString(record, "executor_type", "executorType"),
		FailStatusCode:       failStatusCode,
		FailSummary:          readString(record, "fail_summary", "failSummary", "error_message", "errorMessage"),
		SecuritySignal:       readSecuritySignal(record),
		Endpoint:             endpoint,
		Method:               method,
		Path:                 path,
		AuthType:             readString(record, "auth_type", "authType"),
		AuthIndex:            authIndex,
		Source:               source,
		SourceHash:           hashString(sourceRaw),
		APIKeyHash:           hashString(apiKey),
		AccountSnapshot:      readString(record, "account_snapshot", "accountSnapshot"),
		AuthLabelSnapshot:    readString(record, "auth_label_snapshot", "authLabelSnapshot"),
		AuthFileSnapshot:     readString(record, "auth_file_snapshot", "authFileSnapshot"),
		AuthProviderSnapshot: readString(record, "auth_provider_snapshot", "authProviderSnapshot"),
		AuthSnapshotAtMS:     readInt(record, "auth_snapshot_at_ms", "authSnapshotAtMs"),
		InputTokens:          inputTokens,
		OutputTokens:         outputTokens,
		ReasoningTokens:      reasoningTokens,
		CachedTokens:         cachedTokens,
		CacheTokens:          cacheTokens,
		TotalTokens:          totalTokens,
		LatencyMS:            latencyMS,
		Failed:               failed,
		RawJSON:              string(redactedJSON),
		CreatedAtMS:          time.Now().UnixMilli(),
	}
	if event.Model == "" {
		event.Model = "-"
	}
	event.EventHash = buildEventHash(event)
	return event, nil
}

func BuildPayload(events []Event) Payload {
	payload := Payload{APIs: map[string]*APIAggregate{}}
	for _, event := range events {
		payload.TotalRequests++
		if event.Failed {
			payload.FailureCount++
		} else {
			payload.SuccessCount++
		}
		if event.SecuritySignal == SecuritySignalCyberPolicy {
			payload.SecuritySignalCount++
		}
		payload.TotalTokens += event.TotalTokens

		endpoint := event.Endpoint
		if endpoint == "" {
			endpoint = "-"
		}
		apiEntry := payload.APIs[endpoint]
		if apiEntry == nil {
			apiEntry = &APIAggregate{Models: map[string]*ModelAggregate{}}
			payload.APIs[endpoint] = apiEntry
		}
		model := event.Model
		if model == "" {
			model = "-"
		}
		modelEntry := apiEntry.Models[model]
		if modelEntry == nil {
			modelEntry = &ModelAggregate{}
			apiEntry.Models[model] = modelEntry
		}
		modelEntry.Details = append(modelEntry.Details, Detail{
			Timestamp:            event.Timestamp,
			Source:               event.Source,
			AuthIndex:            event.AuthIndex,
			APIKeyHash:           event.APIKeyHash,
			AccountSnapshot:      event.AccountSnapshot,
			AuthLabelSnapshot:    event.AuthLabelSnapshot,
			AuthFileSnapshot:     event.AuthFileSnapshot,
			AuthProviderSnapshot: event.AuthProviderSnapshot,
			AuthSnapshotAtMS:     event.AuthSnapshotAtMS,
			ReasoningEffort:      event.ReasoningEffort,
			TTFTMS:               event.TTFTMS,
			ServiceTier:          event.ServiceTier,
			RequestServiceTier:   event.RequestServiceTier,
			ResponseServiceTier:  event.ResponseServiceTier,
			ExecutorType:         event.ExecutorType,
			FailStatusCode:       event.FailStatusCode,
			FailSummary:          event.FailSummary,
			SecuritySignal:       event.SecuritySignal,
			LatencyMS:            event.LatencyMS,
			Failed:               event.Failed,
			Tokens: Tokens{
				InputTokens:     event.InputTokens,
				OutputTokens:    event.OutputTokens,
				ReasoningTokens: event.ReasoningTokens,
				CachedTokens:    event.CachedTokens,
				CacheTokens:     event.CacheTokens,
				TotalTokens:     event.TotalTokens,
			},
		})
	}
	return payload
}

func readSecuritySignal(record map[string]any) string {
	if signal := readString(record, "security_signal", "securitySignal"); signal == SecuritySignalCyberPolicy {
		return signal
	}
	return ""
}

func readTimestamp(record map[string]any) (int64, string) {
	raw := first(record, "timestamp", "timestamp_ms", "time", "created_at_ms", "created_at", "createdAt", "created", "request_time", "requestTime")
	now := time.Now()
	if raw == nil {
		return now.UnixMilli(), now.UTC().Format(time.RFC3339Nano)
	}
	switch value := raw.(type) {
	case float64:
		ms := int64(value)
		if ms < 10_000_000_000 {
			ms *= 1000
		}
		return ms, time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	case string:
		trimmed := strings.TrimSpace(value)
		if number, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			if number < 10_000_000_000 {
				number *= 1000
			}
			return number, time.UnixMilli(number).UTC().Format(time.RFC3339Nano)
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if parsed, err := time.Parse(layout, trimmed); err == nil {
				return parsed.UnixMilli(), parsed.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return now.UnixMilli(), now.UTC().Format(time.RFC3339Nano)
}

func readTokenFields(record map[string]any) (int64, int64, int64, int64, int64, int64) {
	tokens := map[string]any{}
	if nested, ok := first(record, "tokens", "usage").(map[string]any); ok {
		tokens = nested
	}
	input := readIntFrom(tokens, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")
	if input == 0 {
		input = readInt(record, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")
	}
	output := readIntFrom(tokens, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
	if output == 0 {
		output = readInt(record, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
	}
	reasoning := readIntFrom(tokens, "reasoning_tokens", "reasoningTokens")
	if reasoning == 0 {
		reasoning = readInt(record, "reasoning_tokens", "reasoningTokens")
	}
	cached := readIntFrom(tokens, "cached_tokens", "cachedTokens")
	if cached == 0 {
		cached = readInt(record, "cached_tokens", "cachedTokens")
	}
	cache := readIntFrom(tokens, "cache_tokens", "cacheTokens")
	if cache == 0 {
		cache = readInt(record, "cache_tokens", "cacheTokens")
	}
	total := readIntFrom(tokens, "total_tokens", "totalTokens", "total")
	if total == 0 {
		total = readInt(record, "total_tokens", "totalTokens", "total")
	}
	return input, output, reasoning, cached, cache, total
}

func readFailed(record map[string]any) bool {
	if value, ok := first(record, "failed", "is_failed", "isFailed").(bool); ok {
		return value
	}
	if value, ok := first(record, "success", "ok").(bool); ok {
		return !value
	}
	status := readInt(record, "status", "status_code", "statusCode", "http_status", "httpStatus")
	if status >= 400 {
		return true
	}
	return first(record, "error", "error_message", "errorMessage") != nil
}

func readOptionalInt(record map[string]any, keys ...string) *int64 {
	value := readInt(record, keys...)
	if value == 0 && first(record, keys...) == nil {
		return nil
	}
	return &value
}

func readOptionalPositiveInt(record map[string]any, keys ...string) *int64 {
	value := readInt(record, keys...)
	if value <= 0 {
		return nil
	}
	return &value
}

func readString(record map[string]any, keys ...string) string {
	raw := first(record, keys...)
	if raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func readInt(record map[string]any, keys ...string) int64 {
	return readIntFrom(record, keys...)
}

func readIntFrom(record map[string]any, keys ...string) int64 {
	raw := first(record, keys...)
	switch value := raw.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		number, _ := value.Int64()
		return number
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed
	default:
		return 0
	}
}

func first(record map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := record[key]; ok {
			return value
		}
	}
	return nil
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func hashString(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

func buildEventHash(event Event) string {
	parts := []string{
		event.RequestID,
		event.Timestamp,
		event.Endpoint,
		event.Model,
		event.AuthIndex,
		event.SourceHash,
		strconv.FormatInt(event.InputTokens, 10),
		strconv.FormatInt(event.OutputTokens, 10),
		strconv.FormatInt(event.ReasoningTokens, 10),
		strconv.FormatInt(maxInt64(event.CachedTokens, event.CacheTokens), 10),
		strconv.FormatBool(event.Failed),
	}
	if event.LatencyMS != nil {
		parts = append(parts, strconv.FormatInt(*event.LatencyMS, 10))
	}
	return hashString(strings.Join(parts, "|"))
}

// MaskUsageSource preserves the display-safe source representation used by
// normalized events. Analytics/detail responses must use it even for legacy
// imported rows that may have bypassed NormalizeRaw.
func MaskUsageSource(value string) string { return maskSource(value) }

func maskSource(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "@") {
		parts := strings.SplitN(trimmed, "@", 2)
		prefix := parts[0]
		if len(prefix) > 3 {
			prefix = prefix[:3]
		}
		return prefix + "***@" + parts[1]
	}
	if looksSecret(trimmed) {
		if len(trimmed) <= 8 {
			return "m:****"
		}
		return "m:" + trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
	}
	return trimmed
}

func looksSecret(value string) bool {
	if strings.ContainsAny(value, " /\\") {
		return false
	}
	return strings.HasPrefix(value, "sk-") || strings.HasPrefix(value, "AIza") || len(value) >= 32
}

func redactValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			if isSecretKey(key) {
				result[key] = "[redacted]"
				continue
			}
			result[key] = redactValue(child)
		}
		return result
	case []any:
		result := make([]any, 0, len(item))
		for _, child := range item {
			result = append(result, redactValue(child))
		}
		return result
	default:
		return value
	}
}

func sanitizeStoredValue(value any) any {
	redacted := redactValue(value)
	return removeFailureBodies(redacted)
}

func sanitizeRawJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return ""
	}
	sanitized, err := json.Marshal(sanitizeStoredValue(value))
	if err != nil {
		return ""
	}
	return string(sanitized)
}

func removeFailureBodies(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			if isFailureContainerKey(key) {
				if nested, ok := child.(map[string]any); ok {
					nested = removeFailureBodies(nested).(map[string]any)
					for nestedKey := range nested {
						if normalizeJSONKey(nestedKey) == "body" {
							delete(nested, nestedKey)
						}
					}
					result[key] = nested
					continue
				}
			}
			result[key] = removeFailureBodies(child)
		}
		return result
	case []any:
		result := make([]any, 0, len(item))
		for _, child := range item {
			result = append(result, removeFailureBodies(child))
		}
		return result
	default:
		return value
	}
}

func isFailureContainerKey(key string) bool {
	switch normalizeJSONKey(key) {
	case "fail", "failure":
		return true
	default:
		return false
	}
}

func normalizeJSONKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return normalized == "api_key" ||
		normalized == "apikey" ||
		normalized == "authorization" ||
		normalized == "access_token" ||
		normalized == "refresh_token" ||
		normalized == "token" ||
		strings.Contains(normalized, "secret")
}
