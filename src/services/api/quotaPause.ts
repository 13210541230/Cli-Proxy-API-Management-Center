import { apiClient } from './client';

const normalizePauseKeyHash = (value: string): string => {
  const normalized = value.trim();
  if (/^[0-9a-f]{64}$/i.test(normalized)) {
    return normalized.slice(0, 8).toLowerCase();
  }
  if (/^[0-9a-f]{8}$/i.test(normalized)) {
    return normalized.toLowerCase();
  }
  return normalized;
};

export interface PauseEntry {
  key_hash: string;
  reason: string;
  paused_at: string;
  expires_at: string;
  created_at: string;
}

export interface PausedKeysResponse {
  entries: PauseEntry[];
}

export const quotaPauseApi = {
  pauseKey: (keyHash: string, reason: string, expiresInSeconds?: number) =>
    apiClient.post<{ status: string }>('/quota/pause', {
      key_hash: normalizePauseKeyHash(keyHash),
      reason,
      expires_in_seconds: expiresInSeconds ?? 0,
    }),

  resumeKey: (keyHash: string) =>
    apiClient.post<{ status: string }>('/quota/resume', { key_hash: normalizePauseKeyHash(keyHash) }),

  listPaused: () => apiClient.get<PausedKeysResponse>('/quota/paused'),
};
