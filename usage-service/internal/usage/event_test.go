package usage

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestNormalizeRawTracksModelRoutingAndErrorTelemetry(t *testing.T) {
	event, err := NormalizeRaw([]byte(`{
		"request_id": "req-route",
		"timestamp": "2026-01-02T03:04:05Z",
		"alias": "gpt-5",
		"model": "gpt-5",
		"upstream_model": "gpt-5-mini",
		"upstream_model_evidence": "response_header",
		"status_code": 503,
		"error_code": "server_is_overloaded",
		"error_type": "service_unavailable_error",
		"fail_summary": "server_is_overloaded: overloaded"
	}`))
	if err != nil {
		t.Fatalf("normalize routing telemetry: %v", err)
	}
	if event.RequestedModel != "gpt-5" || event.ResolvedModel != "gpt-5" || event.UpstreamModel != "gpt-5-mini" {
		t.Fatalf("model telemetry = %#v", event)
	}
	if event.ModelMatch != "upstream_mismatch" || event.ModelEvidence != "response_header" {
		t.Fatalf("model comparison = %#v", event)
	}
	if event.ErrorCode != "server_is_overloaded" || event.ErrorClass != "capacity" {
		t.Fatalf("error telemetry = %#v", event)
	}
}

func TestNormalizeRawAcceptsResponseModelFallback(t *testing.T) {
	event, err := NormalizeRaw([]byte(`{
		"request_id": "req-resp-model",
		"timestamp": "2026-01-02T03:04:05Z",
		"alias": "gpt-5",
		"model": "gpt-5",
		"response_model": "gpt-5.6-sol",
		"status_code": 200
	}`))
	if err != nil {
		t.Fatalf("normalize response_model fallback: %v", err)
	}
	if event.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("upstream model = %q, want response_model fallback gpt-5.6-sol", event.UpstreamModel)
	}
}

func TestNormalizeRawStoresSignalWithoutFailureBody(t *testing.T) {
	event, err := NormalizeRaw([]byte(`{
		"request_id": "req-security",
		"timestamp": "2026-01-02T03:04:05Z",
		"model": "gpt-5",
		"failed": true,
		"security_signal": "cyber_policy",
		"fail": {"status_code": 400, "body": "upstream response must not be stored"}
	}`))
	if err != nil {
		t.Fatalf("normalize security event: %v", err)
	}
	if event.SecuritySignal != SecuritySignalCyberPolicy {
		t.Fatalf("security signal = %q", event.SecuritySignal)
	}
	if strings.Contains(event.RawJSON, "upstream response must not be stored") {
		t.Fatalf("raw JSON retained failure body: %s", event.RawJSON)
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(event.RawJSON), &stored); err != nil {
		t.Fatalf("decode stored raw JSON: %v", err)
	}
	fail, ok := stored["fail"].(map[string]any)
	if !ok {
		t.Fatalf("stored fail object = %#v", stored["fail"])
	}
	if _, ok := fail["body"]; ok {
		t.Fatalf("stored fail object retained body: %#v", fail)
	}
}

func TestBuildPayloadIncludesSecuritySignalCount(t *testing.T) {
	payload := BuildPayload([]Event{
		{Timestamp: "2026-01-02T03:04:05Z", Model: "gpt-5", Endpoint: "POST /v1/responses", SecuritySignal: SecuritySignalCyberPolicy},
		{Timestamp: "2026-01-02T03:05:05Z", Model: "gpt-5", Endpoint: "POST /v1/responses", SecuritySignal: "other"},
	})
	if payload.SecuritySignalCount != 1 {
		t.Fatalf("security signal count = %d, want 1", payload.SecuritySignalCount)
	}
	items := payload.APIs["POST /v1/responses"].Models["gpt-5"].Details
	if len(items) != 2 || items[0].SecuritySignal != SecuritySignalCyberPolicy {
		t.Fatalf("payload details = %#v", items)
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
