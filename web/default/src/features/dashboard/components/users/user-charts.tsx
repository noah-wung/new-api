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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { UserMultiple02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { VChart } from '@visactor/react-vchart'
import type { EventParamsDefinition } from '@visactor/vchart'
import { useTranslation } from 'react-i18next'
import type { TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserQuotaDataByUsers } from '@/features/dashboard/api'
import { processUserChartData } from '@/features/dashboard/lib'
import type {
  ProcessedUserChartData,
  UserAnalyticsMetric,
} from '@/features/dashboard/types'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const USER_CHARTS: {
  value: string
  labelKey: string
  tokensLabelKey: string
  specKey: keyof ProcessedUserChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'User Consumption Ranking',
    tokensLabelKey: 'User Token Ranking',
    specKey: 'spec_user_rank',
  },
  {
    value: 'trend',
    labelKey: 'User Consumption Trend',
    tokensLabelKey: 'User Token Trend',
    specKey: 'spec_user_trend',
  },
]

export type UserUsageMetric = UserAnalyticsMetric

interface UserChartsProps {
  timeRange: { start_timestamp: number; end_timestamp: number }
  timeGranularity: TimeGranularity
  topUserLimit: number
  userMetric: UserUsageMetric
  onUserSelect: (userId: number) => void
}

export function UserCharts(props: UserChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const { data: userData, isLoading } = useQuery({
    queryKey: ['dashboard', 'user-quota', props.timeRange],
    queryFn: () => getUserQuotaDataByUsers(props.timeRange),
    select: (res) => (res.success ? res.data : []),
    staleTime: 60_000,
  })

  const chartData = useMemo(
    () =>
      processUserChartData(
        isLoading ? [] : (userData ?? []),
        props.timeGranularity,
        t,
        props.topUserLimit,
        customization.preset,
        props.userMetric
      ),
    [
      userData,
      isLoading,
      props.timeGranularity,
      t,
      props.topUserLimit,
      customization.preset,
      props.userMetric,
    ]
  )

  return (
    <div className='flex flex-col gap-3'>
      <div className='grid gap-3'>
        {USER_CHARTS.map((chart) => {
          const spec = chartData[chart.specKey]
          const titleKey =
            props.userMetric === 'tokens'
              ? chart.tokensLabelKey
              : chart.labelKey
          const isRank = chart.value === 'rank'
          const actualCount = isRank
            ? ((spec as { data?: Array<{ values?: unknown[] }> })?.data?.[0]
                ?.values?.length ?? 0)
            : 0
          const rankHeight = actualCount * 32 + 50

          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <HugeiconsIcon
                  icon={UserMultiple02Icon}
                  strokeWidth={2}
                  className='text-muted-foreground/60 size-4'
                />
                <div className='text-sm font-semibold'>{t(titleKey)}</div>
              </div>

              <div
                className='p-1.5 sm:p-2'
                style={
                  isRank && actualCount > 0
                    ? { height: `${rankHeight}px` }
                    : undefined
                }
              >
                {isLoading ? (
                  <Skeleton className='h-full w-full' />
                ) : (
                  themeReady &&
                  spec && (
                    <VChart
                      key={`user-${chart.value}-${props.topUserLimit}-${props.userMetric}-${resolvedTheme}-${customization.preset}`}
                      spec={{
                        ...spec,
                        theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                        background: 'transparent',
                      }}
                      option={VCHART_OPTION}
                      onClick={
                        isRank
                          ? (event: EventParamsDefinition['click']) => {
                              const userId = Number(event.datum?.UserID)
                              if (Number.isInteger(userId) && userId > 0) {
                                props.onUserSelect(userId)
                              }
                            }
                          : undefined
                      }
                    />
                  )
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
