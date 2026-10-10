import { describe, expect, it } from 'vitest';
import { compareVersions, isSuiteUpdateAvailable } from './suiteUpdate';
import type { ManagerUpdateManifest } from '@/services/api/usageService';

const manifest: ManagerUpdateManifest = {
  schema: 1,
  bundleVersion: '7.3.35-1.24.7',
  releaseTag: 'v7.3.35',
  cpaVersion: '7.3.35',
  managerVersion: '1.24.7',
  assets: [],
};

describe('suite update helpers', () => {
  it('compares numeric version segments instead of lexicographic text', () => {
    expect(compareVersions('v7.3.10', '7.3.9')).toBe(1);
    expect(compareVersions('1.24', '1.24.0')).toBe(0);
    expect(compareVersions('1.0.0-rc.10', '1.0.0-rc.2')).toBe(1);
    expect(compareVersions('1.0.0', '1.0.0-rc.10')).toBe(1);
    expect(compareVersions('latest', '1.0.0')).toBeNull();
  });

  it('detects an update when either CPA or Manager is newer', () => {
    expect(isSuiteUpdateAvailable(manifest, '1.24.6', '7.3.34')).toBe(true);
    expect(isSuiteUpdateAvailable(manifest, '1.24.7', '7.3.34')).toBe(true);
    expect(isSuiteUpdateAvailable(manifest, '1.24.7', '7.3.35')).toBe(false);
  });
});
