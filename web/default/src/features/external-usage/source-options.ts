import type { SystemStatus } from '@/features/auth/types'

const FALLBACK_CLIENT_SOURCES = ['codex']

function normalizeSources(values: unknown): string[] {
  if (!Array.isArray(values)) {
    return []
  }
  const result: string[] = []
  const seen = new Set<string>()
  for (const value of values) {
    if (typeof value !== 'string') continue
    const normalized = value.trim().toLowerCase()
    if (!normalized || seen.has(normalized)) continue
    seen.add(normalized)
    result.push(normalized)
  }
  return result
}

export function getDefaultDeviceAllowedSources(
  status: Partial<SystemStatus>
): string[] {
  const normalized = normalizeSources(status.external_usage_allowed_sources)
  return normalized.length > 0 ? normalized : [...FALLBACK_CLIENT_SOURCES]
}

export function buildExternalUsageSourceOptions(
  status: Partial<SystemStatus>
): string[] {
  const options = ['cursor', ...getDefaultDeviceAllowedSources(status)]
  return options.filter(
    (value, index) => value && options.indexOf(value) === index
  )
}

export function getExternalUsageMaxDevicesPerUser(
  status: Partial<SystemStatus>
): number {
  const raw = status.external_usage_max_devices_per_user
  const value =
    typeof raw === 'number'
      ? raw
      : typeof raw === 'string'
        ? Number(raw)
        : NaN
  if (!Number.isFinite(value) || value <= 0) {
    return 3
  }
  if (value > 3) {
    return 3
  }
  return Math.floor(value)
}
