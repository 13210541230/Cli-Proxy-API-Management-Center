import { apiClient } from './client';

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
      key_hash: keyHash,
      reason,
      expires_in_seconds: expiresInSeconds ?? 0,
    }),

  resumeKey: (keyHash: string) =>
    apiClient.post<{ status: string }>('/quota/resume', { key_hash: keyHash }),

  listPaused: () => apiClient.get<PausedKeysResponse>('/quota/paused'),
};
