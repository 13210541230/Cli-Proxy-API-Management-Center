import { describe, expect, it } from 'vitest';
import {
  buildPluginResourceRoute,
  collectPluginResourceEntries,
  getPluginTitle,
  resolvePluginAssetURL,
} from './pluginResources';
import type { ManagementPluginEntry } from '@/types/plugin';

const plugin = (overrides: Partial<ManagementPluginEntry> = {}): ManagementPluginEntry => ({
  id: 'demo/plugin',
  registered: true,
  enabled: true,
  effective_enabled: true,
  ...overrides,
});

describe('plugin resource host helpers', () => {
  it('only creates entries for effectively enabled plugins with resource paths', () => {
    const entries = collectPluginResourceEntries([
      plugin({ metadata: { name: 'Demo Plugin' }, menus: [{ path: '/v0/resource/plugins/demo/page', menu: 'Workspace' }] }),
      plugin({ id: 'disabled', effective_enabled: false, menus: [{ path: '/v0/resource/plugins/disabled/page' }] }),
      plugin({ id: 'empty', menus: [{ path: '' }] }),
    ]);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      pluginID: 'demo/plugin',
      pluginTitle: 'Demo Plugin',
      label: 'Workspace',
      route: '/plugin-pages/demo%2Fplugin/0',
    });
  });

  it('builds stable routes and resolves resource URLs against the configured API base', () => {
    expect(buildPluginResourceRoute('demo/plugin', 2)).toBe('/plugin-pages/demo%2Fplugin/2');
    expect(resolvePluginAssetURL('/v0/resource/plugins/demo/page', 'http://localhost:8317/v0/management'))
      .toBe('http://localhost:8317/v0/resource/plugins/demo/page');
    expect(resolvePluginAssetURL('https://plugins.example.test/page', '')).toBe('https://plugins.example.test/page');
  });

  it('falls back to the plugin id when metadata has no display name', () => {
    expect(getPluginTitle(plugin())).toBe('demo/plugin');
  });
});
