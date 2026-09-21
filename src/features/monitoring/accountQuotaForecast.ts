export type AccountQuotaForecast = {
  observedSpendUsd: number;
  usedPercent: number;
  remainingPercent: number;
  estimatedTotalValueUsd: number;
  estimatedRemainingValueUsd: number;
  confidence: 'low' | 'medium' | 'high';
  cycleStartAtMs: number | null;
  cycleEndAtMs: number | null;
};

type WeeklyQuotaWindow = {
  id: string;
  remainingPercent: number | null;
  resetAtMs?: number | null;
  limitWindowSeconds?: number | null;
};

const DEFAULT_WEEKLY_WINDOW_SECONDS = 7 * 24 * 60 * 60;

export const getAccountQuotaCycleBounds = (
  weekly: WeeklyQuotaWindow | undefined
): { startAtMs: number; endAtMs: number } | null => {
  if (!weekly || weekly.id !== 'weekly') return null;
  const endAtMs =
    typeof weekly.resetAtMs === 'number' && Number.isFinite(weekly.resetAtMs) && weekly.resetAtMs > 0
      ? weekly.resetAtMs
      : null;
  if (endAtMs === null) return null;
  const windowSeconds =
    typeof weekly.limitWindowSeconds === 'number' && weekly.limitWindowSeconds > 0
      ? weekly.limitWindowSeconds
      : DEFAULT_WEEKLY_WINDOW_SECONDS;
  return {
    startAtMs: endAtMs - windowSeconds * 1000,
    endAtMs,
  };
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
  const cycle = getAccountQuotaCycleBounds(weekly);
  const cycleStartAtMs = cycle?.startAtMs ?? null;
  const cycleEndAtMs = cycle?.endAtMs ?? null;
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
    cycleStartAtMs,
    cycleEndAtMs,
  };
};
