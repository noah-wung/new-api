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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Coins,
  Gauge,
  HeartPulse,
  Timer,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getUserQuotaDates } from '@/features/dashboard/api'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { formatNumber, formatTokens } from '@/lib/format'
import type { PerfModelSummary } from '@/features/performance-metrics/types'

const PERFORMANCE_WINDOW_HOURS = 24
const TOP_MODEL_LIMIT = 8

type WeightedMetric = 'avg_latency_ms' | 'avg_tps' | 'success_rate'

type PerformanceSummary = {
  totalRequests: number
  avgLatencyMs: number
  avgTps: number
  successRate: number
  totalTokens: number
}

function simpleAverage(
  rows: PerfModelSummary[],
  metric: WeightedMetric,
  isValid: (value: number) => boolean
): number {
  let total = 0
  let count = 0

  for (const row of rows) {
    const value = Number(row[metric])
    if (!isValid(value)) continue
    total += value
    count++
  }

  return count > 0 ? total / count : NaN
}

function buildPerformanceSummary(
  rows: PerfModelSummary[],
  totalTokens: number
): PerformanceSummary {
  const totalRequests = rows.reduce(
    (sum, row) => sum + (Number(row.request_count) || 0),
    0
  )

  return {
    totalRequests,
    avgLatencyMs: Math.round(
      simpleAverage(
        rows,
        'avg_latency_ms',
        (value) => Number.isFinite(value) && value > 0
      )
    ),
    avgTps: simpleAverage(
      rows,
      'avg_tps',
      (value) => Number.isFinite(value) && value > 0
    ),
    successRate: simpleAverage(rows, 'success_rate', Number.isFinite),
    totalTokens,
  }
}

function successRateClassName(successRate: number): string {
  if (!Number.isFinite(successRate)) return 'text-muted-foreground'
  if (successRate >= 99.9) return 'text-emerald-600 dark:text-emerald-400'
  if (successRate >= 99) return 'text-amber-600 dark:text-amber-400'
  return 'text-rose-600 dark:text-rose-400'
}

function successDotClassName(successRate: number): string {
  if (!Number.isFinite(successRate)) return 'bg-muted-foreground'
  if (successRate >= 99.9) return 'bg-emerald-500'
  if (successRate >= 99) return 'bg-amber-500'
  return 'bg-rose-500'
}

function PerformanceTableHeader(props: { description: string }) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-1.5 border-b px-3 py-2 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
      <div className='flex items-center gap-2'>
        <Activity className='text-muted-foreground/60 size-4' />
        <div className='text-sm font-semibold'>
          {t('Model performance metrics')}
        </div>
      </div>
      <span className='text-muted-foreground text-xs'>{props.description}</span>
    </div>
  )
}

interface PerformanceOverviewProps {
  isAdmin?: boolean
}

