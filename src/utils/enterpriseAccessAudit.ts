const hasWhitespaceOrControl = (value: string): boolean => {
  for (const character of value) {
    if (/\s/u.test(character) || character.charCodeAt(0) < 0x20 || character.charCodeAt(0) === 0x7f) {
      return true;
    }
  }
  return false;
};

/** Normalize a policy model ID exactly as the CPA plugin does. */
export const normalizeEnterprisePolicyModel = (value: string): string => {
  const normalized = String(value ?? '').trim();
  if (!normalized) throw new Error('模型 ID 不能为空');
  if (hasWhitespaceOrControl(normalized)) {
    throw new Error('模型 ID 不能包含空白或控制字符');
  }
  return normalized.replace(/[A-Z]/g, (character) => character.toLowerCase());
};

/** Return a deterministic set of model IDs for policy request bodies. */
export const normalizeEnterprisePolicyModels = (values: string[]): string[] => {
  const unique = new Set(values.map(normalizeEnterprisePolicyModel));
  return Array.from(unique).sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
};

/** Policy APIs use the plugin's short hash while binding records retain SHA-256. */
export const normalizeEnterprisePolicyHash = (value: string): string => {
  const normalized = String(value ?? '').trim().toLowerCase();
  if (/^[0-9a-f]{64}$/u.test(normalized)) return normalized.slice(0, 8);
  if (/^[0-9a-f]{8}$/u.test(normalized)) return normalized;
  return '';
};

export const normalizeEnterprisePolicyHashes = (values: string[]): string[] =>
  Array.from(new Set(values.map(normalizeEnterprisePolicyHash).filter(Boolean))).sort();
