import { useCallback, useMemo, useReducer } from 'react';
import { isMap, parse as parseYaml, parseDocument } from 'yaml';
import type {
  PayloadFilterRule,
  PayloadHeaderEntry,
  PayloadModelEntry,
  PayloadParamEntry,
  PayloadParamValueType,
  PayloadRule,
  PluginStoreAuthRule,
  VisualConfigValues,
  VisualConfigValidationErrors,
  PayloadParamValidationErrorCode,
} from '@/types/visualConfig';
import { DEFAULT_VISUAL_VALUES } from '@/types/visualConfig';

function asRecord(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function extractApiKeyValue(raw: unknown): string | null {
  if (typeof raw === 'string') {
    const trimmed = raw.trim();
    return trimmed ? trimmed : null;
  }

  const record = asRecord(raw);
  if (!record) return null;

  const candidates = [record['api-key'], record.apiKey, record.key, record.Key];
  for (const candidate of candidates) {
    if (typeof candidate === 'string') {
      const trimmed = candidate.trim();
      if (trimmed) return trimmed;
    }
  }

  return null;
}

function parseApiKeysText(raw: unknown): string {
  if (!Array.isArray(raw)) return '';

  const keys: string[] = [];
  for (const item of raw) {
    const key = extractApiKeyValue(item);
    if (key) keys.push(key);
  }
  return keys.join('\n');
}

function resolveApiKeysText(parsed: Record<string, unknown>): string {
  if (Object.prototype.hasOwnProperty.call(parsed, 'api-keys')) {
    return parseApiKeysText(parsed['api-keys']);
  }

  const auth = asRecord(parsed.auth);
  const providers = asRecord(auth?.providers);
  const configApiKeyProvider = asRecord(providers?.['config-api-key']);
  if (!configApiKeyProvider) return '';

  if (Object.prototype.hasOwnProperty.call(configApiKeyProvider, 'api-key-entries')) {
    return parseApiKeysText(configApiKeyProvider['api-key-entries']);
  }

  return parseApiKeysText(configApiKeyProvider['api-keys']);
}

type YamlDocument = ReturnType<typeof parseDocument>;
type YamlPath = string[];

function docHas(doc: YamlDocument, path: YamlPath): boolean {
  return doc.hasIn(path);
}

function ensureMapInDoc(doc: YamlDocument, path: YamlPath): void {
  const existing = doc.getIn(path, true);
  if (isMap(existing)) return;
  // Use a YAML node here; plain objects are not treated as collections by subsequent `setIn`.
  doc.setIn(path, doc.createNode({}));
}

function deleteIfMapEmpty(doc: YamlDocument, path: YamlPath): void {
  const value = doc.getIn(path, true);
  if (!isMap(value)) return;
  if (value.items.length === 0) doc.deleteIn(path);
}

function setBooleanInDoc(doc: YamlDocument, path: YamlPath, value: boolean): void {
  if (value) {
    doc.setIn(path, true);
    return;
  }
  if (docHas(doc, path)) doc.setIn(path, false);
}

function shouldWriteManagedField(
  doc: YamlDocument,
  path: YamlPath,
  dirtyFields: Set<string>,
  dirtyKey: string
): boolean {
  // Optional fields managed by the visual editor must not be created during unrelated saves.
  // Only materialize them when the YAML already had the key or the user changed that field.
  // Use this guard for future optional visual-editor fields instead of unconditional `setIn`.
  return docHas(doc, path) || dirtyFields.has(dirtyKey);
}

function setManagedBooleanInDoc(
  doc: YamlDocument,
  path: YamlPath,
  value: boolean,
  dirtyFields: Set<string>,
  dirtyKey: string
): void {
  if (!shouldWriteManagedField(doc, path, dirtyFields, dirtyKey)) return;
  if (dirtyFields.has(dirtyKey)) {
    doc.setIn(path, value);
    return;
  }
  setBooleanInDoc(doc, path, value);
}

function setDisableImageGenerationInDoc(
  doc: YamlDocument,
  path: YamlPath,
  value: VisualConfigValues['disableImageGeneration']
): void {
  if (value === 'chat' || value === 'passthrough') {
    doc.setIn(path, value);
    return;
  }
  if (value === 'true') {
    doc.setIn(path, true);
    return;
  }
  if (docHas(doc, path)) doc.setIn(path, false);
}

function setStringInDoc(doc: YamlDocument, path: YamlPath, value: unknown): void {
  const safe = typeof value === 'string' ? value : '';
  const trimmed = safe.trim();
  if (trimmed !== '') {
    doc.setIn(path, safe);
    return;
  }
  // Preserve existing empty-string keys to avoid dropping template blocks/comments.
  // Only keep the key when it already exists in the YAML.
  if (docHas(doc, path)) {
    doc.setIn(path, '');
  }
}

function setIntFromStringInDoc(doc: YamlDocument, path: YamlPath, value: unknown): void {
  const safe = typeof value === 'string' ? value : '';
  const trimmed = safe.trim();
  if (trimmed === '') {
    if (docHas(doc, path)) doc.deleteIn(path);
    return;
  }

  if (!/^-?\d+$/.test(trimmed)) {
    return;
  }

  const parsed = Number(trimmed);
  if (Number.isFinite(parsed)) {
    doc.setIn(path, parsed);
    return;
  }
}

function getNonNegativeIntegerError(value: string): 'non_negative_integer' | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^-?\d+$/.test(trimmed)) return 'non_negative_integer';
  return Number(trimmed) >= 0 ? undefined : 'non_negative_integer';
}

function getIntegerError(value: string): 'integer' | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  return /^-?\d+$/.test(trimmed) && Number.isSafeInteger(Number(trimmed)) ? undefined : 'integer';
}

function getPortError(value: string): 'port_range' | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^\d+$/.test(trimmed)) return 'port_range';
  const parsed = Number(trimmed);
  return parsed >= 1 && parsed <= 65535 ? undefined : 'port_range';
}

function getRedisUsageQueueRetentionError(value: string): 'retention_seconds_range' | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^\d+$/.test(trimmed)) return 'retention_seconds_range';
  const parsed = Number(trimmed);
  return parsed >= 0 && parsed <= 3600 ? undefined : 'retention_seconds_range';
}

export function getVisualConfigValidationErrors(
  values: VisualConfigValues
): VisualConfigValidationErrors {
  return {
    port: getPortError(values.port),
    errorLogsMaxFiles: getNonNegativeIntegerError(values.errorLogsMaxFiles),
    logsMaxTotalSizeMb: getNonNegativeIntegerError(values.logsMaxTotalSizeMb),
    redisUsageQueueRetentionSeconds: getRedisUsageQueueRetentionError(
      values.redisUsageQueueRetentionSeconds
    ),
    requestRetry: getNonNegativeIntegerError(values.requestRetry),
    maxRetryCredentials: getNonNegativeIntegerError(values.maxRetryCredentials),
    maxRetryInterval: getNonNegativeIntegerError(values.maxRetryInterval),
    authAutoRefreshWorkers: getIntegerError(values.authAutoRefreshWorkers),
    'streaming.keepaliveSeconds': getNonNegativeIntegerError(values.streaming.keepaliveSeconds),
    'streaming.bootstrapRetries': getNonNegativeIntegerError(values.streaming.bootstrapRetries),
    'streaming.nonstreamKeepaliveInterval': getNonNegativeIntegerError(
      values.streaming.nonstreamKeepaliveInterval
    ),
  };
}

export function getPayloadParamValidationError(
  param: PayloadParamEntry
): PayloadParamValidationErrorCode | undefined {
  const trimmedValue = param.value.trim();

  switch (param.valueType) {
    case 'number': {
      if (!trimmedValue) return 'payload_invalid_number';
      const parsed = Number(trimmedValue);
      return Number.isFinite(parsed) ? undefined : 'payload_invalid_number';
    }
    case 'boolean': {
      const normalized = trimmedValue.toLowerCase();
      return normalized === 'true' || normalized === 'false'
        ? undefined
        : 'payload_invalid_boolean';
    }
    case 'json': {
      if (!trimmedValue) return 'payload_invalid_json';
      try {
        JSON.parse(param.value);
        return undefined;
      } catch {
        return 'payload_invalid_json';
      }
    }
    default:
      return undefined;
  }
}

