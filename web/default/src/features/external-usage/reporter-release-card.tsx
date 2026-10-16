import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  activateReporterRelease,
  getReporterReleases,
  pauseReporterRelease,
} from './api'

export type ReporterReleases = {
  releases: { release_id: string; reporter_version: string }[]
  deployment: { release_id: string; sequence: number; paused: boolean } | null
}

export function ReporterReleaseCard() {
  const { t } = useTranslation()
  const [releaseId, setReleaseId] = useState('')
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['reporter-releases'],
    queryFn: getReporterReleases,
  })
  const mutation = useMutation({
    mutationFn: async (action: 'activate' | 'pause') => {
      if (action === 'pause') return pauseReporterRelease()
      return activateReporterRelease(releaseId.trim())
    },
    onSuccess: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: ['reporter-releases'] }),
        client.invalidateQueries({
          queryKey: ['external-usage-client-installer-options'],
        }),
      ]),
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Collector Releases')}</CardTitle>
        <CardDescription>
          {t(
            'Activate a signed release already deployed by the administrator. Rollout percentages are defined in its signed catalog.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {query.data?.deployment ? (
          <p>
            {query.data.deployment.release_id} —{' '}
            {query.data.deployment.paused
              ? t('Distribution paused')
              : t('Distribution active')}
          </p>
        ) : (
          <p>{t('No active collector release')}</p>
        )}
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor='collector-release-id'>
              {t('Signed release ID')}
            </FieldLabel>
            <Input
              id='collector-release-id'
              value={releaseId}
              onChange={(event) => setReleaseId(event.target.value)}
              disabled={mutation.isPending}
            />
          </Field>
        </FieldGroup>
        <div className='flex flex-wrap gap-2'>
          <Button
            disabled={
              mutation.isPending ||
              !/^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$/.test(releaseId.trim())
            }
            onClick={() => mutation.mutate('activate')}
          >
            {t('Activate signed release')}
          </Button>
          <Button
            variant='outline'
            disabled={
              mutation.isPending ||
              !query.data?.deployment ||
              query.data.deployment.paused
            }
            onClick={() => mutation.mutate('pause')}
          >
            {t('Pause collector updates')}
          </Button>
        </div>
        {mutation.isError || query.isError ? (
          <p role='alert'>{t('Collector release operation failed')}</p>
        ) : null}
      </CardContent>
    </Card>
  )
}
