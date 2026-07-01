export type ExternalUsageInstallerPlatform = 'macos' | 'windows'

export type ExternalUsageInstallerCommandData = {
  command: string
  expires_at?: number | string | null
}

type Translate = (key: string) => string

export function buildInstallerCommandCardCopy(t: Translate) {
  return {
    title: t('Codex Installer Command'),
    description: t('Generate a platform-specific installer command for Codex usage reporting.'),
    actionLabel: t('Report Codex Usage'),
    platformLabel: t('Platform'),
    platformOptions: [
      { value: 'macos' as const, label: t('macOS') },
      { value: 'windows' as const, label: t('Windows') },
    ],
    commandLabel: t('Installer Command'),
    expiresLabel: t('Expires At'),
    unavailableTitle: t('Command Unavailable'),
    unavailableDescription: t(
      'Sign in as the selected user to generate a Codex installer command.'
    ),
    emptyState: t('Generate an installer command to enable Codex usage reporting on this device.'),
  }
}

export function normalizeInstallerCommandExpiry(
  expiresAt?: number | string | null
): number | null {
  if (typeof expiresAt === 'number' && Number.isFinite(expiresAt) && expiresAt > 0) {
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