function hasPayloadParamValidationErrors(rules: PayloadRule[]): boolean {
  return rules.some((rule) =>
    rule.params.some((param) => Boolean(getPayloadParamValidationError(param)))
  );
}

function deepClone<T>(value: T): T {
  if (typeof structuredClone === 'function') return structuredClone(value);
  return JSON.parse(JSON.stringify(value)) as T;
}

function arePayloadModelEntriesEqual(
  left: PayloadRule['models'],
  right: PayloadRule['models']
): boolean {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i += 1) {
    const a = left[i];
    const b = right[i];
    if (!a || !b) return false;
    if (
      a.id !== b.id ||
      a.name !== b.name ||
      a.protocol !== b.protocol ||
      a.fromProtocol !== b.fromProtocol
    ) {
      return false;
    }
    if (!arePayloadHeaderEntriesEqual(a.headers, b.headers)) return false;
    if (!arePayloadParamEntriesEqual(a.match ?? [], b.match ?? [])) return false;
    if (!arePayloadParamEntriesEqual(a.notMatch ?? [], b.notMatch ?? [])) return false;
    if (!areStringArraysEqual(a.exist, b.exist) || !areStringArraysEqual(a.notExist, b.notExist)) {
      return false;
    }
  }
  return true;
}

function arePayloadHeaderEntriesEqual(
  left: PayloadHeaderEntry[] | undefined,
  right: PayloadHeaderEntry[] | undefined
): boolean {
  const leftEntries = left ?? [];
  const rightEntries = right ?? [];
  if (leftEntries.length !== rightEntries.length) return false;
  return leftEntries.every((entry, index) => {
    const other = rightEntries[index];
    return Boolean(
      other && entry.id === other.id && entry.name === other.name && entry.value === other.value
    );
  });
}

function areStringArraysEqual(left: string[] | undefined, right: string[] | undefined): boolean {
  const leftItems = left ?? [];
  const rightItems = right ?? [];
  return (
    leftItems.length === rightItems.length &&
    leftItems.every((item, index) => item === rightItems[index])
  );
}

function arePluginStoreAuthRulesEqual(
  left: PluginStoreAuthRule[],
  right: PluginStoreAuthRule[]
): boolean {
  if (left.length !== right.length) return false;
  return left.every((rule, index) => {
    const other = right[index];
    return Boolean(
      other &&
      rule.match === other.match &&
      rule.type === other.type &&
      areStringArraysEqual(rule.applyTo, other.applyTo) &&
      rule.tokenEnv === other.tokenEnv &&
      rule.usernameEnv === other.usernameEnv &&
      rule.passwordEnv === other.passwordEnv &&
      rule.headerName === other.headerName &&
      rule.headerValueEnv === other.headerValueEnv &&
      rule.allowInsecure === other.allowInsecure
    );
  });
}

function arePayloadParamEntriesEqual(
  left: PayloadRule['params'],
  right: PayloadRule['params']
): boolean {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i += 1) {
    const a = left[i];
    const b = right[i];
    if (!a || !b) return false;
    if (a.id !== b.id || a.path !== b.path || a.valueType !== b.valueType || a.value !== b.value) {
      return false;
    }
  }
  return true;
}

function arePayloadRulesEqual(left: PayloadRule[], right: PayloadRule[]): boolean {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i += 1) {
    const a = left[i];
    const b = right[i];
    if (!a || !b) return false;
    if (a.id !== b.id) return false;
    if (!arePayloadModelEntriesEqual(a.models, b.models)) return false;
    if (!arePayloadParamEntriesEqual(a.params, b.params)) return false;
  }
  return true;
}

function arePayloadFilterRulesEqual(
  left: PayloadFilterRule[],
  right: PayloadFilterRule[]
): boolean {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i += 1) {
    const a = left[i];
    const b = right[i];
    if (!a || !b) return false;
    if (a.id !== b.id) return false;
    if (!arePayloadModelEntriesEqual(a.models, b.models)) return false;
    if (a.params.length !== b.params.length) return false;
    for (let j = 0; j < a.params.length; j += 1) {
      if (a.params[j] !== b.params[j]) return false;
    }
  }
  return true;
}

function parsePayloadParamValue(raw: unknown): { valueType: PayloadParamValueType; value: string } {
  if (typeof raw === 'number') {
    return { valueType: 'number', value: String(raw) };
  }

  if (typeof raw === 'boolean') {
    return { valueType: 'boolean', value: String(raw) };
  }

  if (raw === null || typeof raw === 'object') {
    try {
      const json = JSON.stringify(raw, null, 2);
      return { valueType: 'json', value: json ?? 'null' };
    } catch {
      return { valueType: 'json', value: String(raw) };
    }
  }

  return { valueType: 'string', value: String(raw ?? '') };
}

function parseRawPayloadParamValue(raw: unknown): string {
  if (typeof raw === 'string') return raw;

  try {
    const json = JSON.stringify(raw, null, 2);
    return json ?? '';
  } catch {
    return String(raw ?? '');
  }
}

function parsePayloadProtocol(raw: unknown): string | undefined {
  if (typeof raw !== 'string') return undefined;
  return raw.trim() ? raw : undefined;
}

function parseStringList(raw: unknown): string[] {
  return Array.isArray(raw) ? raw.map((item) => String(item ?? '').trim()).filter(Boolean) : [];
}

function parsePayloadHeaders(raw: unknown, idPrefix: string): PayloadHeaderEntry[] {
  const record = asRecord(raw);
  if (!record) return [];

  return Object.entries(record).map(([name, value], index) => ({
    id: `${idPrefix}-header-${index}`,
    name,
    value: String(value ?? ''),
  }));
}

function parsePayloadConditions(raw: unknown, idPrefix: string): PayloadParamEntry[] {
  if (!Array.isArray(raw)) return [];

  const entries: PayloadParamEntry[] = [];
  raw.forEach((item, itemIndex) => {
    const record = asRecord(item);
    if (!record) {
      if (typeof item === 'string') {
        entries.push({
          id: `${idPrefix}-condition-${itemIndex}-0`,
          path: item,
          valueType: 'string',
          value: '',
        });
      }
      return;
    }

    Object.entries(record).forEach(([path, value], valueIndex) => {
      const parsedValue = parsePayloadParamValue(value);
      entries.push({
        id: `${idPrefix}-condition-${itemIndex}-${valueIndex}`,
        path,
        valueType: parsedValue.valueType,
        value: parsedValue.value,
      });
    });
  });

  return entries;
}

function parsePayloadModelEntries(raw: unknown, idPrefix: string): PayloadModelEntry[] {
  if (!Array.isArray(raw)) return [];

  return raw.map((model, modelIndex) => {
    const modelRecord = asRecord(model);
    const nameRaw =
      typeof model === 'string' ? model : (modelRecord?.name ?? modelRecord?.id ?? '');
    const name = typeof nameRaw === 'string' ? nameRaw : String(nameRaw ?? '');
    const modelId = `${idPrefix}-${modelIndex}`;

    return {
      id: modelId,
      name,
      protocol: parsePayloadProtocol(modelRecord?.protocol),
      fromProtocol: parsePayloadProtocol(modelRecord?.['from-protocol']),
      headers: parsePayloadHeaders(modelRecord?.headers, modelId),
      match: parsePayloadConditions(modelRecord?.match, `${modelId}-match`),
      notMatch: parsePayloadConditions(modelRecord?.['not-match'], `${modelId}-not-match`),
      exist: parseStringList(modelRecord?.exist),
      notExist: parseStringList(modelRecord?.['not-exist']),
    };
  });
}

const PLUGIN_STORE_AUTH_TYPES = ['none', 'bearer', 'basic', 'header', 'github-token'] as const;
const PLUGIN_STORE_AUTH_APPLY_TO = ['registry', 'metadata', 'artifact'] as const;

