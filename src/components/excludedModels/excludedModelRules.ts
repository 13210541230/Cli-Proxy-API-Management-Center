/**
 * The single source of truth for excluded-model rules.
 *
 * This consolidates the former `excludedModelSelection.ts` (text/array-based) and
 * `oauthExcludedRules.ts` (Set-based) modules. They duplicated the same domain model
 * and normalization logic, differing only in whether input came from text or an iterable.
 *
 * Rule semantics match the backend:
 * - Matching is case-insensitive.
 * - `*` matches any sequence; all other characters are literal (`.` in `gpt-4.1` is not a wildcard).
 * - Deduplication uses a lowercase key while preserving the first spelling.
 */

/** Backend encoding for disabling an entire provider; only the provider form's disabled toggle owns it. */
export const DISABLE_ALL_RULE = '*';

const ruleKey = (value: string): string => value.trim().toLowerCase();

export const isWildcardRule = (rule: string): boolean => rule.includes('*');

export function normalizeExcludedRules(values: Iterable<string>): string[] {
  const seen = new Set<string>();
  const rules: string[] = [];

  for (const value of values) {
    const rule = value.trim();
    const key = ruleKey(rule);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    rules.push(rule);
  }

  return rules;
}

export const parseExcludedRulesText = (text: string): string[] =>
  normalizeExcludedRules(text.split(/\r?\n/));

export const formatExcludedRulesText = (rules: readonly string[]): string => rules.join('\n');

export function matchesExcludedRule(rule: string, modelId: string): boolean {
  const normalizedRule = ruleKey(rule);
  const normalizedModel = ruleKey(modelId);
  if (!normalizedRule || !normalizedModel) return false;
  if (!isWildcardRule(normalizedRule)) return normalizedRule === normalizedModel;

  // Split on `*`, escape regex metacharacters in each segment, then join with `.*`; only `*` is a wildcard.
  const escaped = normalizedRule
    .split('*')
    .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .join('.*');
  return new RegExp(`^${escaped}$`, 'i').test(normalizedModel);
}

/** Whether any **wildcard** rule matches this model; exact rules do not count. */
export const isMatchedByWildcardRule = (rules: Iterable<string>, modelId: string): boolean =>
  Array.from(rules).some((rule) => isWildcardRule(rule) && matchesExcludedRule(rule, modelId));

/** Whether the rule list contains a case-insensitive literal match for the candidate. Wildcards are not expanded. */
export function hasExcludedRule(rules: Iterable<string>, candidate: string): boolean {
  const candidateKey = ruleKey(candidate);
  if (!candidateKey) return false;
  return Array.from(rules).some((rule) => ruleKey(rule) === candidateKey);
}

/**
 * Add or remove a literal rule.
 *
 * Filtering uses the normalized key and does not exempt rules containing `*`. The former
 * `toggleExcludedModel` prevented wildcard deletion because two independent editors (the
 * list for exact rules and a textarea for wildcard rules) could overwrite each other. In the
 * unified component both editors share one state, so that guard belongs in the component.
 */
export function toggleExcludedRule(
  rules: Iterable<string>,
  candidate: string,
  excluded: boolean
): string[] {
  const candidateRule = candidate.trim();
  const candidateKey = ruleKey(candidateRule);
  const next = normalizeExcludedRules(rules).filter((rule) => ruleKey(rule) !== candidateKey);

  if (excluded && candidateKey) next.push(candidateRule);
  return next;
}

export interface SplitExcludedRules {
  /** Exact rules matching the catalog, normalized to the catalog's spelling for checkbox-driven IDs. */
  exactRules: string[];
  /** Rules containing `*`, preserving their configured spelling. */
  wildcardRules: string[];
  /** Exact rules absent from the catalog (such as retired model IDs), preserving configured spelling. */
  unknownRules: string[];
  /** `wildcardRules ∪ unknownRules` in original order; the textarea and order-sensitive diffs depend on it. */
  customRules: string[];
}

