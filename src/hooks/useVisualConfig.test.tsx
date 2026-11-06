import { act, createRef, forwardRef, useImperativeHandle } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { parse } from 'yaml';
import { afterEach, describe, expect, it } from 'vitest';
import { getVisualConfigValidationErrors, useVisualConfig } from './useVisualConfig';
import { DEFAULT_VISUAL_VALUES } from '@/types/visualConfig';

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

describe('v8 visual config validation', () => {
  it('validates trusted proxy CIDRs and enabled discovery service types', () => {
    expect(
      getVisualConfigValidationErrors({
        ...DEFAULT_VISUAL_VALUES,
        trustedProxies: ['10.0.0.0/8', '2001:db8::/32'],
        discoveryEnabled: true,
        discoveryServiceType: '_gateway._tcp',
      })
    ).toMatchObject({ trustedProxies: undefined, discoveryServiceType: undefined });

    expect(
      getVisualConfigValidationErrors({
        ...DEFAULT_VISUAL_VALUES,
        trustedProxies: ['10.0.0.1/33'],
        discoveryEnabled: true,
        discoveryServiceType: 'gateway._tcp',
      })
    ).toMatchObject({
      trustedProxies: 'invalid_trusted_proxies',
      discoveryServiceType: 'invalid_discovery_service_type',
    });
  });
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

  it('loads and writes the canonical v8 layout without adding legacy keys', () => {
    const yaml = `config-version: 8
server:
  host: 127.0.0.1
  port: 8317
  github-token: old-token
  trusted-proxies: [10.0.0.0/8]
  discovery:
    enabled: true
    service-name: cpa
  tls:
    enable: true
  commercial-mode: true
management:
  allow-remote: true
oauth:
  auth-dir: .auth
  providers:
    aistudio:
      ws-auth: true
    antigravity:
      sensitive-words: [secret]
      antigravity-credits: true
      connection-pool:
        enabled: true
        idle-conn-timeout: 45s
    codex:
      header-defaults:
        user-agent: codex_cli_rs/0.114.0
      live-media-relay:
        enabled: true
        max-sessions: 12
        ice-servers:
          - urls: [turn:relay.example]
            username: old-user
            credential: old-secret
access:
  api-keys: [panel-key]
plugins:
  enabled: true
  configs:
    sample-plugin:
      mode: strict
quota:
  spend-limit: 100
upstream:
  claude:
    header-defaults:
      timezone: Asia/Singapore
    model-level-cooling: true
  codex:
    disable-codex-cloaking: true
  xai:
    inject-x-search: true
client:
  codex:
    optimize-multi-agent-v2: true
multimedia:
  video-result-auth-cache-ttl: 2h
observability:
  logs:
    debug: true
  usage:
    usage-statistics-enabled: true
requests:
  passthrough-headers: true
  payload:
    default:
      - models:
          - name: gpt-test
        params:
          service_tier: priority
routing:
  strategy: weighted-round-robin
  retry:
    request-retry: 2
  cooldown:
    disable-cooling: true
    save-cooldown-status: true
`;

    const hookRef = createRef<HookValue>();
    const currentHook = () => hookRef.current!;
    act(() => {
      renderer = create(<HookHarness ref={hookRef} />);
    });
    act(() => {
      currentHook().loadVisualValuesFromYaml(yaml);
    });

    expect(currentHook().visualValues.host).toBe('127.0.0.1');
    expect(currentHook().visualValues.port).toBe('8317');
    expect(currentHook().visualValues.tlsEnable).toBe(true);
    expect(currentHook().visualValues.rmAllowRemote).toBe(true);
    expect(currentHook().visualValues.debug).toBe(true);
    expect(currentHook().visualValues.apiKeysText).toBe('panel-key');
    expect(currentHook().visualValues.codexHeaderUserAgent).toBe('codex_cli_rs/0.114.0');
    expect(currentHook().visualValues.githubToken).toBe('old-token');
    expect(currentHook().visualValues.trustedProxies).toEqual(['10.0.0.0/8']);
    expect(currentHook().visualValues.discoveryEnabled).toBe(true);
    expect(currentHook().visualValues.claudeHeaderTimezone).toBe('Asia/Singapore');
    expect(currentHook().visualValues.codexDisableCloaking).toBe(true);
    expect(currentHook().visualValues.codexOptimizeMultiAgentV2).toBe(true);
    expect(currentHook().visualValues.antigravityConnectionPoolIdleTimeout).toBe('45s');
    expect(currentHook().visualValues.codexLiveMediaRelayICEServers[0]).toMatchObject({
      urlsText: 'turn:relay.example',
      username: 'old-user',
      credential: 'old-secret',
    });

    act(() => {
      currentHook().setVisualValues({
        port: '9000',
        debug: false,
        disableCooling: false,
        githubToken: 'new-token',
        trustedProxies: ['192.168.0.0/16'],
        discoveryEnabled: false,
        discoveryAuthRequired: false,
        routingSessionAffinitySubagents: false,
        codexDisableCloaking: false,
        codexLiveMediaRelayICEServers: [
          { id: 'ice-new', urlsText: 'turns:relay.example', username: 'new-user', credential: 'new-secret' },
        ],
      });
    });

    const saved = parse(currentHook().applyVisualChangesToYaml(yaml));
    expect(saved.server.port).toBe(9000);
    expect(saved.server['github-token']).toBe('new-token');
    expect(saved.server['trusted-proxies']).toEqual(['192.168.0.0/16']);
    expect(saved.server.discovery.enabled).toBe(false);
    expect(saved.server.discovery['auth-required']).toBe(false);
    expect(saved.routing['session-affinity-subagents']).toBe(false);
    expect(saved.observability.logs.debug).toBe(false);
    expect(saved.routing.cooldown['disable-cooling']).toBe(false);
    expect(saved.upstream.codex['disable-codex-cloaking']).toBe(false);
    expect(saved.oauth.providers.codex['live-media-relay']['ice-servers'][0]).toEqual({
      urls: ['turns:relay.example'],
      username: 'new-user',
      credential: 'new-secret',
    });
    expect(saved.port).toBeUndefined();
    expect(saved.debug).toBeUndefined();
    expect(saved.plugins.configs['sample-plugin'].mode).toBe('strict');
    expect(saved.quota['spend-limit']).toBe(100);
    expect(saved['quota-exceeded']).toBeUndefined();
  });

  it('keeps deprecated quota switches editable for legacy CPA config', () => {
    const yaml = `quota-exceeded:
  switch-project: true
  switch-preview-model: false
  antigravity-credits: true
`;
    const hookRef = createRef<HookValue>();
    const currentHook = () => hookRef.current!;
    act(() => {
      renderer = create(<HookHarness ref={hookRef} />);
    });
    act(() => {
      currentHook().loadVisualValuesFromYaml(yaml);
    });

    expect(currentHook().visualValues.quotaSwitchProject).toBe(true);
    expect(currentHook().visualValues.quotaSwitchPreviewModel).toBe(false);
    expect(currentHook().visualValues.quotaAntigravityCredits).toBe(true);

    act(() => {
      currentHook().setVisualValues({ quotaSwitchProject: false, quotaSwitchPreviewModel: true });
    });

    const saved = parse(currentHook().applyVisualChangesToYaml(yaml));
    expect(saved['quota-exceeded']['switch-project']).toBe(false);
    expect(saved['quota-exceeded']['switch-preview-model']).toBe(true);
    expect(saved['quota-exceeded']['antigravity-credits']).toBe(true);
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
