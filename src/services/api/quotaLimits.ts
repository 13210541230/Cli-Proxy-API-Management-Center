import { apiClient } from './client';

export type QuotaLimitMode = 'cost' | 'tokens';

export interface SpendLimit {
  daily_cents: number;
  weekly_cents: number;
  daily_tokens?: number;
  weekly_tokens?: number;
}

export interface SpendLimitEntry {
  apply_to: string;
  apply_value: string;
  daily_cents: number;
  weekly_cents: number;
  daily_tokens?: number;
  weekly_tokens?: number;
}

export interface QuotaConfig {
  enabled: boolean;
  mode?: QuotaLimitMode;
  db_path: string;
  default: SpendLimit;
  overrides: SpendLimitEntry[];
}

export interface DowngradeQuotaConfig extends QuotaConfig {
  fallback_model: string;
}

export const quotaLimitsApi = {
  getConfig: () => apiClient.get<QuotaConfig>('/quota/config'),

  updateConfig: (cfg: Partial<QuotaConfig>) =>
    apiClient.put<{ status: string }>('/quota/config', cfg),

  getDowngradeConfig: () => apiClient.get<DowngradeQuotaConfig>('/quota/downgrade-config'),

  updateDowngradeConfig: (cfg: Partial<DowngradeQuotaConfig>) =>
    apiClient.put<{ status: string }>('/quota/downgrade-config', cfg),
};