function parsePluginStoreAuthRules(raw: unknown): PluginStoreAuthRule[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item, index): PluginStoreAuthRule | null => {
      const record = asRecord(item);
      if (!record) return null;
      const type = String(record.type ?? '')
        .trim()
        .toLowerCase();
      const rule: PluginStoreAuthRule = {
        id: `plugin-store-auth-${index}`,
        match: typeof record.match === 'string' ? record.match : '',
        applyTo: parseStringList(record['apply-to'] ?? record.apply_to).filter(
          (value): value is PluginStoreAuthRule['applyTo'][number] =>
            PLUGIN_STORE_AUTH_APPLY_TO.includes(
              value as (typeof PLUGIN_STORE_AUTH_APPLY_TO)[number]
            )
        ),
        type: PLUGIN_STORE_AUTH_TYPES.includes(type as (typeof PLUGIN_STORE_AUTH_TYPES)[number])
          ? (type as PluginStoreAuthRule['type'])
          : 'none',
        tokenEnv: typeof record['token-env'] === 'string' ? record['token-env'] : '',
        usernameEnv: typeof record['username-env'] === 'string' ? record['username-env'] : '',
        passwordEnv: typeof record['password-env'] === 'string' ? record['password-env'] : '',
        headerName: typeof record['header-name'] === 'string' ? record['header-name'] : '',
        headerValueEnv:
          typeof record['header-value-env'] === 'string' ? record['header-value-env'] : '',
        allowInsecure: Boolean(record['allow-insecure'] ?? record.allow_insecure),
      };
      return rule.match.trim() ||
        rule.type !== 'none' ||
        rule.applyTo.length > 0 ||
        rule.tokenEnv.trim() ||
        rule.usernameEnv.trim() ||
        rule.passwordEnv.trim() ||
        rule.headerName.trim() ||
        rule.headerValueEnv.trim() ||
        rule.allowInsecure
        ? rule
        : null;
    })
    .filter((rule): rule is PluginStoreAuthRule => Boolean(rule));
}

function parseRoutingStrategy(raw: unknown): VisualConfigValues['routingStrategy'] {
  const normalized = String(raw ?? '')
    .trim()
    .toLowerCase();
  if (['weighted-round-robin', 'weightedroundrobin', 'wrr'].includes(normalized)) {
    return 'weighted-round-robin';
  }
  return normalized === 'fill-first' ? 'fill-first' : 'round-robin';
}

function parseDisableImageGenerationMode(
  raw: unknown
): VisualConfigValues['disableImageGeneration'] {
  if (raw === true) return 'true';
  if (typeof raw === 'string') {
    const normalized = raw.trim().toLowerCase();
    if (normalized === 'true' || normalized === 'chat' || normalized === 'passthrough') {
      return normalized;
    }
  }
  return 'false';
}

function deleteLegacyApiKeysProvider(doc: YamlDocument): void {
  if (docHas(doc, ['auth', 'providers', 'config-api-key', 'api-key-entries'])) {
    doc.deleteIn(['auth', 'providers', 'config-api-key', 'api-key-entries']);
  }
  if (docHas(doc, ['auth', 'providers', 'config-api-key', 'api-keys'])) {
    doc.deleteIn(['auth', 'providers', 'config-api-key', 'api-keys']);
  }
  deleteIfMapEmpty(doc, ['auth', 'providers', 'config-api-key']);
  deleteIfMapEmpty(doc, ['auth', 'providers']);
  deleteIfMapEmpty(doc, ['auth']);
}

function parsePayloadRules(rules: unknown): PayloadRule[] {
  if (!Array.isArray(rules)) return [];

  return rules.map((rule, index) => {
    const record = asRecord(rule) ?? {};

    const models = parsePayloadModelEntries(record.models, `model-${index}`);

    const paramsRecord = asRecord(record.params);
    const params = paramsRecord
      ? Object.entries(paramsRecord).map(([path, value], pIndex) => {
          const parsedValue = parsePayloadParamValue(value);
          return {
            id: `param-${index}-${pIndex}`,
            path,
            valueType: parsedValue.valueType,
            value: parsedValue.value,
          };
        })
      : [];

    return { id: `payload-rule-${index}`, models, params };
  });
}

function parsePayloadFilterRules(rules: unknown): PayloadFilterRule[] {
  if (!Array.isArray(rules)) return [];

  return rules.map((rule, index) => {
    const record = asRecord(rule) ?? {};

    const models = parsePayloadModelEntries(record.models, `filter-model-${index}`);

    const paramsRaw = record.params;
    const params = Array.isArray(paramsRaw) ? paramsRaw.map(String) : [];

    return { id: `payload-filter-rule-${index}`, models, params };
  });
}

function parseRawPayloadRules(rules: unknown): PayloadRule[] {
  if (!Array.isArray(rules)) return [];

  return rules.map((rule, index) => {
    const record = asRecord(rule) ?? {};

    const models = parsePayloadModelEntries(record.models, `raw-model-${index}`);

    const paramsRecord = asRecord(record.params);
    const params = paramsRecord
      ? Object.entries(paramsRecord).map(([path, value], pIndex) => ({
          id: `raw-param-${index}-${pIndex}`,
          path,
          valueType: 'json' as const,
          value: parseRawPayloadParamValue(value),
        }))
      : [];

    return { id: `payload-raw-rule-${index}`, models, params };
  });
}

function serializePayloadParamValue(param: PayloadParamEntry): unknown {
  if (param.valueType === 'number') {
    const num = Number(param.value);
    return Number.isFinite(num) ? num : param.value;
  }
  if (param.valueType === 'boolean') return param.value === 'true';
  if (param.valueType === 'json') {
    try {
      return JSON.parse(param.value);
    } catch {
      return param.value;
    }
  }
  return param.value;
}

function serializePayloadConditions(
  conditions?: PayloadParamEntry[]
): Array<Record<string, unknown>> {
  return (conditions ?? [])
    .filter((condition) => condition.path.trim())
    .map((condition) => ({ [condition.path.trim()]: serializePayloadParamValue(condition) }));
}

function serializePayloadModelEntries(models: PayloadModelEntry[]): Array<Record<string, unknown>> {
  return (models ?? [])
    .filter((model) => model.name.trim())
    .map((model) => {
      const output: Record<string, unknown> = { name: model.name.trim() };
      if (model.protocol) output.protocol = model.protocol;
      if (model.fromProtocol) output['from-protocol'] = model.fromProtocol;
      const headers = Object.fromEntries(
        (model.headers ?? [])
          .filter((header) => header.name.trim())
          .map((header) => [header.name.trim(), header.value])
      );
      if (Object.keys(headers).length) output.headers = headers;
      const match = serializePayloadConditions(model.match);
      const notMatch = serializePayloadConditions(model.notMatch);
      if (match.length) output.match = match;
      if (notMatch.length) output['not-match'] = notMatch;
      const exist = parseStringList(model.exist);
      const notExist = parseStringList(model.notExist);
      if (exist.length) output.exist = exist;
      if (notExist.length) output['not-exist'] = notExist;
      return output;
    });
}

function serializePluginStoreAuthForYaml(
  rules: PluginStoreAuthRule[]
): Array<Record<string, unknown>> {
  return rules
    .map((rule) => {
      const match = rule.match.trim();
      if (!match) return null;
      const item: Record<string, unknown> = { match, type: rule.type };
      const applyTo = parseStringList(rule.applyTo);
      if (applyTo.length) item['apply-to'] = applyTo;
      if (rule.tokenEnv.trim()) item['token-env'] = rule.tokenEnv.trim();
      if (rule.usernameEnv.trim()) item['username-env'] = rule.usernameEnv.trim();
      if (rule.passwordEnv.trim()) item['password-env'] = rule.passwordEnv.trim();
      if (rule.headerName.trim()) item['header-name'] = rule.headerName.trim();
      if (rule.headerValueEnv.trim()) item['header-value-env'] = rule.headerValueEnv.trim();
      if (rule.allowInsecure) item['allow-insecure'] = true;
      return item;
    })
    .filter((rule): rule is Record<string, unknown> => Boolean(rule));
}

