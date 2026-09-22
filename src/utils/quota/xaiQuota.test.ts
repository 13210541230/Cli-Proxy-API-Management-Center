import { describe, expect, it } from 'vitest';
import { parseXaiMonthlyBillingPayload, parseXaiWeeklyBillingPayload } from './parsers';
import { buildXaiQuotaRows } from './builders';
import { isXaiFile } from './validators';

describe('parseXaiWeeklyBillingPayload', () => {
  it('parses percent and current period from a JSON string', () => {
    const window = parseXaiWeeklyBillingPayload(
      JSON.stringify({
        creditUsagePercent: 42.5,
        currentPeriod: { start: '2026-09-16T00:00:00Z', end: '2026-09-23T00:00:00Z' },
      })
    );
    expect(window).toMatchObject({
      percent: 42.5,
      periodStart: '2026-09-16T00:00:00Z',
      periodEnd: '2026-09-23T00:00:00Z',
    });
  });

  it('clamps percent into 0..100', () => {
    expect(parseXaiWeeklyBillingPayload({ creditUsagePercent: 250 })?.percent).toBe(100);
    expect(parseXaiWeeklyBillingPayload({ creditUsagePercent: -3 })?.percent).toBe(0);
  });

  it('accepts numeric strings for percent', () => {
    expect(parseXaiWeeklyBillingPayload({ creditUsagePercent: '12.5' })?.percent).toBe(12.5);
  });

  it('returns null for malformed or empty payloads', () => {
    expect(parseXaiWeeklyBillingPayload('not-json')).toBeNull();
    expect(parseXaiWeeklyBillingPayload({ unexpected: true })).toBeNull();
    expect(parseXaiWeeklyBillingPayload(null)).toBeNull();
  });
});

describe('parseXaiMonthlyBillingPayload', () => {
  it('derives percent from monthlyLimit and used', () => {
    const window = parseXaiMonthlyBillingPayload({
      monthlyLimit: 150,
      used: 30,
      currentPeriod: { end: '2026-10-01T00:00:00Z' },
    });
    expect(window?.percent).toBe(20);
    expect(window?.detail).toBe('30 / 150');
    expect(window?.periodEnd).toBe('2026-10-01T00:00:00Z');
  });

  it('falls back to creditUsagePercent when money fields are missing', () => {
    const window = parseXaiMonthlyBillingPayload({ creditUsagePercent: 60 });
    expect(window?.percent).toBe(60);
  });
});

describe('buildXaiQuotaRows', () => {
  it('builds weekly and monthly rows', () => {
    const rows = buildXaiQuotaRows(
      { percent: 10, periodStart: '2026-09-16T00:00:00Z', periodEnd: '2026-09-23T00:00:00Z' },
      { percent: 20, periodEnd: '2026-10-01T00:00:00Z', detail: '30 / 150' }
    );
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ id: 'xai-weekly', labelKey: 'xai_quota.weekly', percent: 10 });
    expect(rows[1]).toMatchObject({ id: 'xai-monthly', labelKey: 'xai_quota.monthly', detail: '30 / 150' });
  });

  it('includes only windows that parsed successfully', () => {
    expect(buildXaiQuotaRows(null, { percent: 5 })).toHaveLength(1);
    expect(buildXaiQuotaRows(null, null)).toHaveLength(0);
  });

  it('keeps a null percent when the window only carries a period', () => {
    const rows = buildXaiQuotaRows({ periodEnd: '2026-09-23T00:00:00Z' }, null);
    expect(rows[0].percent).toBeNull();
    expect(rows[0].periodEnd).toBe('2026-09-23T00:00:00Z');
  });
});

describe('isXaiFile', () => {
  it('matches xai provider auth files only', () => {
    expect(isXaiFile({ name: 'a.json', provider: 'xai' } as never)).toBe(true);
    expect(isXaiFile({ name: 'a.json', type: 'xai' } as never)).toBe(true);
    expect(isXaiFile({ name: 'a.json', provider: 'grok' } as never)).toBe(false);
    expect(isXaiFile({ name: 'a.json', provider: 'codex' } as never)).toBe(false);
  });
});