export function splitExcludedRules(
  rules: Iterable<string>,
  candidateIds: readonly string[]
): SplitExcludedRules {
  const candidateByKey = new Map(candidateIds.map((id) => [ruleKey(id), id]));
  const exactRules: string[] = [];
  const wildcardRules: string[] = [];
  const unknownRules: string[] = [];
  const customRules: string[] = [];

  normalizeExcludedRules(rules).forEach((rule) => {
    if (isWildcardRule(rule)) {
      wildcardRules.push(rule);
      customRules.push(rule);
      return;
    }
    const candidate = candidateByKey.get(ruleKey(rule));
    if (candidate) {
      exactRules.push(candidate);
      return;
    }
    unknownRules.push(rule);
    customRules.push(rule);
  });

  return { exactRules, wildcardRules, unknownRules, customRules };
}

/** Replace the custom half (wildcards and out-of-catalog exact rules) from text while retaining exact selections. */
export function replaceCustomExcludedRules(
  rules: Iterable<string>,
  candidateIds: readonly string[],
  text: string
): string[] {
  const { exactRules } = splitExcludedRules(rules, candidateIds);
  return normalizeExcludedRules([...exactRules, ...parseExcludedRulesText(text)]);
}

/* -------------------------------------------------------------------------- */
/* Derived display values                                                       */
/* -------------------------------------------------------------------------- */

/**
 * Exclusion state for one model.
 *
 * `both` is the subtle case: the model is explicitly selected and also matched by a wildcard.
 * Unchecking it does not stop excluding it, so the row must not visually appear unchecked or
 * users may think the action failed. The old UI hid this state entirely.
 */
export type ModelExclusionState =
  | { state: 'included' }
  | { state: 'excluded'; by: 'exact' }
  | { state: 'excluded'; by: 'wildcard'; rule: string }
  | { state: 'excluded'; by: 'both'; rule: string };

export function getModelExclusionState(
  rules: readonly string[],
  modelId: string
): ModelExclusionState {
  const modelKey = ruleKey(modelId);
  if (!modelKey) return { state: 'included' };

  let hasExact = false;
  let wildcard: string | undefined;

  for (const rule of rules) {
    if (isWildcardRule(rule)) {
      if (wildcard === undefined && matchesExcludedRule(rule, modelId)) wildcard = rule;
    } else if (!hasExact && ruleKey(rule) === modelKey) {
      hasExact = true;
    }
  }

  if (hasExact && wildcard !== undefined) return { state: 'excluded', by: 'both', rule: wildcard };
  if (hasExact) return { state: 'excluded', by: 'exact' };
  if (wildcard !== undefined) return { state: 'excluded', by: 'wildcard', rule: wildcard };
  return { state: 'included' };
}

export const isModelExcluded = (rules: readonly string[], modelId: string): boolean =>
  getModelExclusionState(rules, modelId).state === 'excluded';

export interface RuleMatchSummary {
  rule: string;
  /** Catalog models matched by this rule, in catalog order. */
  matched: string[];
  matchCount: number;
}

/** Models matched by each rule; the wildcard editor uses this for live feedback. */
export const matchedModelsByRule = (
  rules: readonly string[],
  candidateIds: readonly string[]
): RuleMatchSummary[] =>
  rules.map((rule) => {
    const matched = candidateIds.filter((id) => matchesExcludedRule(rule, id));
    return { rule, matched, matchCount: matched.length };
  });

export interface ExclusionStats {
  total: number;
  excluded: number;
  available: number;
}

/**
 * Data source for the summary and meter.
 *
 * `excluded` counts catalog models matched by any rule, not `rules.length`: `gpt-5-*` may
 * match six models or none. Using the rule count would repeat the old UI's misleading metric;
 * the numerator and denominator must come from the same catalog.
 */
export function summarizeExclusion(
  rules: readonly string[],
  candidateIds: readonly string[]
): ExclusionStats {
  const total = candidateIds.length;
  const excluded = candidateIds.reduce(
    (count, id) => (isModelExcluded(rules, id) ? count + 1 : count),
    0
  );
  return { total, excluded, available: total - excluded };
}
