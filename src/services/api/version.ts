/**
 * 版本相关 API
 */

import { apiClient } from './client';

export interface UpdateInfo {
  current_version: string;
  latest_version: string;
  has_update: boolean;
  download_url?: string;
  release_url?: string;
  release_notes?: string;
}

export const versionApi = {
  checkLatest: () => apiClient.get<Record<string, unknown>>('/latest-version'),
  
  /**
   * 检查 CLI Proxy API 是否有新版本
   */
  checkUpdate: () => apiClient.get<UpdateInfo>('/check-update')
};
