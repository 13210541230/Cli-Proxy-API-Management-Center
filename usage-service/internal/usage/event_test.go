package usage

import "testing"

func TestNormalizeRawPreservesReasoningEffort(t *testing.T) {
	event, err := NormalizeRaw([]byte(`{
		"request_id": "req-reasoning",
		"timestamp": "2026-01-02T03:04:05Z",
		"method": "POST",
		"path": "/v1/responses",
		"model": "gpt-5",
		"reasoning_effort": "high",
		"tokens": {"input_tokens": 10, "output_tokens": 20, "reasoning_tokens": 30}
	}`))
	if err != nil {
		t.Fatalf("normalize raw: %v", err)
	}
	if event.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want high", event.ReasoningEffort)
	}

	legacy, err := NormalizeRaw([]byte(`{
		"request_id": "req-thinking-level",
		"timestamp": "2026-01-02T03:04:05Z",
		"model": "o3",
		"thinkingLevel": "medium"
	}`))
	if err != nil {
		t.Fatalf("normalize legacy reasoning field: %v", err)
	}
	if legacy.ReasoningEffort != "medium" {
		t.Fatalf("legacy reasoning effort = %q, want medium", legacy.ReasoningEffort)
	}
}

func TestNormalizeRawPreservesRequestTelemetry(t *testing.T) {
	event, err := NormalizeRaw([]byte(`{
		"request_id": "req-telemetry",
		"timestamp": "2026-01-02T03:04:05Z",
		"model": "gpt-5",
		"ttft_ms": 420,
		"service_tier": "priority",
		"request_service_tier": "priority",
		"response_service_tier": "standard",
		"executor_type": "responses",
		"status_code": 429,
		"error_message": "rate limited"
	}`))
	if err != nil {
		t.Fatalf("normalize telemetry: %v", err)
	}
	if event.TTFTMS == nil || *event.TTFTMS != 420 || event.ServiceTier != "priority" {
		t.Fatalf("telemetry timing/tier = %#v", event)
	}
	if event.RequestServiceTier != "priority" || event.ResponseServiceTier != "standard" || event.ExecutorType != "responses" {
		t.Fatalf("telemetry service fields = %#v", event)
	}
	if event.FailStatusCode == nil || *event.FailStatusCode != 429 || event.FailSummary != "rate limited" || !event.Failed {
		t.Fatalf("telemetry failure fields = %#v", event)
	}
}

func TestBuildPayloadIncludesReasoningEffort(t *testing.T) {
	payload := BuildPayload([]Event{{
		Timestamp:       "2026-01-02T03:04:05Z",
		Model:           "gpt-5",
		Endpoint:        "POST /v1/responses",
		ReasoningEffort: "low",
		InputTokens:     1,
		OutputTokens:    2,
		TotalTokens:     3,
	}})
	items := payload.APIs["POST /v1/responses"].Models["gpt-5"].Details
	if len(items) != 1 || items[0].ReasoningEffort != "low" {
		t.Fatalf("payload detail = %#v", items)
	}
}
