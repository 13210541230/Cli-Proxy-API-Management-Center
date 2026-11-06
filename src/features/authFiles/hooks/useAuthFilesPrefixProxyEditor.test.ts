import { describe, expect, it } from 'vitest';
import {
  readAuthFileUseSiwc,
  supportsAuthFileSiwc,
} from '@/features/authFiles/constants';
import {
  buildAuthFileFieldsPatch,
  type PrefixProxyEditorState,
} from './useAuthFilesPrefixProxyEditor';

const makeEditor = (
  json: Record<string, unknown>,
  overrides: Partial<PrefixProxyEditorState> = {}
): PrefixProxyEditorState => ({
  fileName: 'codex.json',
  fileInfoText: '',
  loading: false,
  saving: false,
  error: null,
  originalText: '',
  rawText: '',
  json,
  prefix: '',
  proxyUrl: '',
  baseUrl: '',
  priority: '',
  note: '',
  noteTouched: false,
  headersText: '',
  headersTouched: false,
  headersError: null,
  providerKey: 'codex',
  useSiwc: false,
  useSiwcTouched: false,
  ...overrides,
});

const resolveError = (key: string) => key;

describe('Codex SIWC auth-file setting', () => {
  it('is Codex-only and reads stored boolean-compatible values', () => {
    expect(supportsAuthFileSiwc('codex')).toBe(true);
    expect(supportsAuthFileSiwc(' Claude ')).toBe(false);
    expect(readAuthFileUseSiwc({})).toBe(false);
    expect(readAuthFileUseSiwc({ use_siwc: false })).toBe(false);
    expect(readAuthFileUseSiwc({ use_siwc: true })).toBe(true);
    expect(readAuthFileUseSiwc({ use_siwc: 'true' })).toBe(true);
  });

  it('disables SIWC without changing its binding data or other credentials', () => {
    const json = { use_siwc: true, siwc: { session: 'bound' }, api_key: 'keep' };
    const original = JSON.stringify(json);
    const editor = makeEditor(json, { useSiwcTouched: true });

    expect(buildAuthFileFieldsPatch(editor, resolveError)).toEqual({ use_siwc: false });
    expect(JSON.stringify(json)).toBe(original);
  });

  it('only patches changed SIWC state on Codex auth files', () => {
    const enabled = makeEditor({}, { useSiwc: true, useSiwcTouched: true });
    expect(buildAuthFileFieldsPatch(enabled, resolveError)).toEqual({ use_siwc: true });
    expect(buildAuthFileFieldsPatch(makeEditor({}, { useSiwc: true }), resolveError)).toEqual({});
    expect(
      buildAuthFileFieldsPatch(
        makeEditor({}, { providerKey: 'claude', useSiwc: true, useSiwcTouched: true }),
        resolveError
      )
    ).toEqual({});
    expect(
      buildAuthFileFieldsPatch(
        makeEditor({ use_siwc: true }, { useSiwc: true, useSiwcTouched: true }),
        resolveError
      )
    ).toEqual({});
  });
});
