import { describe, expect, it } from 'vitest';
import {
  normalizeEnterprisePolicyHash,
  normalizeEnterprisePolicyHashes,
  normalizeEnterprisePolicyModels,
} from './enterpriseAccessAudit';

describe('enterprise access policy normalization', () => {
  it('trims, lowercases, deduplicates, and sorts model IDs', () => {
    expect(normalizeEnterprisePolicyModels([' GPT-4O ', 'gpt-4o', 'Claude-3'])).toEqual([
      'claude-3',
      'gpt-4o',
    ]);
  });

  it('rejects whitespace and control characters', () => {
    expect(() => normalizeEnterprisePolicyModels(['gpt 4o'])).toThrow();
    expect(() => normalizeEnterprisePolicyModels(['gpt\n4o'])).toThrow();
  });

  it('normalizes short and full hashes without exposing key values', () => {
    expect(normalizeEnterprisePolicyHash(' ABCDEF12 ')).toBe('abcdef12');
    expect(normalizeEnterprisePolicyHash('abcdef12'.repeat(8))).toBe('abcdef12');
    expect(normalizeEnterprisePolicyHashes(['ABCDEF12', 'abcdef12', 'invalid'])).toEqual(['abcdef12']);
  });
});
