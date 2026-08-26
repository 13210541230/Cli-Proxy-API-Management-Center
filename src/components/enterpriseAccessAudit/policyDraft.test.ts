import { describe, expect, it } from 'vitest';
import { buildEnterprisePolicyEditorDraft, buildEnterprisePolicyMutationPlan } from './policyDraft';

describe('enterprise access policy editor draft', () => {
  it('normalizes a clearable deny list while preserving an unchanged audit toggle', () => {
    expect(buildEnterprisePolicyEditorDraft([' B ', 'a', 'a'], null)).toEqual({
      deniedModels: ['a', 'b'],
      auditEnabled: null,
    });
  });

  it('keeps a dynamic model ID manually submittable without catalog suggestions', () => {
    expect(buildEnterprisePolicyMutationPlan({
      mode: 'single',
      keyHashes: ['ABCDEF12'],
      deniedModels: [' Dynamic-Model-9 '],
      auditEnabled: null,
      modelsChanged: true,
      auditChanged: false,
    })).toEqual([{
      kind: 'single',
      payload: { key_hash: 'abcdef12', denied_models: ['dynamic-model-9'] },
    }]);
  });

  it('uses per-key audit-only patches for mixed deny lists', () => {
    expect(buildEnterprisePolicyMutationPlan({
      mode: 'batch',
      keyHashes: ['ABCDEF13', 'ABCDEF12'],
      deniedModels: [],
      auditEnabled: false,
      modelsChanged: false,
      auditChanged: true,
    })).toEqual([
      { kind: 'single', payload: { key_hash: 'abcdef12', audit_enabled: false } },
      { kind: 'single', payload: { key_hash: 'abcdef13', audit_enabled: false } },
    ]);
  });

  it('uses the atomic batch endpoint when models change and preserves audit with null', () => {
    expect(buildEnterprisePolicyMutationPlan({
      mode: 'batch',
      keyHashes: ['abcdef12', 'abcdef13'],
      deniedModels: [' Z ', 'a', 'z'],
      auditEnabled: null,
      modelsChanged: true,
      auditChanged: false,
    })).toEqual([{
      kind: 'batch',
      payload: {
        key_hashes: ['abcdef12', 'abcdef13'],
        denied_models: ['a', 'z'],
        audit_enabled: null,
      },
    }]);
  });

  it('does not mutate policies for an unchanged batch with an unchanged audit selection', () => {
    expect(buildEnterprisePolicyMutationPlan({
      mode: 'batch',
      keyHashes: ['abcdef12'],
      deniedModels: [],
      auditEnabled: null,
      modelsChanged: false,
      auditChanged: false,
    })).toEqual([{ kind: 'noop' }]);
  });
});
