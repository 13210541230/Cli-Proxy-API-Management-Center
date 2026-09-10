import type { UsageAnalyticsRequest } from '@/services/api/usageService';

export const buildMonitoringAnalyticsRequest = (
  fromMs: number,
  toMs: number,
  reasoningEffort: string,
  filters: Record<string, string> = {},
  includeApiKeyTimeline = false
): UsageAnalyticsRequest => {
  const include = [
    'summary',
    'timeline',
    'model_stats',
    'account_stats',
    'api_key_stats',
    'provider_stats',
    'reasoning_stats',
    ...(includeApiKeyTimeline ? ['api_key_timeline'] : []),
    'events',
  ];
  const requestFilters = { ...filters };
  if (reasoningEffort !== 'all') requestFilters.reasoning_effort = reasoningEffort;
  return {
    from_ms: fromMs,
    to_ms: Math.max(toMs, fromMs + 1),
    include,
    filters: Object.keys(requestFilters).length > 0 ? requestFilters : undefined,
    events_page: { limit: 200, include_total_count: false },
  };
};
