import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useEnterpriseAccessAuditStore } from './useEnterpriseAccessAuditStore';
import { enterpriseAccessAuditApi } from '@/services/api/enterpriseAccessAudit';

vi.mock('@/services/api/enterpriseAccessAudit', () => ({
  enterpriseAccessAuditApi: {
    listPolicies: vi.fn(),
    updatePolicy: vi.fn(),
    updatePoliciesBatch: vi.fn(),
  },
}));

const listPolicies = vi.mocked(enterpriseAccessAuditApi.listPolicies);

describe('useEnterpriseAccessAuditStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useEnterpriseAccessAuditStore.setState({ policies: {}, status: 'idle', error: null, mutating: false });
  });

  it.each([
    ['404', { status: 404 }],
    ['405', { status: 405 }],
    ['500', { status: 500 }],
    ['undefined status', new Error('network unavailable')],
  ])('marks plugin %s as unavailable', async (_label, error) => {
    listPolicies.mockRejectedValueOnce(error);

    await expect(useEnterpriseAccessAuditStore.getState().loadPolicies(['abcdef12'])).rejects.toEqual(error);

    expect(useEnterpriseAccessAuditStore.getState().status).toBe('unavailable');
  });

  it('keeps an ordinary client error distinct from unavailable', async () => {
    const error = Object.assign(new Error('bad request'), { status: 400 });
    listPolicies.mockRejectedValueOnce(error);

    await expect(useEnterpriseAccessAuditStore.getState().loadPolicies(['abcdef12'])).rejects.toEqual(error);

    expect(useEnterpriseAccessAuditStore.getState().status).toBe('error');
  });
});
