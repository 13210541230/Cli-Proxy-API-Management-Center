import { act, createRef, forwardRef, useImperativeHandle } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { parse } from 'yaml';
import { afterEach, describe, expect, it } from 'vitest';
import { useVisualConfig } from './useVisualConfig';

type HookValue = ReturnType<typeof useVisualConfig>;

let renderer: ReactTestRenderer | undefined;

const HookHarness = forwardRef<HookValue>(function HookHarness(_, ref) {
  const value = useVisualConfig();
  useImperativeHandle(ref, () => value, [value]);
  return null;
});

afterEach(() => {
  act(() => renderer?.unmount());
  renderer = undefined;
});

describe('useVisualConfig upstream field support', () => {
  it('loads and round-trips supported CPA settings without changing unrelated YAML', () => {
    const yaml = `# keep this comment\nunknown-setting: keep-me\nremote-management:\n  disable-auto-update-panel: true\nerror-logs-max-files: 12\nauth-auto-refresh-workers: 8\npassthrough-headers: true\ndisable-cooling: true\ndisable-image-generation: chat\ngpt-image-2-base-model: gpt-5.4-mini\nplugins:\n  enabled: true\n  store-sources:\n    - https://registry.example/plugins.json\n  store-auth:\n    - match: https://registry.example/\n      apply-to: [registry, artifact]\n      type: bearer\n      token-env: PLUGIN_TOKEN\nantigravity:\n  sensitive-words: [secret]\ndevin:\n  sensitive-words: [internal]\nantigravity-signature-cache-enabled: false\nantigravity-signature-bypass-strict: true\nclaude-header-defaults:\n  user-agent: claude-cli/2.1.0\n  package-version: 0.74.0\n  runtime-version: v24.0.0\n  os: MacOS\n  arch: arm64\n  timeout: 600\n  stabilize-device-profile: true\ncodex-header-defaults:\n  user-agent: codex_cli_rs/0.114.0\n  beta-features: multi_agent\nrouting:\n  strategy: weighted-round-robin\npayload:\n  default:\n    - models:\n        - name: gpt-test\n          protocol: codex\n          from-protocol: responses\n          headers:\n            X-Client-Tier: team-*\n          match:\n            - metadata.client: codex\n          not-match:\n            - metadata.mode: dev\n          exist: [tools]\n          not-exist: [metadata.disabled]\n      params:\n        service_tier: priority\n`;

    const hookRef = createRef<HookValue>();
    const currentHook = () => hookRef.current!;
    act(() => {
      renderer = create(<HookHarness ref={hookRef} />);
    });
    act(() => {
      currentHook().loadVisualValuesFromYaml(yaml);
    });

    expect((currentHook().visualValues as any).rmDisableAutoUpdatePanel).toBe(true);
    expect((currentHook().visualValues as any).errorLogsMaxFiles).toBe('12');
    expect((currentHook().visualValues as any).authAutoRefreshWorkers).toBe('8');
    expect((currentHook().visualValues as any).disableImageGeneration).toBe('chat');
    expect((currentHook().visualValues as any).pluginsEnabled).toBe(true);
    expect((currentHook().visualValues as any).antigravitySensitiveWords).toEqual(['secret']);
    expect((currentHook().visualValues as any).devinSensitiveWords).toEqual(['internal']);
    expect((currentHook().visualValues as any).antigravitySignatureCacheEnabled).toBe(false);
    expect((currentHook().visualValues as any).antigravitySignatureBypassStrict).toBe(true);
    expect((currentHook().visualValues as any).claudeHeaderRuntimeVersion).toBe('v24.0.0');
    expect((currentHook().visualValues as any).claudeHeaderStabilizeDeviceProfile).toBe(true);
    expect((currentHook().visualValues as any).pluginStoreSources).toEqual([
      'https://registry.example/plugins.json',
    ]);
    expect((currentHook().visualValues as any).pluginStoreAuth[0]).toMatchObject({
      match: 'https://registry.example/',
      applyTo: ['registry', 'artifact'],
      type: 'bearer',
      tokenEnv: 'PLUGIN_TOKEN',
    });
    expect((currentHook().visualValues as any).claudeHeaderUserAgent).toBe('claude-cli/2.1.0');
    expect((currentHook().visualValues as any).codexHeaderBetaFeatures).toBe('multi_agent');
    expect((currentHook().visualValues as any).routingStrategy).toBe('weighted-round-robin');

    act(() => {
      currentHook().setVisualValues({
        errorLogsMaxFiles: '9',
        disableCooling: false,
        pluginStoreSources: ['https://mirror.example/registry.json'],
        pluginStoreAuth: [
          { ...(currentHook().visualValues as any).pluginStoreAuth[0], tokenEnv: 'ROTATED_PLUGIN_TOKEN' },
        ],
        antigravitySensitiveWords: ['secret', 'proxy'],
      } as any);
    });

    const saved = parse(currentHook().applyVisualChangesToYaml(yaml)) as Record<string, any>;
    expect(saved['unknown-setting']).toBe('keep-me');
    expect(saved['error-logs-max-files']).toBe(9);
    expect(saved['disable-cooling']).toBe(false);
    expect(saved.plugins['store-sources']).toEqual(['https://mirror.example/registry.json']);
    expect(saved.plugins['store-auth'][0]['token-env']).toBe('ROTATED_PLUGIN_TOKEN');
    expect(saved.antigravity['sensitive-words']).toEqual(['secret', 'proxy']);
    expect(saved['claude-header-defaults']['user-agent']).toBe('claude-cli/2.1.0');
    expect(saved.payload.default[0].models[0]).toMatchObject({
      'from-protocol': 'responses',
      headers: { 'X-Client-Tier': 'team-*' },
      match: [{ 'metadata.client': 'codex' }],
      'not-match': [{ 'metadata.mode': 'dev' }],
      exist: ['tools'],
      'not-exist': ['metadata.disabled'],
    });
  });

  it('does not add optional upstream defaults during unrelated saves', () => {
    const yaml = 'unknown-setting: keep-me\n';
    const hookRef = createRef<HookValue>();
    const currentHook = () => hookRef.current!;
    act(() => {
      renderer = create(<HookHarness ref={hookRef} />);
    });
    act(() => {
      currentHook().loadVisualValuesFromYaml(yaml);
    });

    expect(parse(currentHook().applyVisualChangesToYaml(yaml))).toEqual({ 'unknown-setting': 'keep-me' });
  });
});
