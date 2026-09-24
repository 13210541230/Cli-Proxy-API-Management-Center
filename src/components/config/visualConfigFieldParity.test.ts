import { describe, expect, it } from 'vitest';
import editor from './VisualConfigEditor.tsx?raw';
import blocks from './VisualConfigEditorBlocks.tsx?raw';
const source = `${editor}\n${blocks}`;

const upstreamFields = [
  'rmDisableAutoUpdatePanel',
  'errorLogsMaxFiles',
  'pluginsEnabled',
  'pluginStoreSources',
  'pluginStoreAuth',
  'authAutoRefreshWorkers',
  'passthroughHeaders',
  'disableCooling',
  'disableImageGeneration',
  'gptImage2BaseModel',
  'antigravitySensitiveWords',
  'devinSensitiveWords',
  'antigravitySignatureCacheEnabled',
  'antigravitySignatureBypassStrict',
  'claudeHeaderUserAgent',
  'claudeHeaderPackageVersion',
  'claudeHeaderRuntimeVersion',
  'claudeHeaderOs',
  'claudeHeaderArch',
  'claudeHeaderTimeout',
  'claudeHeaderStabilizeDeviceProfile',
  'codexHeaderUserAgent',
  'codexHeaderBetaFeatures',
];

describe('visual config editor upstream field coverage', () => {
  it.each(upstreamFields)('renders a control for %s', (field) => {
    expect(source.includes(`values.${field}`), `missing control for ${field}`).toBe(true);
  });

  it('supports weighted round-robin routing', () => {
    expect(source.includes('weighted-round-robin'), 'missing weighted round-robin option').toBe(
      true
    );
  });

  it('edits advanced payload model matching fields', () => {
    for (const field of ['fromProtocol', 'headers', 'match', 'notMatch']) {
      expect(blocks.includes(`model.${field}`), `missing payload editor for ${field}`).toBe(true);
    }
    expect(blocks.includes("(['exist', 'notExist'] as const)")).toBe(true);
  });
});
