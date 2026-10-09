import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  compareVersions,
  getSuiteUpdateDismissalStorageKey,
  getSuiteUpdateIdentity,
  isSuiteUpdateAvailable,
  startPeriodicUpdateCheck,
  SUITE_UPDATE_CHECK_INTERVAL_MS,
} from './suiteUpdate';
import type { ManagerUpdateManifest } from '@/services/api/usageService';

const manifest: ManagerUpdateManifest = {
  schema: 1,
  bundleVersion: '7.3.35-1.24.7',
  releaseTag: 'v7.3.35',
  cpaVersion: '7.3.35',
  managerVersion: '1.24.7',
  assets: [],
};

afterEach(() => {
  vi.useRealTimers();
});

describe('suite update helpers', () => {
  it('compares numeric version segments instead of lexicographic text', () => {
    expect(compareVersions('v7.3.10', '7.3.9')).toBe(1);
    expect(compareVersions('1.24', '1.24.0')).toBe(0);
    expect(compareVersions('latest', '1.0.0')).toBeNull();
  });

  it('detects an update when either CPA or Manager is newer', () => {
    expect(isSuiteUpdateAvailable(manifest, '1.24.6', '7.3.34')).toBe(true);
    expect(isSuiteUpdateAvailable(manifest, '1.24.7', '7.3.34')).toBe(true);
    expect(isSuiteUpdateAvailable(manifest, '1.24.7', '7.3.35')).toBe(false);
  });

  it('uses the release tag to deduplicate notifications and versions as fallback', () => {
    expect(getSuiteUpdateIdentity(manifest)).toBe('release:v7.3.35');
    expect(getSuiteUpdateDismissalStorageKey('release:v7.3.35')).toBe(
      'cpa-manager:suite-update-dismissed:release%3Av7.3.35'
    );
    expect(getSuiteUpdateIdentity({ ...manifest, releaseTag: '', bundleVersion: '' })).toBe(
      'versions:7.3.35:1.24.7'
    );
  });

  it('checks immediately, repeats at the configured interval, and stops on cleanup', async () => {
    vi.useFakeTimers();
    const check = vi.fn();
    const stop = startPeriodicUpdateCheck(check, SUITE_UPDATE_CHECK_INTERVAL_MS);

    expect(check).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(SUITE_UPDATE_CHECK_INTERVAL_MS * 2);
    expect(check).toHaveBeenCalledTimes(3);

    stop();
    await vi.advanceTimersByTimeAsync(SUITE_UPDATE_CHECK_INTERVAL_MS);
    expect(check).toHaveBeenCalledTimes(3);
  });

  it('does not overlap a slow check with the next polling interval', async () => {
    vi.useFakeTimers();
    let finishCheck!: () => void;
    const check = vi.fn(() => new Promise<void>((resolve) => (finishCheck = resolve)));
    const stop = startPeriodicUpdateCheck(check, 1000);

    await vi.advanceTimersByTimeAsync(3000);
    expect(check).toHaveBeenCalledTimes(1);

    finishCheck();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(1000);
    expect(check).toHaveBeenCalledTimes(2);
    stop();
  });
});