function serializePayloadRulesForYaml(rules: PayloadRule[]): Array<Record<string, unknown>> {
  return rules
    .map((rule) => {
      const models = serializePayloadModelEntries(rule.models);

      const params: Record<string, unknown> = {};
      for (const param of rule.params || []) {
        if (!param.path?.trim()) continue;
        let value: unknown = param.value;
        if (param.valueType === 'number') {
          const num = Number(param.value);
          value = Number.isFinite(num) ? num : param.value;
        } else if (param.valueType === 'boolean') {
          value = param.value === 'true';
        } else if (param.valueType === 'json') {
          try {
            value = JSON.parse(param.value);
          } catch {
            value = param.value;
          }
        }
        params[param.path.trim()] = value;
      }

      return { models, params };
    })
    .filter((rule) => rule.models.length > 0);
}

function serializePayloadFilterRulesForYaml(
  rules: PayloadFilterRule[]
): Array<Record<string, unknown>> {
  return rules
    .map((rule) => {
      const models = serializePayloadModelEntries(rule.models);

      const params = (Array.isArray(rule.params) ? rule.params : [])
        .map((path) => String(path).trim())
        .filter(Boolean);

      return { models, params };
    })
    .filter((rule) => rule.models.length > 0);
}

function serializeRawPayloadRulesForYaml(rules: PayloadRule[]): Array<Record<string, unknown>> {
  return rules
    .map((rule) => {
      const models = serializePayloadModelEntries(rule.models);

      const params: Record<string, unknown> = {};
      for (const param of rule.params || []) {
        if (!param.path?.trim()) continue;
        params[param.path.trim()] = param.value;
      }

      return { models, params };
    })
    .filter((rule) => rule.models.length > 0);
}

type VisualConfigState = {
  visualValues: VisualConfigValues;
  baselineValues: VisualConfigValues;
  dirtyFields: Set<string>;
  visualParseError: string | null;
};

type VisualConfigAction =
  | {
      type: 'load_success';
      values: VisualConfigValues;
    }
  | {
      type: 'load_error';
      error: string;
    }
  | {
      type: 'set_values';
      values: Partial<VisualConfigValues>;
    };

function createInitialVisualConfigState(): VisualConfigState {
  const initialValues = deepClone(DEFAULT_VISUAL_VALUES);
  return {
    visualValues: initialValues,
    baselineValues: deepClone(initialValues),
    dirtyFields: new Set(),
    visualParseError: null,
  };
}

function mergeVisualConfigValues(
  currentValues: VisualConfigValues,
  patch: Partial<VisualConfigValues>
): VisualConfigValues {
  const nextValues: VisualConfigValues = { ...currentValues, ...patch } as VisualConfigValues;
  if (patch.streaming) {
    nextValues.streaming = { ...currentValues.streaming, ...patch.streaming };
  }
  return nextValues;
}

function getNextDirtyFields(
  currentDirtyFields: Set<string>,
  patch: Partial<VisualConfigValues>,
  nextValues: VisualConfigValues,
  baselineValues: VisualConfigValues
): Set<string> {
  const nextDirtyFields = new Set(currentDirtyFields);
  const updateDirty = (key: string, isEqual: boolean) => {
    if (isEqual) {
      nextDirtyFields.delete(key);
    } else {
      nextDirtyFields.add(key);
    }
  };

  if (Object.prototype.hasOwnProperty.call(patch, 'host')) {
    updateDirty('host', nextValues.host === baselineValues.host);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'port')) {
    updateDirty('port', nextValues.port === baselineValues.port);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'tlsEnable')) {
    updateDirty('tlsEnable', nextValues.tlsEnable === baselineValues.tlsEnable);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'tlsCert')) {
    updateDirty('tlsCert', nextValues.tlsCert === baselineValues.tlsCert);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'tlsKey')) {
    updateDirty('tlsKey', nextValues.tlsKey === baselineValues.tlsKey);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'rmAllowRemote')) {
    updateDirty('rmAllowRemote', nextValues.rmAllowRemote === baselineValues.rmAllowRemote);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'rmSecretKey')) {
    updateDirty('rmSecretKey', nextValues.rmSecretKey === baselineValues.rmSecretKey);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'rmDisableControlPanel')) {
    updateDirty(
      'rmDisableControlPanel',
      nextValues.rmDisableControlPanel === baselineValues.rmDisableControlPanel
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'rmDisableAutoUpdatePanel')) {
    updateDirty(
      'rmDisableAutoUpdatePanel',
      nextValues.rmDisableAutoUpdatePanel === baselineValues.rmDisableAutoUpdatePanel
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'rmPanelRepo')) {
    updateDirty('rmPanelRepo', nextValues.rmPanelRepo === baselineValues.rmPanelRepo);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'authDir')) {
    updateDirty('authDir', nextValues.authDir === baselineValues.authDir);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'apiKeysText')) {
    updateDirty('apiKeysText', nextValues.apiKeysText === baselineValues.apiKeysText);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'pluginsEnabled')) {
    updateDirty('pluginsEnabled', nextValues.pluginsEnabled === baselineValues.pluginsEnabled);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'pluginStoreSources')) {
    updateDirty(
      'pluginStoreSources',
      areStringArraysEqual(nextValues.pluginStoreSources, baselineValues.pluginStoreSources)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'pluginStoreAuth')) {
    updateDirty(
      'pluginStoreAuth',
      arePluginStoreAuthRulesEqual(nextValues.pluginStoreAuth, baselineValues.pluginStoreAuth)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'debug')) {
    updateDirty('debug', nextValues.debug === baselineValues.debug);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'commercialMode')) {
    updateDirty('commercialMode', nextValues.commercialMode === baselineValues.commercialMode);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'usageStatisticsEnabled')) {
    updateDirty(
      'usageStatisticsEnabled',
      nextValues.usageStatisticsEnabled === baselineValues.usageStatisticsEnabled
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'loggingToFile')) {
    updateDirty('loggingToFile', nextValues.loggingToFile === baselineValues.loggingToFile);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'logsMaxTotalSizeMb')) {
    updateDirty(
      'logsMaxTotalSizeMb',
      nextValues.logsMaxTotalSizeMb === baselineValues.logsMaxTotalSizeMb
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'errorLogsMaxFiles')) {
    updateDirty(
      'errorLogsMaxFiles',
      nextValues.errorLogsMaxFiles === baselineValues.errorLogsMaxFiles
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'redisUsageQueueRetentionSeconds')) {
    updateDirty(
      'redisUsageQueueRetentionSeconds',
      nextValues.redisUsageQueueRetentionSeconds === baselineValues.redisUsageQueueRetentionSeconds
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'proxyUrl')) {
    updateDirty('proxyUrl', nextValues.proxyUrl === baselineValues.proxyUrl);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'forceModelPrefix')) {
    updateDirty(
      'forceModelPrefix',
      nextValues.forceModelPrefix === baselineValues.forceModelPrefix
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'requestRetry')) {
    updateDirty('requestRetry', nextValues.requestRetry === baselineValues.requestRetry);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'maxRetryCredentials')) {
    updateDirty(
      'maxRetryCredentials',
      nextValues.maxRetryCredentials === baselineValues.maxRetryCredentials
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'maxRetryInterval')) {
    updateDirty(
      'maxRetryInterval',
      nextValues.maxRetryInterval === baselineValues.maxRetryInterval
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'authAutoRefreshWorkers')) {
    updateDirty(
      'authAutoRefreshWorkers',
      nextValues.authAutoRefreshWorkers === baselineValues.authAutoRefreshWorkers
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'passthroughHeaders')) {
    updateDirty(
      'passthroughHeaders',
      nextValues.passthroughHeaders === baselineValues.passthroughHeaders
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'disableCooling')) {
    updateDirty('disableCooling', nextValues.disableCooling === baselineValues.disableCooling);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'disableImageGeneration')) {
    updateDirty(
      'disableImageGeneration',
      nextValues.disableImageGeneration === baselineValues.disableImageGeneration
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'gptImage2BaseModel')) {
    updateDirty(
      'gptImage2BaseModel',
      nextValues.gptImage2BaseModel === baselineValues.gptImage2BaseModel
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'wsAuth')) {
    updateDirty('wsAuth', nextValues.wsAuth === baselineValues.wsAuth);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'antigravitySensitiveWords')) {
    updateDirty(
      'antigravitySensitiveWords',
      areStringArraysEqual(
        nextValues.antigravitySensitiveWords,
        baselineValues.antigravitySensitiveWords
      )
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'devinSensitiveWords')) {
    updateDirty(
      'devinSensitiveWords',
      areStringArraysEqual(nextValues.devinSensitiveWords, baselineValues.devinSensitiveWords)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'antigravitySignatureCacheEnabled')) {
    updateDirty(
      'antigravitySignatureCacheEnabled',
      nextValues.antigravitySignatureCacheEnabled ===
        baselineValues.antigravitySignatureCacheEnabled
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'antigravitySignatureBypassStrict')) {
    updateDirty(
      'antigravitySignatureBypassStrict',
      nextValues.antigravitySignatureBypassStrict ===
        baselineValues.antigravitySignatureBypassStrict
    );
  }
  for (const key of [
    'claudeHeaderUserAgent',
    'claudeHeaderPackageVersion',
    'claudeHeaderRuntimeVersion',
    'claudeHeaderOs',
    'claudeHeaderArch',
    'claudeHeaderTimeout',
    'codexHeaderUserAgent',
    'codexHeaderBetaFeatures',
  ] as const) {
    if (Object.prototype.hasOwnProperty.call(patch, key)) {
      updateDirty(key, nextValues[key] === baselineValues[key]);
    }
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'claudeHeaderStabilizeDeviceProfile')) {
    updateDirty(
      'claudeHeaderStabilizeDeviceProfile',
      nextValues.claudeHeaderStabilizeDeviceProfile ===
        baselineValues.claudeHeaderStabilizeDeviceProfile
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'quotaSwitchProject')) {
    updateDirty(
      'quotaSwitchProject',
      nextValues.quotaSwitchProject === baselineValues.quotaSwitchProject
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'quotaSwitchPreviewModel')) {
    updateDirty(
      'quotaSwitchPreviewModel',
      nextValues.quotaSwitchPreviewModel === baselineValues.quotaSwitchPreviewModel
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'quotaAntigravityCredits')) {
    updateDirty(
      'quotaAntigravityCredits',
      nextValues.quotaAntigravityCredits === baselineValues.quotaAntigravityCredits
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'routingStrategy')) {
    updateDirty('routingStrategy', nextValues.routingStrategy === baselineValues.routingStrategy);
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'routingSessionAffinity')) {
    updateDirty(
      'routingSessionAffinity',
      nextValues.routingSessionAffinity === baselineValues.routingSessionAffinity
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'routingSessionAffinityTTL')) {
    updateDirty(
      'routingSessionAffinityTTL',
      nextValues.routingSessionAffinityTTL === baselineValues.routingSessionAffinityTTL
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'payloadDefaultRules')) {
    updateDirty(
      'payloadDefaultRules',
      arePayloadRulesEqual(nextValues.payloadDefaultRules, baselineValues.payloadDefaultRules)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'payloadDefaultRawRules')) {
    updateDirty(
      'payloadDefaultRawRules',
      arePayloadRulesEqual(nextValues.payloadDefaultRawRules, baselineValues.payloadDefaultRawRules)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'payloadOverrideRules')) {
    updateDirty(
      'payloadOverrideRules',
      arePayloadRulesEqual(nextValues.payloadOverrideRules, baselineValues.payloadOverrideRules)
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'payloadOverrideRawRules')) {
    updateDirty(
      'payloadOverrideRawRules',
      arePayloadRulesEqual(
        nextValues.payloadOverrideRawRules,
        baselineValues.payloadOverrideRawRules
      )
    );
  }
  if (Object.prototype.hasOwnProperty.call(patch, 'payloadFilterRules')) {
    updateDirty(
      'payloadFilterRules',
      arePayloadFilterRulesEqual(nextValues.payloadFilterRules, baselineValues.payloadFilterRules)
    );
  }
  if (patch.streaming) {
    const streamingPatch = patch.streaming;
    if (Object.prototype.hasOwnProperty.call(streamingPatch, 'keepaliveSeconds')) {
      updateDirty(
        'streaming.keepaliveSeconds',
        nextValues.streaming.keepaliveSeconds === baselineValues.streaming.keepaliveSeconds
      );
    }
    if (Object.prototype.hasOwnProperty.call(streamingPatch, 'bootstrapRetries')) {
      updateDirty(
        'streaming.bootstrapRetries',
        nextValues.streaming.bootstrapRetries === baselineValues.streaming.bootstrapRetries
      );
    }
    if (Object.prototype.hasOwnProperty.call(streamingPatch, 'nonstreamKeepaliveInterval')) {
      updateDirty(
        'streaming.nonstreamKeepaliveInterval',
        nextValues.streaming.nonstreamKeepaliveInterval ===
          baselineValues.streaming.nonstreamKeepaliveInterval
      );
    }
  }

  return nextDirtyFields;
}

