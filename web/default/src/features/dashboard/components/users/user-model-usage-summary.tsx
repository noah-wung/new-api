/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Alert01Icon,
  Refresh01Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  ActivityIcon,
  CoinsIcon,
  GaugeIcon,
  ListChecksIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatNumber, formatQuota, formatTokens } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { MultiSelect } from '@/components/multi-select'
import { getUserModelUsage } from '@/features/dashboard/api'
import {
  decodeUsageSources,
  encodeUsageSources,
  type UserAnalyticsRange,
} from '@/features/dashboard/lib'
import type {
  UserAnalyticsSearch,
  UserModelUsageSortField,
  UserModelUsageSortOrder,
  UserModelUsageTarget,
} from '@/features/dashboard/types'
import { StatCard } from '../ui/stat-card'
import { UserModelUsageTable } from './user-model-usage-table'

interface UserModelUsageSummaryProps {
  userId?: number
  timeRange: UserAnalyticsRange
  search: UserAnalyticsSearch
  onSearchChange: (patch: Partial<UserAnalyticsSearch>) => void
  onUserMetadata: (user: UserModelUsageTarget) => void
}

// eslint-disable-next-line react-refresh/only-export-components
export function resolveSelectedSources(
  sources: string | undefined,
  availableSources: readonly string[]
): string[] {
  if (sources == null) return [...availableSources]
  return decodeUsageSources(sources)
}

// eslint-disable-next-line react-refresh/only-export-components
export function encodeSelectedSources(
  selectedSources: readonly string[],
  availableSources: readonly string[]
): string | undefined {
  const selected = decodeUsageSources(selectedSources.join(','))
  const available = decodeUsageSources(availableSources.join(','))
  const selectedSet = new Set(selected)
  if (
    selected.length === available.length &&
    available.every((source) => selectedSet.has(source))
  ) {
    return undefined
  }
  return encodeUsageSources(selected) || undefined
}

// eslint-disable-next-line react-refresh/only-export-components
export function createManualSortPatch(
  sortBy: UserModelUsageSortField,
  sortOrder: UserModelUsageSortOrder
): Partial<UserAnalyticsSearch> {
  return {
    sort_mode: 'manual',
    sort_by: sortBy,
    sort_order: sortOrder,
  }
}

