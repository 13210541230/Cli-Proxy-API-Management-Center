import { apiClient } from './client';
import type { ManagementPluginListResponse } from '@/types/plugin';

export const pluginsApi = {
  list: () => apiClient.get<ManagementPluginListResponse>('/plugins'),
};
