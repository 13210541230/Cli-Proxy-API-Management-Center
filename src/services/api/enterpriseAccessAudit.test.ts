import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { enterpriseAccessAuditApi } from './enterpriseAccessAudit';

vi.mock('./client', () => ({
  apiClient: {
    get: vi.fn(),
    put: vi.fn(),
  },
}));

describe('enterpriseAccessAuditApi', () => {
  beforeEach(() => vi.clearAllMocks());

  it('queries policies by normalized short hashes', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ policies: [] });

    await enterpriseAccessAuditApi.listPolicies(['ABCDEF12', 'abcdef12', 'invalid']);

    expect(apiClient.get).toHaveBeenCalledWith('/enterprise-access-audit/policies', {
      params: { key_hash: ['abcdef12'] },
      paramsSerializer: { indexes: null },
    });
  });

  it('sends only hash-based normalized policy fields for batch updates', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ updated: ['abcdef12', 'abcdef13'] });

    await enterpriseAccessAuditApi.updatePoliciesBatch({
      key_hashes: ['ABCDEF13', 'abcdef12', 'abcdef12'],
      denied_models: [' GPT-4O ', 'gpt-4o', 'Claude-3'],
      audit_enabled: null,
    });

    expect(apiClient.put).toHaveBeenCalledWith('/enterprise-access-audit/policies/batch', {
      key_hashes: ['abcdef12', 'abcdef13'],
      denied_models: ['claude-3', 'gpt-4o'],
      audit_enabled: null,
    });
    expect(JSON.stringify(vi.mocked(apiClient.put).mock.calls[0][1])).not.toContain('apiKey');
  });

  it('keeps denied models out of an audit-only single update', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ updated: 'abcdef12' });

    await enterpriseAccessAuditApi.updatePolicy({ key_hash: 'ABCDEF12', audit_enabled: false });

    expect(apiClient.put).toHaveBeenCalledWith('/enterprise-access-audit/policy', {
      key_hash: 'abcdef12',
      audit_enabled: false,
    });
  });
});
