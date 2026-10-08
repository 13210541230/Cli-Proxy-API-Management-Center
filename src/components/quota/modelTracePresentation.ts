import type { ModelTraceCredentialState, ModelTraceModel, ModelTraceRecord } from '@/services/api/modelTrace';
import type { AuthFileItem } from '@/types';

export type ModelTraceLabel = 'never' | 'running' | 'cancelling' | 'consistent' | 'suspected' | 'inconclusive' | 'failed' | 'cancelled' | 'interrupted';

export const getModelTraceLabel = (state?: {
  running?: { phase: 'collecting' | 'cancelling' };
  latest?: ModelTraceRecord;
}): ModelTraceLabel => {
  if (state?.running) return state.running.phase === 'cancelling' ? 'cancelling' : 'running';
  const record = state?.latest;
  if (!record) return 'never';
  if (record.status === 'failed' || record.status === 'cancelled' || record.status === 'interrupted') return record.status;
  if (record.status !== 'completed') return 'inconclusive';
  return record.verdict;
};

export type ModelTraceMatch = 'matched' | 'mismatched' | 'match_unknown';

export const getModelTraceMatch = (record?: ModelTraceRecord): ModelTraceMatch => {
  const attribution = record?.attribution;
  if (!record || record.status !== 'completed' || record.verdict === 'inconclusive' ||
      !record.model || !attribution?.prediction || attribution.used_outputs !== 3 || attribution.probability < .8) {
    return 'match_unknown';
  }
  return record.model === attribution.prediction ? 'matched' : 'mismatched';
};

export const supportedModelTraceModels = (
  bank: ModelTraceModel[], catalog: Array<{ id: string }>,
): ModelTraceModel[] => {
  const ids = new Set(catalog.map((model) => model.id));
  return bank.filter((model) => ids.has(model.id));
};

export const modelTraceAuthIndex = (file: AuthFileItem): string =>
  String(file.authIndex ?? file['auth_index'] ?? '').trim();

export const hasActiveModelTrace = (states: Record<string, ModelTraceCredentialState>) =>
  Object.values(states).some((state) => Boolean(state.running));