export function UserModelUsageSummary(props: UserModelUsageSummaryProps) {
  const { t } = useTranslation()
  const { onUserMetadata } = props
  const requestSources = decodeUsageSources(props.search.sources)
  const modelSearch = props.search.model_search
  const sortBy = props.search.sort_by ?? 'quota'
  const sortOrder = props.search.sort_order ?? 'desc'
  const page = props.search.p ?? 1
  const pageSize = props.search.page_size ?? 20

  const summaryQuery = useQuery({
    queryKey: [
      'dashboard',
      'user-model-usage',
      props.userId,
      props.timeRange,
      requestSources,
      modelSearch,
      sortBy,
      sortOrder,
      page,
      pageSize,
    ],
    queryFn: async () => {
      try {
        const response = await getUserModelUsage(props.userId!, {
          ...props.timeRange,
          sources: requestSources.length > 0 ? requestSources : undefined,
          model_search: modelSearch,
          sort_by: sortBy,
          sort_order: sortOrder,
          p: page,
          page_size: pageSize,
        })
        if (!response.success || !response.data) {
          throw new Error(response.message || 'Failed to load model usage')
        }
        return response.data
      } catch (error) {
        const responseMessage = (
          error as { response?: { data?: { message?: unknown } } }
        ).response?.data?.message
        throw new Error(
          typeof responseMessage === 'string'
            ? responseMessage
            : error instanceof Error
              ? error.message
              : 'Failed to load model usage',
          { cause: error }
        )
      }
    },
    enabled: props.userId != null,
    staleTime: 60_000,
  })

  const data = summaryQuery.data

  useEffect(() => {
    if (data?.user) onUserMetadata(data.user)
  }, [data?.user, onUserMetadata])

  if (props.userId == null) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('Model usage summary')}</CardTitle>
          <CardDescription>
            {t('Review one user across gateway and external usage sources.')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
              </EmptyMedia>
              <EmptyTitle>{t('Select a user')}</EmptyTitle>
              <EmptyDescription>
                {t('Search for a user or click a bar in the user ranking.')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>
    )
  }

  const selectedSources = resolveSelectedSources(
    props.search.sources,
    data?.available_sources ?? []
  )
  const isFiltered = Boolean(modelSearch || props.search.sources)
  const totals = data?.totals
  const statItems = [
    {
      key: 'token-usage',
      title: t('Token usage'),
      value: formatTokens(totals?.token_usage ?? 0),
      description: t('Raw tokens across every filtered model'),
      icon: GaugeIcon,
    },
    {
      key: 'quota',
      title: t('Quota'),
      value: formatQuota(totals?.quota ?? 0),
      description: t('Gateway billing consumption'),
      icon: CoinsIcon,
    },
    {
      key: 'gateway-requests',
      title: t('Gateway requests'),
      value: formatNumber(totals?.gateway_requests ?? 0),
      description: t('Requests handled by the gateway'),
      icon: ListChecksIcon,
    },
    {
      key: 'external-events',
      title: t('External events'),
      value: formatNumber(totals?.external_events ?? 0),
      description: t('Usage events reported by external tools'),
      icon: ActivityIcon,
    },
  ]

  return (
    <div className='flex flex-col gap-3'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Model usage summary')}</CardTitle>
          <CardDescription>
            {t(
              'Totals cover the complete filtered result, not only this page.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            {statItems.map((item) => (
              <div key={item.key} className='rounded-xl border p-3'>
                <StatCard
                  title={item.title}
                  value={item.value}
                  description={item.description}
                  icon={item.icon}
                  loading={summaryQuery.isPending}
                  error={summaryQuery.isError && !data}
                />
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {summaryQuery.isError && (
        <Alert variant='destructive'>
          <HugeiconsIcon icon={Alert01Icon} strokeWidth={2} />
          <AlertTitle>{t('Failed to load model usage')}</AlertTitle>
          <AlertDescription className='flex flex-col items-start gap-2'>
            <span>
              {summaryQuery.error instanceof Error
                ? summaryQuery.error.message
                : t('An unexpected server error occurred.')}
            </span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={summaryQuery.isFetching}
              onClick={() => void summaryQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {data && !data.collection_status.data_export_enabled && (
        <Alert>
          <HugeiconsIcon icon={Alert01Icon} strokeWidth={2} />
          <AlertTitle>{t('Dashboard aggregation is disabled')}</AlertTitle>
          <AlertDescription>
            {t('New dashboard aggregates are not being collected.')}
          </AlertDescription>
        </Alert>
      )}

      {data && !data.collection_status.consume_log_enabled && (
        <Alert>
          <HugeiconsIcon icon={Alert01Icon} strokeWidth={2} />
          <AlertTitle>{t('Consume-log collection is disabled')}</AlertTitle>
          <AlertDescription>
            {t('New gateway request usage may be missing from this summary.')}
          </AlertDescription>
        </Alert>
      )}

      {data && !data.collection_status.external_usage_enabled && (
        <Alert>
          <HugeiconsIcon icon={Alert01Icon} strokeWidth={2} />
          <AlertTitle>{t('External usage collection is disabled')}</AlertTitle>
          <AlertDescription>
            {t('Historical external usage remains visible in this summary.')}
          </AlertDescription>
        </Alert>
      )}

      {data ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('Models and sources')}</CardTitle>
            <CardDescription>
              {summaryQuery.isFetching
                ? t('Refreshing model usage…')
                : t(
                    'Cached for 60 seconds. Server aggregates refresh every {{minutes}} minutes.',
                    {
                      minutes: data.collection_status.refresh_interval_minutes,
                    }
                  )}
            </CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-4'>
            <FieldGroup className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]'>
              <Field>
                <FieldLabel htmlFor='user-model-usage-model-search'>
                  {t('Model name')}
                </FieldLabel>
                <Input
                  id='user-model-usage-model-search'
                  value={modelSearch ?? ''}
                  aria-label={t('Model name')}
                  placeholder={t('Filter by literal model name')}
                  onChange={(event) =>
                    props.onSearchChange({
                      model_search: event.target.value || undefined,
                    })
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor='user-model-usage-sources'>
                  {t('Sources')}
                </FieldLabel>
                <MultiSelect
                  id='user-model-usage-sources'
                  options={data.available_sources.map((source) => ({
                    label: source,
                    value: source,
                  }))}
                  selected={selectedSources}
                  onChange={(sources) =>
                    props.onSearchChange({
                      sources: encodeSelectedSources(
                        sources,
                        data.available_sources
                      ),
                    })
                  }
                  placeholder={t('Select sources')}
                  emptyText={t('No sources available')}
                />
              </Field>
              <Field>
                <FieldTitle>{t('Data')}</FieldTitle>
                <Button
                  type='button'
                  variant='outline'
                  disabled={summaryQuery.isFetching}
                  onClick={() => void summaryQuery.refetch()}
                >
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {summaryQuery.isFetching ? t('Refreshing') : t('Refresh')}
                </Button>
              </Field>
            </FieldGroup>

            {data.total === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
                  </EmptyMedia>
                  <EmptyTitle>
                    {isFiltered
                      ? t('No matching usage records')
                      : t('No usage records')}
                  </EmptyTitle>
                  <EmptyDescription>
                    {isFiltered
                      ? t('Clear the filters to review all model usage.')
                      : t(
                          'This user has no model usage in the selected range.'
                        )}
                  </EmptyDescription>
                </EmptyHeader>
                {isFiltered && (
                  <EmptyContent>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() =>
                        props.onSearchChange({
                          sources: undefined,
                          model_search: undefined,
                        })
                      }
                    >
                      {t('Clear filters')}
                    </Button>
                  </EmptyContent>
                )}
              </Empty>
            ) : (
              <UserModelUsageTable
                items={data.items}
                page={data.page}
                pageSize={data.page_size}
                total={data.total}
                sortBy={sortBy}
                sortOrder={sortOrder}
                onSortChange={(nextSortBy, nextSortOrder) =>
                  props.onSearchChange(
                    createManualSortPatch(nextSortBy, nextSortOrder)
                  )
                }
                onPageChange={(nextPage) =>
                  props.onSearchChange({ p: nextPage })
                }
                onPageSizeChange={(nextPageSize) =>
                  props.onSearchChange({ p: 1, page_size: nextPageSize })
                }
              />
            )}
          </CardContent>
        </Card>
      ) : summaryQuery.isPending ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('Models and sources')}</CardTitle>
            <CardDescription>{t('Loading model usage')}</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <Skeleton className='h-9 w-full' />
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full' />
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}
