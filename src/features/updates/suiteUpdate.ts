import type { ManagerUpdateManifest } from '@/services/api/usageService';

export const SUITE_UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;
export const SUITE_UPDATE_DISMISSED_STORAGE_KEY = 'cpa-manager:suite-update-dismissed';

export const getSuiteUpdateDismissalStorageKey = (identity: string) =>
  `${SUITE_UPDATE_DISMISSED_STORAGE_KEY}:${encodeURIComponent(identity)}`;

const parseVersionSegments = (version?: string | null) => {
  if (!version) return null;
  const cleaned = version.trim().replace(/^v/i, '');
  if (!cleaned) return null;
  const parts = cleaned
    .split(/[^0-9]+/)
    .filter(Boolean)
    .map((segment) => Number.parseInt(segment, 10))
    .filter(Number.isFinite);
  return parts.length ? parts : null;
};

export const compareVersions = (latest?: string | null, current?: string | null) => {
  const latestParts = parseVersionSegments(latest);
  const currentParts = parseVersionSegments(current);
  if (!latestParts || !currentParts) return null;
  const length = Math.max(latestParts.length, currentParts.length);
  for (let i = 0; i < length; i++) {
    const latestSegment = latestParts[i] || 0;
    const currentSegment = currentParts[i] || 0;
    if (latestSegment > currentSegment) return 1;
    if (latestSegment < currentSegment) return -1;
  }
  return 0;
};

export const isSuiteUpdateAvailable = (
  manifest: ManagerUpdateManifest,
  currentManagerVersion?: string | null,
  currentCPAVersion?: string | null
) =>
  compareVersions(manifest.managerVersion, currentManagerVersion) === 1 ||
  compareVersions(manifest.cpaVersion, currentCPAVersion) === 1;

export const getSuiteUpdateIdentity = (manifest: ManagerUpdateManifest) => {
  const releaseTag = manifest.releaseTag.trim();
  if (releaseTag) return `release:${releaseTag}`;
  return `versions:${manifest.cpaVersion.trim()}:${manifest.managerVersion.trim()}`;
};

export const startPeriodicUpdateCheck = (
  check: () => void | Promise<void>,
  intervalMs = SUITE_UPDATE_CHECK_INTERVAL_MS
) => {
  let stopped = false;
  let inFlight = false;

  const run = async () => {
    if (stopped || inFlight) return;
    inFlight = true;
    try {
      await check();
    } catch {
      // A periodic update check is best-effort and must not interrupt normal use.
    } finally {
      inFlight = false;
    }
  };

  void run();
  const timer = setInterval(() => void run(), intervalMs);

  return () => {
    stopped = true;
    clearInterval(timer);
  };
};