function visualConfigReducer(
  state: VisualConfigState,
  action: VisualConfigAction
): VisualConfigState {
  switch (action.type) {
    case 'load_success':
      return {
        visualValues: action.values,
        baselineValues: deepClone(action.values),
        dirtyFields: new Set(),
        visualParseError: null,
      };
    case 'load_error':
      return {
        ...state,
        visualParseError: action.error,
      };
    case 'set_values': {
      const nextValues = mergeVisualConfigValues(state.visualValues, action.values);
      const nextDirtyFields = getNextDirtyFields(
        state.dirtyFields,
        action.values,
        nextValues,
        state.baselineValues
      );

      return {
        ...state,
        visualValues: nextValues,
        dirtyFields: nextDirtyFields,
      };
    }
    default:
      return state;
  }
}

export function useVisualConfig() {
  const [state, dispatch] = useReducer(
    visualConfigReducer,
    undefined,
    createInitialVisualConfigState
  );
  const { visualValues, visualParseError, dirtyFields } = state;
  const visualDirty = dirtyFields.size > 0;
  const visualValidationErrors = useMemo(
    () => getVisualConfigValidationErrors(visualValues),
    [visualValues]
  );
  const visualHasPayloadValidationErrors = useMemo(
    () =>
      hasPayloadParamValidationErrors(visualValues.payloadDefaultRules) ||
      hasPayloadParamValidationErrors(visualValues.payloadDefaultRawRules) ||
      hasPayloadParamValidationErrors(visualValues.payloadOverrideRules) ||
      hasPayloadParamValidationErrors(visualValues.payloadOverrideRawRules),
    [
      visualValues.payloadDefaultRules,
      visualValues.payloadDefaultRawRules,
      visualValues.payloadOverrideRules,
      visualValues.payloadOverrideRawRules,
    ]
  );

  const loadVisualValuesFromYaml = useCallback((yamlContent: string) => {
    try {
      const document = parseDocument(yamlContent);
      if (document.errors.length > 0) {
        throw new Error(document.errors[0]?.message ?? 'Invalid YAML');
      }

      const parsedRaw: unknown = parseYaml(yamlContent) || {};
      const parsed = asRecord(parsedRaw) ?? {};
      const tls = asRecord(parsed.tls);
      const remoteManagement = asRecord(parsed['remote-management']);
      const plugins = asRecord(parsed.plugins);
      const quotaExceeded = asRecord(parsed['quota-exceeded']);
      const routing = asRecord(parsed.routing);
      const payload = asRecord(parsed.payload);
      const streaming = asRecord(parsed.streaming);
      const antigravity = asRecord(parsed.antigravity);
      const devin = asRecord(parsed.devin);
      const claudeHeaders = asRecord(parsed['claude-header-defaults']);
      const codexHeaders = asRecord(parsed['codex-header-defaults']);

      const newValues: VisualConfigValues = {
        host: typeof parsed.host === 'string' ? parsed.host : '',
        port: String(parsed.port ?? ''),

        tlsEnable: Boolean(tls?.enable),
        tlsCert: typeof tls?.cert === 'string' ? tls.cert : '',
        tlsKey: typeof tls?.key === 'string' ? tls.key : '',

        rmAllowRemote: Boolean(remoteManagement?.['allow-remote']),
        rmSecretKey:
          typeof remoteManagement?.['secret-key'] === 'string'
            ? remoteManagement['secret-key']
            : '',
        rmDisableControlPanel: Boolean(remoteManagement?.['disable-control-panel']),
        rmDisableAutoUpdatePanel: Boolean(remoteManagement?.['disable-auto-update-panel']),
        rmPanelRepo:
          typeof remoteManagement?.['panel-github-repository'] === 'string'
            ? remoteManagement['panel-github-repository']
            : typeof remoteManagement?.['panel-repo'] === 'string'
              ? remoteManagement['panel-repo']
              : '',

        authDir: typeof parsed['auth-dir'] === 'string' ? parsed['auth-dir'] : '',
        apiKeysText: resolveApiKeysText(parsed),
        pluginsEnabled: Boolean(plugins?.enabled),
        pluginStoreSources: parseStringList(plugins?.['store-sources']),
        pluginStoreAuth: parsePluginStoreAuthRules(plugins?.['store-auth']),

        debug: Boolean(parsed.debug),
        commercialMode: Boolean(parsed['commercial-mode']),
        usageStatisticsEnabled: Boolean(
          parsed['usage-statistics-enabled'] ?? parsed.usageStatisticsEnabled
        ),
        loggingToFile: Boolean(parsed['logging-to-file']),
        logsMaxTotalSizeMb: String(parsed['logs-max-total-size-mb'] ?? ''),
        errorLogsMaxFiles: String(parsed['error-logs-max-files'] ?? ''),
        redisUsageQueueRetentionSeconds: String(
          parsed['redis-usage-queue-retention-seconds'] ??
            parsed.redisUsageQueueRetentionSeconds ??
            ''
        ),

        proxyUrl: typeof parsed['proxy-url'] === 'string' ? parsed['proxy-url'] : '',
        forceModelPrefix: Boolean(parsed['force-model-prefix']),
        requestRetry: String(parsed['request-retry'] ?? ''),
        maxRetryCredentials: String(parsed['max-retry-credentials'] ?? ''),
        maxRetryInterval: String(parsed['max-retry-interval'] ?? ''),
        authAutoRefreshWorkers: String(parsed['auth-auto-refresh-workers'] ?? ''),
        passthroughHeaders: Boolean(parsed['passthrough-headers']),
        disableCooling: Boolean(parsed['disable-cooling']),
        disableImageGeneration: parseDisableImageGenerationMode(parsed['disable-image-generation']),
        gptImage2BaseModel:
          typeof parsed['gpt-image-2-base-model'] === 'string'
            ? parsed['gpt-image-2-base-model']
            : '',
        wsAuth: Boolean(parsed['ws-auth']),

        quotaSwitchProject: Boolean(quotaExceeded?.['switch-project'] ?? true),
        quotaSwitchPreviewModel: Boolean(quotaExceeded?.['switch-preview-model'] ?? true),
        quotaAntigravityCredits: Boolean(quotaExceeded?.['antigravity-credits'] ?? false),

        routingStrategy: parseRoutingStrategy(routing?.strategy),
        routingSessionAffinity: Boolean(
          routing?.['session-affinity'] ?? routing?.sessionAffinity ?? routing?.['sessionAffinity']
        ),
        routingSessionAffinityTTL:
          typeof routing?.['session-affinity-ttl'] === 'string'
            ? routing['session-affinity-ttl']
            : typeof routing?.sessionAffinityTTL === 'string'
              ? routing.sessionAffinityTTL
              : typeof routing?.['sessionAffinityTTL'] === 'string'
                ? routing['sessionAffinityTTL']
                : '',

        payloadDefaultRules: parsePayloadRules(payload?.default),
        payloadDefaultRawRules: parseRawPayloadRules(payload?.['default-raw']),
        payloadOverrideRules: parsePayloadRules(payload?.override),
        payloadOverrideRawRules: parseRawPayloadRules(payload?.['override-raw']),
        payloadFilterRules: parsePayloadFilterRules(payload?.filter),
        antigravitySensitiveWords: parseStringList(antigravity?.['sensitive-words']),
        devinSensitiveWords: parseStringList(devin?.['sensitive-words']),
        antigravitySignatureCacheEnabled: Boolean(
          parsed['antigravity-signature-cache-enabled'] ?? true
        ),
        antigravitySignatureBypassStrict: Boolean(parsed['antigravity-signature-bypass-strict']),
        claudeHeaderUserAgent:
          typeof claudeHeaders?.['user-agent'] === 'string' ? claudeHeaders['user-agent'] : '',
        claudeHeaderPackageVersion:
          typeof claudeHeaders?.['package-version'] === 'string'
            ? claudeHeaders['package-version']
            : '',
        claudeHeaderRuntimeVersion:
          typeof claudeHeaders?.['runtime-version'] === 'string'
            ? claudeHeaders['runtime-version']
            : '',
        claudeHeaderOs: typeof claudeHeaders?.os === 'string' ? claudeHeaders.os : '',
        claudeHeaderArch: typeof claudeHeaders?.arch === 'string' ? claudeHeaders.arch : '',
        claudeHeaderTimeout: String(claudeHeaders?.timeout ?? ''),
        claudeHeaderStabilizeDeviceProfile: Boolean(claudeHeaders?.['stabilize-device-profile']),
        codexHeaderUserAgent:
          typeof codexHeaders?.['user-agent'] === 'string' ? codexHeaders['user-agent'] : '',
        codexHeaderBetaFeatures:
          typeof codexHeaders?.['beta-features'] === 'string' ? codexHeaders['beta-features'] : '',

        streaming: {
          keepaliveSeconds: String(streaming?.['keepalive-seconds'] ?? ''),
          bootstrapRetries: String(streaming?.['bootstrap-retries'] ?? ''),
          nonstreamKeepaliveInterval: String(parsed['nonstream-keepalive-interval'] ?? ''),
        },
      };

      dispatch({ type: 'load_success', values: newValues });
      return { ok: true as const };
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Invalid YAML';
      dispatch({ type: 'load_error', error: message });
      return { ok: false as const, error: message };
    }
  }, []);

  const applyVisualChangesToYaml = useCallback(
    (currentYaml: string): string => {
      try {
        const doc = parseDocument(currentYaml);
        if (doc.errors.length > 0) return currentYaml;
        if (!isMap(doc.contents)) {
          doc.contents = doc.createNode({}) as unknown as typeof doc.contents;
        }
        const values = visualValues;

        setStringInDoc(doc, ['host'], values.host);
        setIntFromStringInDoc(doc, ['port'], values.port);

        if (
          docHas(doc, ['tls']) ||
          values.tlsEnable ||
          values.tlsCert.trim() ||
          values.tlsKey.trim()
        ) {
          ensureMapInDoc(doc, ['tls']);
          setBooleanInDoc(doc, ['tls', 'enable'], values.tlsEnable);
          setStringInDoc(doc, ['tls', 'cert'], values.tlsCert);
          setStringInDoc(doc, ['tls', 'key'], values.tlsKey);
          deleteIfMapEmpty(doc, ['tls']);
        }

        if (
          docHas(doc, ['remote-management']) ||
          values.rmAllowRemote ||
          values.rmSecretKey.trim() ||
          values.rmDisableControlPanel ||
          values.rmDisableAutoUpdatePanel ||
          values.rmPanelRepo.trim() ||
          dirtyFields.has('rmDisableAutoUpdatePanel')
        ) {
          ensureMapInDoc(doc, ['remote-management']);
          setBooleanInDoc(doc, ['remote-management', 'allow-remote'], values.rmAllowRemote);
          setStringInDoc(doc, ['remote-management', 'secret-key'], values.rmSecretKey);
          setBooleanInDoc(
            doc,
            ['remote-management', 'disable-control-panel'],
            values.rmDisableControlPanel
          );
          setManagedBooleanInDoc(
            doc,
            ['remote-management', 'disable-auto-update-panel'],
            values.rmDisableAutoUpdatePanel,
            dirtyFields,
            'rmDisableAutoUpdatePanel'
          );
          setStringInDoc(doc, ['remote-management', 'panel-github-repository'], values.rmPanelRepo);
          if (docHas(doc, ['remote-management', 'panel-repo'])) {
            doc.deleteIn(['remote-management', 'panel-repo']);
          }
          deleteIfMapEmpty(doc, ['remote-management']);
        }

        setStringInDoc(doc, ['auth-dir'], values.authDir);
        const apiKeys = values.apiKeysText
          .split('\n')
          .map((key) => key.trim())
          .filter(Boolean);
        if (apiKeys.length > 0) {
          doc.setIn(['api-keys'], apiKeys);
        } else if (docHas(doc, ['api-keys'])) {
          doc.deleteIn(['api-keys']);
        }
        deleteLegacyApiKeysProvider(doc);

        if (
          docHas(doc, ['plugins']) ||
          values.pluginsEnabled ||
          values.pluginStoreSources.length > 0 ||
          values.pluginStoreAuth.length > 0 ||
          dirtyFields.has('pluginsEnabled') ||
          dirtyFields.has('pluginStoreSources') ||
          dirtyFields.has('pluginStoreAuth')
        ) {
          ensureMapInDoc(doc, ['plugins']);
          setManagedBooleanInDoc(
            doc,
            ['plugins', 'enabled'],
            values.pluginsEnabled,
            dirtyFields,
            'pluginsEnabled'
          );
          if (dirtyFields.has('pluginStoreSources')) {
            if (values.pluginStoreSources.length) {
              doc.setIn(['plugins', 'store-sources'], values.pluginStoreSources);
            } else if (docHas(doc, ['plugins', 'store-sources'])) {
              doc.deleteIn(['plugins', 'store-sources']);
            }
          }
          if (dirtyFields.has('pluginStoreAuth')) {
            const storeAuth = serializePluginStoreAuthForYaml(values.pluginStoreAuth);
            if (storeAuth.length) doc.setIn(['plugins', 'store-auth'], storeAuth);
            else if (docHas(doc, ['plugins', 'store-auth']))
              doc.deleteIn(['plugins', 'store-auth']);
          }
          deleteIfMapEmpty(doc, ['plugins']);
        }

        setBooleanInDoc(doc, ['debug'], values.debug);

        setBooleanInDoc(doc, ['commercial-mode'], values.commercialMode);
        setBooleanInDoc(doc, ['usage-statistics-enabled'], values.usageStatisticsEnabled);
        setBooleanInDoc(doc, ['logging-to-file'], values.loggingToFile);
        setIntFromStringInDoc(doc, ['logs-max-total-size-mb'], values.logsMaxTotalSizeMb);
        if (
          shouldWriteManagedField(doc, ['error-logs-max-files'], dirtyFields, 'errorLogsMaxFiles')
        ) {
          setIntFromStringInDoc(doc, ['error-logs-max-files'], values.errorLogsMaxFiles);
        }
        if (
          shouldWriteManagedField(
            doc,
            ['redis-usage-queue-retention-seconds'],
            dirtyFields,
            'redisUsageQueueRetentionSeconds'
          )
        ) {
          setIntFromStringInDoc(
            doc,
            ['redis-usage-queue-retention-seconds'],
            values.redisUsageQueueRetentionSeconds
          );
        }

        setStringInDoc(doc, ['proxy-url'], values.proxyUrl);
        setBooleanInDoc(doc, ['force-model-prefix'], values.forceModelPrefix);
        setIntFromStringInDoc(doc, ['request-retry'], values.requestRetry);
        setIntFromStringInDoc(doc, ['max-retry-credentials'], values.maxRetryCredentials);
        setIntFromStringInDoc(doc, ['max-retry-interval'], values.maxRetryInterval);
        if (
          shouldWriteManagedField(
            doc,
            ['auth-auto-refresh-workers'],
            dirtyFields,
            'authAutoRefreshWorkers'
          )
        ) {
          setIntFromStringInDoc(doc, ['auth-auto-refresh-workers'], values.authAutoRefreshWorkers);
        }
        setManagedBooleanInDoc(
          doc,
          ['passthrough-headers'],
          values.passthroughHeaders,
          dirtyFields,
          'passthroughHeaders'
        );
        setManagedBooleanInDoc(
          doc,
          ['disable-cooling'],
          values.disableCooling,
          dirtyFields,
          'disableCooling'
        );
        if (
          shouldWriteManagedField(
            doc,
            ['disable-image-generation'],
            dirtyFields,
            'disableImageGeneration'
          )
        ) {
          setDisableImageGenerationInDoc(
            doc,
            ['disable-image-generation'],
            values.disableImageGeneration
          );
        }
        if (
          shouldWriteManagedField(
            doc,
            ['gpt-image-2-base-model'],
            dirtyFields,
            'gptImage2BaseModel'
          )
        ) {
          setStringInDoc(doc, ['gpt-image-2-base-model'], values.gptImage2BaseModel);
        }
        setBooleanInDoc(doc, ['ws-auth'], values.wsAuth);

        if (
          docHas(doc, ['antigravity']) ||
          values.antigravitySensitiveWords.length > 0 ||
          dirtyFields.has('antigravitySensitiveWords')
        ) {
          ensureMapInDoc(doc, ['antigravity']);
          if (dirtyFields.has('antigravitySensitiveWords')) {
            if (values.antigravitySensitiveWords.length) {
              doc.setIn(['antigravity', 'sensitive-words'], values.antigravitySensitiveWords);
            } else if (docHas(doc, ['antigravity', 'sensitive-words'])) {
              doc.deleteIn(['antigravity', 'sensitive-words']);
            }
          }
          deleteIfMapEmpty(doc, ['antigravity']);
        }
        if (
          docHas(doc, ['devin']) ||
          values.devinSensitiveWords.length > 0 ||
          dirtyFields.has('devinSensitiveWords')
        ) {
          ensureMapInDoc(doc, ['devin']);
          if (dirtyFields.has('devinSensitiveWords')) {
            if (values.devinSensitiveWords.length) {
              doc.setIn(['devin', 'sensitive-words'], values.devinSensitiveWords);
            } else if (docHas(doc, ['devin', 'sensitive-words'])) {
              doc.deleteIn(['devin', 'sensitive-words']);
            }
          }
          deleteIfMapEmpty(doc, ['devin']);
        }
        setManagedBooleanInDoc(
          doc,
          ['antigravity-signature-cache-enabled'],
          values.antigravitySignatureCacheEnabled,
          dirtyFields,
          'antigravitySignatureCacheEnabled'
        );
        setManagedBooleanInDoc(
          doc,
          ['antigravity-signature-bypass-strict'],
          values.antigravitySignatureBypassStrict,
          dirtyFields,
          'antigravitySignatureBypassStrict'
        );

        const claudeHeaderValues: Array<[keyof VisualConfigValues, string[]]> = [
          ['claudeHeaderUserAgent', ['claude-header-defaults', 'user-agent']],
          ['claudeHeaderPackageVersion', ['claude-header-defaults', 'package-version']],
          ['claudeHeaderRuntimeVersion', ['claude-header-defaults', 'runtime-version']],
          ['claudeHeaderOs', ['claude-header-defaults', 'os']],
          ['claudeHeaderArch', ['claude-header-defaults', 'arch']],
          ['claudeHeaderTimeout', ['claude-header-defaults', 'timeout']],
        ];
        const claudeHeaderTouched =
          claudeHeaderValues.some(([key, path]) =>
            shouldWriteManagedField(doc, path, dirtyFields, key)
          ) ||
          shouldWriteManagedField(
            doc,
            ['claude-header-defaults', 'stabilize-device-profile'],
            dirtyFields,
            'claudeHeaderStabilizeDeviceProfile'
          );
        if (docHas(doc, ['claude-header-defaults']) || claudeHeaderTouched) {
          ensureMapInDoc(doc, ['claude-header-defaults']);
          for (const [key, path] of claudeHeaderValues) {
            if (!shouldWriteManagedField(doc, path, dirtyFields, key)) continue;
            const value = values[key];
            if (key === 'claudeHeaderTimeout') setIntFromStringInDoc(doc, path, value);
            else setStringInDoc(doc, path, value);
          }
          setManagedBooleanInDoc(
            doc,
            ['claude-header-defaults', 'stabilize-device-profile'],
            values.claudeHeaderStabilizeDeviceProfile,
            dirtyFields,
            'claudeHeaderStabilizeDeviceProfile'
          );
          deleteIfMapEmpty(doc, ['claude-header-defaults']);
        }
        const codexHeaderValues: Array<[keyof VisualConfigValues, string[]]> = [
          ['codexHeaderUserAgent', ['codex-header-defaults', 'user-agent']],
          ['codexHeaderBetaFeatures', ['codex-header-defaults', 'beta-features']],
        ];
        if (
          docHas(doc, ['codex-header-defaults']) ||
          codexHeaderValues.some(([key, path]) =>
            shouldWriteManagedField(doc, path, dirtyFields, key)
          )
        ) {
          ensureMapInDoc(doc, ['codex-header-defaults']);
          for (const [key, path] of codexHeaderValues) {
            if (shouldWriteManagedField(doc, path, dirtyFields, key)) {
              setStringInDoc(doc, path, values[key]);
            }
          }
          deleteIfMapEmpty(doc, ['codex-header-defaults']);
        }

        if (
          docHas(doc, ['quota-exceeded']) ||
          !values.quotaSwitchProject ||
          !values.quotaSwitchPreviewModel ||
          shouldWriteManagedField(
            doc,
            ['quota-exceeded', 'antigravity-credits'],
            dirtyFields,
            'quotaAntigravityCredits'
          )
        ) {
          ensureMapInDoc(doc, ['quota-exceeded']);
          const writeQuotaAntigravityCredits = shouldWriteManagedField(
            doc,
            ['quota-exceeded', 'antigravity-credits'],
            dirtyFields,
            'quotaAntigravityCredits'
          );
          doc.setIn(['quota-exceeded', 'switch-project'], values.quotaSwitchProject);
          doc.setIn(['quota-exceeded', 'switch-preview-model'], values.quotaSwitchPreviewModel);
          if (writeQuotaAntigravityCredits) {
            doc.setIn(['quota-exceeded', 'antigravity-credits'], values.quotaAntigravityCredits);
          }
          deleteIfMapEmpty(doc, ['quota-exceeded']);
        }

        if (
          docHas(doc, ['routing']) ||
          values.routingStrategy !== 'round-robin' ||
          values.routingSessionAffinity ||
          values.routingSessionAffinityTTL.trim()
        ) {
          ensureMapInDoc(doc, ['routing']);
          doc.setIn(['routing', 'strategy'], values.routingStrategy);
          setBooleanInDoc(doc, ['routing', 'session-affinity'], values.routingSessionAffinity);
          setStringInDoc(
            doc,
            ['routing', 'session-affinity-ttl'],
            values.routingSessionAffinityTTL
          );
          deleteIfMapEmpty(doc, ['routing']);
        }

        const keepaliveSeconds =
          typeof values.streaming?.keepaliveSeconds === 'string'
            ? values.streaming.keepaliveSeconds
            : '';
        const bootstrapRetries =
          typeof values.streaming?.bootstrapRetries === 'string'
            ? values.streaming.bootstrapRetries
            : '';
        const nonstreamKeepaliveInterval =
          typeof values.streaming?.nonstreamKeepaliveInterval === 'string'
            ? values.streaming.nonstreamKeepaliveInterval
            : '';

        const streamingDefined =
          docHas(doc, ['streaming']) || keepaliveSeconds.trim() || bootstrapRetries.trim();
        if (streamingDefined) {
          ensureMapInDoc(doc, ['streaming']);
          setIntFromStringInDoc(doc, ['streaming', 'keepalive-seconds'], keepaliveSeconds);
          setIntFromStringInDoc(doc, ['streaming', 'bootstrap-retries'], bootstrapRetries);
          deleteIfMapEmpty(doc, ['streaming']);
        }

        setIntFromStringInDoc(doc, ['nonstream-keepalive-interval'], nonstreamKeepaliveInterval);

        if (
          docHas(doc, ['payload']) ||
          values.payloadDefaultRules.length > 0 ||
          values.payloadDefaultRawRules.length > 0 ||
          values.payloadOverrideRules.length > 0 ||
          values.payloadOverrideRawRules.length > 0 ||
          values.payloadFilterRules.length > 0
        ) {
          ensureMapInDoc(doc, ['payload']);
          if (values.payloadDefaultRules.length > 0) {
            doc.setIn(
              ['payload', 'default'],
              serializePayloadRulesForYaml(values.payloadDefaultRules)
            );
          } else if (docHas(doc, ['payload', 'default'])) {
            doc.deleteIn(['payload', 'default']);
          }
          if (values.payloadDefaultRawRules.length > 0) {
            doc.setIn(
              ['payload', 'default-raw'],
              serializeRawPayloadRulesForYaml(values.payloadDefaultRawRules)
            );
          } else if (docHas(doc, ['payload', 'default-raw'])) {
            doc.deleteIn(['payload', 'default-raw']);
          }
          if (values.payloadOverrideRules.length > 0) {
            doc.setIn(
              ['payload', 'override'],
              serializePayloadRulesForYaml(values.payloadOverrideRules)
            );
          } else if (docHas(doc, ['payload', 'override'])) {
            doc.deleteIn(['payload', 'override']);
          }
          if (values.payloadOverrideRawRules.length > 0) {
            doc.setIn(
              ['payload', 'override-raw'],
              serializeRawPayloadRulesForYaml(values.payloadOverrideRawRules)
            );
          } else if (docHas(doc, ['payload', 'override-raw'])) {
            doc.deleteIn(['payload', 'override-raw']);
          }
          if (values.payloadFilterRules.length > 0) {
            doc.setIn(
              ['payload', 'filter'],
              serializePayloadFilterRulesForYaml(values.payloadFilterRules)
            );
          } else if (docHas(doc, ['payload', 'filter'])) {
            doc.deleteIn(['payload', 'filter']);
          }
          deleteIfMapEmpty(doc, ['payload']);
        }

        return doc.toString({ indent: 2, lineWidth: 120, minContentWidth: 0 });
      } catch {
        return currentYaml;
      }
    },
    [dirtyFields, visualValues]
  );

  const setVisualValues = useCallback((newValues: Partial<VisualConfigValues>) => {
    dispatch({ type: 'set_values', values: newValues });
  }, []);

  return {
    visualValues,
    visualDirty,
    visualParseError,
    visualValidationErrors,
    visualHasPayloadValidationErrors,
    loadVisualValuesFromYaml,
    applyVisualChangesToYaml,
    setVisualValues,
  };
}

