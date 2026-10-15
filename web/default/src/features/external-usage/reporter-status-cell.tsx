import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { formatTimestamp } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { getReporterStatuses } from './api'

export type ReporterStatus = {
  device_id: number
  reporter_version: string
  available_version: string
  auto_update: boolean
  phase: string
  error_code: string
  last_check_at: number
  received_at: number
}

export function ReporterStatusCell(props: {
  deviceId: number
  canView: boolean
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: ['external-usage-reporter-status', userId],
    queryFn: getReporterStatuses,
    enabled: props.canView,
    staleTime: 60_000,
    refetchInterval: 60_000,
  })
  if (!props.canView) return <span>-</span>
  if (query.isPending) return <span>{t('Loading...')}</span>
  if (query.isError) return <span>{t('Update status unavailable')}</span>
  const status = query.data?.find((row) => row.device_id === props.deviceId)
  if (!status) return <span>{t('Automatic update support not reported')}</span>
  const phases: Record<string, string> = {
    idle:
      status.available_version &&
      status.available_version !== status.reporter_version
        ? t('Waiting for release rollout')
        : t('Up to date'),
    checking: t('Checking for updates'),
    downloading: t('Downloading update'),
    prepared: t('Update prepared'),
    switching: t('Applying update'),
    health_check: t('Checking collector health'),
    committed: t('Update completed'),
    check_failed: t('Update check failed'),
    rolling_back: t('Restoring previous version'),
    rolled_back: t('Previous version restored'),
    recovery_required: t('Update recovery required'),
  }
  const stale = Date.now() / 1000 - status.received_at > 48 * 60 * 60
  return (
    <div className='flex flex-col gap-1'>
      <span>{status.reporter_version}</span>
      {status.available_version &&
      status.available_version !== status.reporter_version ? (
        <span>
          {t('Available version: {{version}}', {
            version: status.available_version,
          })}
        </span>
      ) : null}
      <Badge variant='outline'>
        {phases[status.phase] ?? t('Update status unavailable')}
      </Badge>
      <span>
        {status.auto_update
          ? t('Automatic updates on')
          : t('Automatic updates off')}
      </span>
      <span>
        {t('Last update check')}: {formatTimestamp(status.last_check_at)}
      </span>
      <span>
        {t('Status received')}: {formatTimestamp(status.received_at)}
      </span>
      {stale ? <span>{t('Update status is stale')}</span> : null}
      {status.error_code ? (
        <span>
          {t('Update error code')}: <code>{status.error_code}</code>
        </span>
      ) : null}
    </div>
  )
}
