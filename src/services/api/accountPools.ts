import { apiClient } from './client';
import type {
  AccountPool,
  AccountPoolMember,
  AccountPoolSnapshot,
} from '@/types/accountPool';

const BASE = '/account-pools';

export const accountPoolsApi = {
  getSnapshot: () => apiClient.get<AccountPoolSnapshot>(BASE),

  sync: () => apiClient.post<AccountPoolSnapshot>(`${BASE}/sync`),

  create: (pool: Pick<AccountPool, 'id' | 'name' | 'provider' | 'enabled'>) =>
    apiClient.post<AccountPoolSnapshot>(BASE, pool),

  update: (id: string, pool: Pick<AccountPool, 'name' | 'provider' | 'enabled'>) =>
    apiClient.put<AccountPoolSnapshot>(`${BASE}/${encodeURIComponent(id)}`, pool),

  remove: (id: string) =>
    apiClient.delete<AccountPoolSnapshot>(`${BASE}/${encodeURIComponent(id)}`),

  replaceMembers: (id: string, items: AccountPoolMember[]) =>
    apiClient.put<AccountPoolSnapshot>(`${BASE}/${encodeURIComponent(id)}/members`, { items }),

  replaceBindings: (items: Array<{ apiKeyHash: string; poolId: string }>) =>
    apiClient.put<AccountPoolSnapshot>(`${BASE}/bindings`, { items }),
};