export const VISUAL_CONFIG_PROTOCOL_OPTIONS = [
  {
    value: '',
    labelKey: 'config_management.visual.payload_rules.provider_default',
    defaultLabel: 'Default',
  },
  {
    value: 'openai',
    labelKey: 'config_management.visual.payload_rules.provider_openai',
    defaultLabel: 'OpenAI',
  },
  {
    value: 'openai-response',
    labelKey: 'config_management.visual.payload_rules.provider_openai_response',
    defaultLabel: 'OpenAI Response',
  },
  {
    value: 'gemini',
    labelKey: 'config_management.visual.payload_rules.provider_gemini',
    defaultLabel: 'Gemini',
  },
  {
    value: 'claude',
    labelKey: 'config_management.visual.payload_rules.provider_claude',
    defaultLabel: 'Claude',
  },
  {
    value: 'codex',
    labelKey: 'config_management.visual.payload_rules.provider_codex',
    defaultLabel: 'Codex',
  },
  {
    value: 'antigravity',
    labelKey: 'config_management.visual.payload_rules.provider_antigravity',
    defaultLabel: 'Antigravity',
  },
] as const;

export const VISUAL_CONFIG_PAYLOAD_VALUE_TYPE_OPTIONS = [
  {
    value: 'string',
    labelKey: 'config_management.visual.payload_rules.value_type_string',
    defaultLabel: 'String',
  },
  {
    value: 'number',
    labelKey: 'config_management.visual.payload_rules.value_type_number',
    defaultLabel: 'Number',
  },
  {
    value: 'boolean',
    labelKey: 'config_management.visual.payload_rules.value_type_boolean',
    defaultLabel: 'Boolean',
  },
  {
    value: 'json',
    labelKey: 'config_management.visual.payload_rules.value_type_json',
    defaultLabel: 'JSON',
  },
] as const satisfies ReadonlyArray<{
  value: PayloadParamValueType;
  labelKey: string;
  defaultLabel: string;
}>;
