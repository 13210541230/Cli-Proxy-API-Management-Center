import { describe, expect, it } from 'vitest';
import { collectUsageDetailsWithEndpoint } from './usage';

describe('usage detail telemetry', () => {
  it('normalizes Plus-style request telemetry for the monitoring table', () => {
    const details = collectUsageDetailsWithEndpoint({
      apis: {
        'POST /v1/responses': {
          models: {
            'gpt-5': {
              details: [
                {
                  timestamp: '2026-01-02T03:04:05Z',
                  reasoning_effort: 'high',
                  ttft_ms: 420,
                  service_tier: 'priority',
                  request_service_tier: 'priority',
                  response_service_tier: 'standard',
                  executor_type: 'responses',
                  fail_status_code: 429,
                  fail_summary: 'rate limited',
                  failed: true,
                  tokens: {
                    input_tokens: 10,
                    output_tokens: 20,
                    reasoning_tokens: 30,
                  },
                },
              ],
            },
          },
        },
      },
    });

    expect(details).toHaveLength(1);
    expect(details[0]).toMatchObject({
      reasoning_effort: 'high',
      ttft_ms: 420,
      service_tier: 'priority',
      request_service_tier: 'priority',
      response_service_tier: 'standard',
      executor_type: 'responses',
      fail_status_code: 429,
      fail_summary: 'rate limited',
      failed: true,
    });
  });

  it('keeps turn-state classification fields through detail collection', () => {
    const details = collectUsageDetailsWithEndpoint({
      apis: {
        'POST /v1/responses': {
          models: {
            'gpt-5.6-luna': {
              details: [
                {
                  timestamp: '2026-09-22T06:56:32Z',
                  turn_state_class: 'suspected',
                  turn_state_evidence: 'blocks=11;length=312',
                  failed: false,
                  tokens: { input_tokens: 1, output_tokens: 1 },
                },
                {
                  timestamp: '2026-09-22T06:56:33Z',
                  turnStateClass: 'missing',
                  turnStateEvidence: 'response_without_turn_state',
                  failed: false,
                  tokens: { input_tokens: 1, output_tokens: 1 },
                },
              ],
            },
          },
        },
      },
    });

    expect(details).toHaveLength(2);
    expect(details[0].turn_state_class).toBe('suspected');
    expect(details[0].turn_state_evidence).toBe('blocks=11;length=312');
    expect(details[1].turn_state_class).toBe('missing');
    expect(details[1].turn_state_evidence).toBe('response_without_turn_state');
  });
});
