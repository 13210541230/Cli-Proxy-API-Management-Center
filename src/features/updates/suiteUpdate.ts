import type { ManagerUpdateManifest } from '@/services/api/usageService';

type ParsedVersion = {
  numeric: number[];
  prerelease: string[] | null;
};

const parseVersion = (version?: string | null): ParsedVersion | null => {
  if (!version) return null;
  const cleaned = version.trim().replace(/^v/i, '');
  const match = cleaned.match(/^(\d+(?:\.\d+)*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/);
  if (!match) return null;
  const numeric = match[1].split('.').map((segment) => Number.parseInt(segment, 10));
  if (numeric.some((segment) => !Number.isSafeInteger(segment))) return null;
  return { numeric, prerelease: match[2] ? match[2].split('.') : null };
};

const comparePrerelease = (latest: string[], current: string[]) => {
  const length = Math.max(latest.length, current.length);
  for (let index = 0; index < length; index++) {
    const latestIdentifier = latest[index];
    const currentIdentifier = current[index];
    if (latestIdentifier === undefined) return -1;
    if (currentIdentifier === undefined) return 1;
    const latestNumeric = /^\d+$/.test(latestIdentifier);
    const currentNumeric = /^\d+$/.test(currentIdentifier);
    if (latestNumeric && currentNumeric) {
      const difference = Number(latestIdentifier) - Number(currentIdentifier);
      if (difference !== 0) return difference > 0 ? 1 : -1;
    } else if (latestNumeric !== currentNumeric) {
      return latestNumeric ? -1 : 1;
    } else if (latestIdentifier !== currentIdentifier) {
      return latestIdentifier > currentIdentifier ? 1 : -1;
    }
  }
  return 0;
};

export const compareVersions = (latest?: string | null, current?: string | null) => {
  const latestVersion = parseVersion(latest);
  const currentVersion = parseVersion(current);
  if (!latestVersion || !currentVersion) return null;
  const length = Math.max(latestVersion.numeric.length, currentVersion.numeric.length);
  for (let index = 0; index < length; index++) {
    const latestSegment = latestVersion.numeric[index] || 0;
    const currentSegment = currentVersion.numeric[index] || 0;
    if (latestSegment > currentSegment) return 1;
    if (latestSegment < currentSegment) return -1;
  }
  if (latestVersion.prerelease === null && currentVersion.prerelease !== null) return 1;
  if (latestVersion.prerelease !== null && currentVersion.prerelease === null) return -1;
  if (latestVersion.prerelease === null || currentVersion.prerelease === null) return 0;
  return comparePrerelease(latestVersion.prerelease, currentVersion.prerelease);
};

export const isSuiteUpdateAvailable = (
  manifest: ManagerUpdateManifest,
  currentManagerVersion?: string | null,
  currentCPAVersion?: string | null
) =>
  compareVersions(manifest.managerVersion, currentManagerVersion) === 1 ||
  compareVersions(manifest.cpaVersion, currentCPAVersion) === 1;
