export type AccountQuotaForecast = {
  observedSpendUsd: number;
  usedPercent: number;
  remainingPercent: number;
  estimatedTotalValueUsd: number;
  estimatedRemainingValueUsd: number;
  confidence: 'low' | 'medium' | 'high';
};

type WeeklyQuotaWindow = {
  id: string;
  remainingPercent: number | null;
};

export const buildAccountQuotaForecast = (
  observedSpendUsd: number | undefined,
  windows: WeeklyQuotaWindow[]
): AccountQuotaForecast | null => {
  const spend = Number(observedSpendUsd);
  if (!Number.isFinite(spend) || spend <= 0) return null;

  const weekly = windows.find((window) => window.id === 'weekly');
  if (!weekly || weekly.remainingPercent === null) return null;

  const remainingPercent = Math.max(0, Math.min(100, weekly.remainingPercent));
  const usedPercent = 100 - remainingPercent;
  if (usedPercent <= 0) return null;

  const estimatedTotalValueUsd = spend / (usedPercent / 100);
  const estimatedRemainingValueUsd = estimatedTotalValueUsd * (remainingPercent / 100);
  const confidence = usedPercent >= 20 ? 'high' : usedPercent >= 5 ? 'medium' : 'low';

  return {
    observedSpendUsd: spend,
    usedPercent,
    remainingPercent,
    estimatedTotalValueUsd,
    estimatedRemainingValueUsd,
    confidence,
  };
};
