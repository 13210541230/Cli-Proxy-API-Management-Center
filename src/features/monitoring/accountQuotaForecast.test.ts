import { describe, expect, it } from 'vitest';
import { buildAccountQuotaForecast } from './accountQuotaForecast';

describe('buildAccountQuotaForecast', () => {
  it('estimates total and remaining value from the weekly provider percentage', () => {
    expect(
      buildAccountQuotaForecast(20, [
        { id: 'five-hour', remainingPercent: 40 },
        { id: 'weekly', remainingPercent: 75 },
      ])
    ).toEqual({
      observedSpendUsd: 20,
      usedPercent: 25,
      remainingPercent: 75,
      estimatedTotalValueUsd: 80,
      estimatedRemainingValueUsd: 60,
      confidence: 'high',
    });
  });

  it('does not estimate without weekly usage or with zero observed spend', () => {
    expect(buildAccountQuotaForecast(0, [{ id: 'weekly', remainingPercent: 50 }])).toBeNull();
    expect(buildAccountQuotaForecast(20, [{ id: 'five-hour', remainingPercent: 50 }])).toBeNull();
    expect(buildAccountQuotaForecast(20, [{ id: 'weekly', remainingPercent: 100 }])).toBeNull();
  });
});
