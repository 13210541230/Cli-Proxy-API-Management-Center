import { apiClient } from './client';

export interface ModelTraceModel {
  id: string;
  display_name: string;
}
export interface ModelTraceProgress {
  id: string;
  model: string;
  phase: 'collecting' | 'cancelling';
  done: number;
  total: number;
  started_at: string;
}
export interface ModelTraceRecord {
  id: string;
  time: string;
  model: string;
  status: 'running' | 'completed' | 'partial' | 'failed' | 'cancelled' | 'interrupted';
  verdict: 'consistent' | 'suspected' | 'inconclusive';
  bank_revision: string;
  duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  attribution?: {
    prediction: string;
    probability: number;
    used_outputs: number;
    family_prediction_name?: string;
    family_probability?: number;
    results?: Array<{ model: string; display_name: string; probability: number }>;
  };
  samples?: Array<{
    prompt: string;
    expected_count: number;
    text?: string;
    error?: string;
    parsed_numbers: number;
    accepted: boolean;
  }>;
  error?: string;
  storage_error?: string;
}
export interface ModelTraceCredentialState {
  running?: ModelTraceProgress;
  latest?: ModelTraceRecord;
}
export interface ModelTraceState {
  bank_revision: string;
  models: ModelTraceModel[];
  states: Record<string, ModelTraceCredentialState>;
  storage_error?: string;
}

const endpoint = '/auth-files/modeltrace';
export const modelTraceApi = {
  state: (signal?: AbortSignal) => apiClient.get<ModelTraceState>(endpoint, { signal }),
  async start(authIndex: string, model: string, signal?: AbortSignal) {
    const data = await apiClient.post<{ run: ModelTraceProgress }>(endpoint, {
      auth_index: authIndex, model,
    }, { signal });
    return data.run;
  },
  cancel: (authIndex: string, signal?: AbortSignal) =>
    apiClient.delete<{ cancelled: boolean }>(endpoint, { params: { auth_index: authIndex }, signal }),
  async record(authIndex: string, id?: string, signal?: AbortSignal) {
    const data = await apiClient.get<{ record: ModelTraceRecord }>(`${endpoint}/record`, {
      params: { auth_index: authIndex, id }, signal,
    });
    return data.record;
  },
};
