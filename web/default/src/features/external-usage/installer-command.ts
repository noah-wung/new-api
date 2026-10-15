export type ExternalUsageInstallerPlatform = 'macos' | 'windows'

export type ExternalUsageInstallerCommandData = {
  command: string
  expires_at?: number | string | null
}

export function normalizeInstallerCommandExpiry(
  expiresAt?: number | string | null
): number | null {
  if (
    typeof expiresAt === 'number' &&
    Number.isFinite(expiresAt) &&
    expiresAt > 0
  ) {
    return expiresAt
  }
  if (typeof expiresAt === 'string') {
    const normalized = expiresAt.trim()
    if (!normalized) return null
    const parsedAsNumber = Number(normalized)
    if (Number.isFinite(parsedAsNumber) && parsedAsNumber > 0) {
      return parsedAsNumber
    }
    const parsedAsDate = Math.floor(new Date(normalized).getTime() / 1000)
    if (Number.isFinite(parsedAsDate) && parsedAsDate > 0) {
      return parsedAsDate
    }
  }
  return null
}
