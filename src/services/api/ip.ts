/**
 * IP 监控相关 API
 */

import { apiClient } from './client';

const IP_TIMEOUT_MS = 30 * 1000;

export interface IPActivity {
  ip: string;
  last_seen: string;
  total_requests: number;
  total_tokens: number;
  api_keys: Record<string, number>;
  failed_requests: number;
}

export interface ActiveIPsResponse {
  active_ips: Record<string, IPActivity>;
  count: number;
  since?: string;
  minutes?: number;
  ips?: Record<string, IPActivity>;
}

export interface IPStatisticsResponse {
  total_ips: number;
  active_ips: number;
  total_requests: number;
  total_tokens: number;
}

export interface CleanupInactiveIPsResponse {
  success: boolean;
  message: string;
  removed: number;
  hours: number;
}

export const ipApi = {
  /**
   * 获取活跃的 IP 地址列表
   * @param minutes 查询最近 N 分钟内活跃的 IP（默认 5 分钟）
   */
  getActiveIPs: (minutes: number = 5) =>
    apiClient.get<ActiveIPsResponse>(`/active-ips?minutes=${minutes}`, { timeout: IP_TIMEOUT_MS }),

  /**
   * 获取所有追踪的 IP 地址
   */
  getAllIPs: () =>
    apiClient.get<ActiveIPsResponse>('/active-ips?all=true', { timeout: IP_TIMEOUT_MS }),

  /**
   * 获取 IP 统计摘要
   */
  getStatistics: () =>
    apiClient.get<IPStatisticsResponse>('/ip-statistics', { timeout: IP_TIMEOUT_MS }),

  /**
   * 清理不活跃的 IP 记录
   * @param hours 清理超过 N 小时未活跃的 IP（默认 24 小时）
   */
  cleanupInactiveIPs: (hours: number = 24) =>
    apiClient.delete<CleanupInactiveIPsResponse>(`/inactive-ips?hours=${hours}`, { timeout: IP_TIMEOUT_MS }),
};
