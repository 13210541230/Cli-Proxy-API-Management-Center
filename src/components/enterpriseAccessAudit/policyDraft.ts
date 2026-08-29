import type {
  EnterpriseAccessAuditBatchPayload,
  EnterpriseAccessAuditSinglePayload,
} from '@/types/enterpriseAccessAudit';
import {
  normalizeEnterprisePolicyHashes,
  normalizeEnterprisePolicyModels,
} from '@/utils/enterpriseAccessAudit';

export interface EnterpriseAccessPolicyEditorDraft {
  deniedModels: string[];
  auditEnabled: boolean | null;
}

export type EnterpriseAccessPolicyMutationPlan =
  | { kind: 'noop' }
  | { kind: 'single'; payload: EnterpriseAccessAuditSinglePayload }
  | { kind: 'batch'; payload: EnterpriseAccessAuditBatchPayload };

/** Build the normalized draft shared by single-row and batch policy editors. */
export const buildEnterprisePolicyEditorDraft = (
  models: string[],
  auditEnabled: boolean | null
): EnterpriseAccessPolicyEditorDraft => ({
  deniedModels: normalizeEnterprisePolicyModels(models),
  auditEnabled,
});

/**
 * Build a safe mutation plan without treating a mixed deny-list placeholder as an explicit clear.
 * Batch audit-only changes use single-key patches because the T3 batch contract replaces models.
 */
export const buildEnterprisePolicyMutationPlan = (options: {
  mode: 'single' | 'batch';
  keyHashes: string[];
  deniedModels: string[];
  auditEnabled: boolean | null;
  modelsChanged: boolean;
  auditChanged: boolean;
}): EnterpriseAccessPolicyMutationPlan[] => {
  const keyHashes = normalizeEnterprisePolicyHashes(options.keyHashes);
  if (keyHashes.length === 0) return [{ kind: 'noop' }];
  const deniedModels = normalizeEnterprisePolicyModels(options.deniedModels);

  if (options.mode === 'single') {
    const payload: EnterpriseAccessAuditSinglePayload = { key_hash: keyHashes[0] };
    if (options.modelsChanged) payload.denied_models = deniedModels;
    if (options.auditChanged && options.auditEnabled !== null) payload.audit_enabled = options.auditEnabled;
    return options.modelsChanged || options.auditChanged ? [{ kind: 'single', payload }] : [{ kind: 'noop' }];
  }

  if (options.modelsChanged) {
    return [
      {
        kind: 'batch',
        payload: { key_hashes: keyHashes, denied_models: deniedModels, audit_enabled: options.auditEnabled },
      },
    ];
  }
  if (!options.auditChanged || options.auditEnabled === null) return [{ kind: 'noop' }];

  return keyHashes.map((key_hash) => ({
    kind: 'single',
    payload: { key_hash, audit_enabled: options.auditEnabled as boolean },
  }));
};