export function PerformanceOverview({
  isAdmin = false,
}: PerformanceOverviewProps) {
  const { t } = useTranslation()
  const metricsQuery = useQuery({
    queryKey: ['perf-metrics-summary', PERFORMANCE_WINDOW_HOURS],
    queryFn: () => getPerfMetricsSummary(PERFORMANCE_WINDOW_HOURS),
    staleTime: 60 * 1000,
    retry: false,
  })

  const tokenQuery = useQuery({
    queryKey: [
      'dashboard',
      'perf-token-data',
      isAdmin,
      PERFORMANCE_WINDOW_HOURS,
    ] as const,
    queryFn: async ({ queryKey }) => {
      const [, , queryIsAdmin, windowHours] = queryKey
      const now = Math.floor(Date.now() / 1000)
      const res = await getUserQuotaDates(
        {
          start_timestamp: now - windowHours * 3600,
          end_timestamp: now,
        },
        queryIsAdmin
      )
      if (!res.success) {
        throw new Error(res.message || 'Failed to fetch usage')
      }
      return res.data ?? []
    },
    staleTime: 60 * 1000,
    retry: false,
  })

  const tokenByModel = useMemo(() => {
    const map = new Map<string, number>()
    if (!tokenQuery.data) return map
    for (const item of tokenQuery.data) {
      const model = item.model_name || 'Unknown'
      const tokens = Number(item.token_used) || 0
      map.set(model, (map.get(model) || 0) + tokens)
    }
    return map
  }, [tokenQuery.data])

  const totalTokens = useMemo(() => {
    let sum = 0
    for (const v of tokenByModel.values()) sum += v
    return sum
  }, [tokenByModel])

  const models = useMemo(
    () =>
      [...(metricsQuery.data?.data.models ?? [])]
        .filter((model) => Number(model.request_count) > 0)
        .sort((a, b) => (b.request_count ?? 0) - (a.request_count ?? 0)),
    [metricsQuery.data]
  )
  const summary = useMemo(
    () => buildPerformanceSummary(models, totalTokens),
    [models, totalTokens]
  )
  const topModels = useMemo(() => models.slice(0, TOP_MODEL_LIMIT), [models])
  const loading = metricsQuery.isLoading
  const tokenDataUnavailable = tokenQuery.isError
  const hasData = models.length > 0
  const description = t('Performance metrics for the last 24 hours')

  if (!loading && !hasData) {
    return (
      <div className='text-muted-foreground overflow-hidden rounded-lg border px-4 py-3 text-center text-xs'>
        {t('No performance data available')}
      </div>
    )
  }

  return (
    <section className='space-y-3 sm:space-y-4'>
      <div className='overflow-hidden rounded-lg border'>
        <div className='flex flex-wrap items-center gap-x-5 gap-y-2.5 px-4 py-2.5 sm:px-5 sm:py-3'>
          {/* Title */}
          <div className='flex items-center gap-1.5'>
            <HeartPulse
              className='text-muted-foreground/60 size-3.5 shrink-0'
              aria-hidden='true'
            />
            <span className='text-xs font-semibold whitespace-nowrap'>
              {t('Performance health')}
            </span>
          </div>

          {/* Separator */}
          <div className='bg-border hidden h-4 w-px sm:block' />

          {/* 4 KPI inline metrics */}
          {loading ? (
            <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className='flex items-center gap-1.5'>
                  <Skeleton className='h-3 w-14' />
                  <Skeleton className='h-4 w-16' />
                </div>
              ))}
            </div>
          ) : (
            <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
              <InlineMetric
                icon={HeartPulse}
                label={t('Success rate')}
                value={formatUptimePct(summary.successRate)}
                valueClassName={successRateClassName(summary.successRate)}
              />
              <InlineMetric
                icon={Timer}
                label={t('Average latency')}
                value={formatLatency(summary.avgLatencyMs)}
              />
              <InlineMetric
                icon={Gauge}
                label={t('Throughput')}
                value={formatThroughput(summary.avgTps)}
              />
              <InlineMetric
                icon={Coins}
                label={t('Tokens (24h)')}
                value={
                  tokenDataUnavailable
                    ? t('Not available')
                    : formatTokens(summary.totalTokens)
                }
              />
            </div>
          )}
        </div>
      </div>

      <div className='overflow-hidden rounded-lg border'>
        <PerformanceTableHeader description={description} />
        {!loading && !hasData ? (
          <div className='text-muted-foreground p-6 text-center text-sm'>
            {t('No performance data available')}
          </div>
        ) : (
          <div className='overflow-x-auto'>
            <Table className='text-sm'>
              <TableHeader>
                <TableRow className='hover:bg-transparent'>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead className='text-right'>
                    {t('Requests (24h)')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Average latency')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Throughput')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Success rate')}
                  </TableHead>
                  <TableHead className='text-right'>{t('Tokens')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading
                  ? Array.from({ length: 4 }).map((_, index) => (
                      <TableRow key={index}>
                        <TableCell>
                          <Skeleton className='h-4 w-40' />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Skeleton className='ml-auto h-4 w-16' />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Skeleton className='ml-auto h-4 w-16' />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Skeleton className='ml-auto h-4 w-16' />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Skeleton className='ml-auto h-4 w-20' />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Skeleton className='ml-auto h-4 w-16' />
                        </TableCell>
                      </TableRow>
                    ))
                  : topModels.map((model) => (
                      <TableRow key={model.model_name}>
                        <TableCell className='max-w-[220px] truncate font-mono'>
                          {model.model_name}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatNumber(model.request_count)}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatLatency(model.avg_latency_ms)}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatThroughput(model.avg_tps)}
                        </TableCell>
                        <TableCell
                          className={cn(
                            'text-right font-mono font-semibold tabular-nums',
                            successRateClassName(model.success_rate)
                          )}
                        >
                          <span className='inline-flex items-center justify-end gap-1.5'>
                            <span
                              className={cn(
                                'size-2 rounded-full',
                                successDotClassName(model.success_rate)
                              )}
                              aria-hidden='true'
                            />
                            {formatUptimePct(model.success_rate)}
                          </span>
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {tokenDataUnavailable
                            ? t('Not available')
                            : formatTokens(
                                tokenByModel.get(model.model_name) || 0
                              )}
                        </TableCell>
                      </TableRow>
                    ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </section>
  )
}

function InlineMetric(props: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
  valueClassName?: string
}) {
  const Icon = props.icon

  return (
    <div className='flex items-center gap-1.5'>
      <Icon
        className='text-muted-foreground/50 size-3 shrink-0'
        aria-hidden='true'
      />
      <span className='text-muted-foreground text-[11px]'>{props.label}</span>
      <span
        className={cn(
          'font-mono text-xs font-semibold tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </span>
    </div>
  )
}
