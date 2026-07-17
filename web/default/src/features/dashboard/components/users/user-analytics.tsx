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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import type { TimeGranularity } from '@/lib/time'
import {
  changeUserAnalyticsMetric,
  createUserAnalyticsPresetRange,
  getDefaultDays,
  getSavedGranularity,
  initializeUserAnalyticsSearch,
  patchUserAnalyticsSearch,
  resolveUserAnalyticsRange,
  saveGranularity,
  USER_ANALYTICS_PRESET_DAYS,
  type UserAnalyticsPresetDays,
  type UserAnalyticsRange,
} from '@/features/dashboard/lib'
import type { UserAnalyticsSearch } from '@/features/dashboard/types'
import { UserAnalyticsControls } from './user-analytics-controls'
import { UserCharts, type UserUsageMetric } from './user-charts'

const route = getRouteApi('/_authenticated/dashboard/$section')

export function UserAnalytics() {
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [initialState] = useState<{
    granularity: TimeGranularity
    range: UserAnalyticsRange
  }>(() => {
    const granularity = getSavedGranularity()
    const days = getDefaultDays(granularity) as UserAnalyticsPresetDays
    return {
      granularity,
      range: createUserAnalyticsPresetRange(days),
    }
  })

  const timeRange = resolveUserAnalyticsRange(search, initialState.range)
  const [timeGranularity, setTimeGranularity] = useState<TimeGranularity>(
    initialState.granularity
  )
  const userMetric: UserUsageMetric = search.metric ?? 'quota'
  const [topUserLimit, setTopUserLimit] = useState(10)

  const updateSearch = useCallback(
    (patch: Partial<UserAnalyticsSearch>) => {
      void navigate({
        replace: true,
        search: (current) => patchUserAnalyticsSearch(current, patch),
      })
    },
    [navigate]
  )

  useEffect(() => {
    const initializedSearch = initializeUserAnalyticsSearch(
      search,
      initialState.range
    )
    const needsInitialization = Object.entries(initializedSearch).some(
      ([key, value]) => search[key as keyof UserAnalyticsSearch] !== value
    )

    if (!needsInitialization) return

    updateSearch(initializedSearch)
  }, [initialState.range, search, updateSearch])

  const selectedPresetDays = useMemo(() => {
    const seconds = timeRange.end_timestamp - timeRange.start_timestamp
    return USER_ANALYTICS_PRESET_DAYS.find(
      (days) => seconds === days * 24 * 60 * 60
    )
  }, [timeRange.end_timestamp, timeRange.start_timestamp])

  const handlePresetChange = useCallback(
    (days: UserAnalyticsPresetDays) => {
      const range = createUserAnalyticsPresetRange(days)
      updateSearch(range)
    },
    [updateSearch]
  )

  const handleGranularityChange = useCallback(
    (granularity: TimeGranularity) => {
      setTimeGranularity(granularity)
      saveGranularity(granularity)
    },
    []
  )

  const handleMetricChange = useCallback(
    (metric: UserUsageMetric) => {
      void navigate({
        replace: true,
        search: (current) =>
          changeUserAnalyticsMetric(
            initializeUserAnalyticsSearch(current, initialState.range),
            metric
          ),
      })
    },
    [initialState.range, navigate]
  )

  return (
    <div className='flex flex-col gap-3'>
      <UserAnalyticsControls
        variant='analytics'
        selectedPresetDays={selectedPresetDays}
        timeGranularity={timeGranularity}
        userMetric={userMetric}
        topUserLimit={topUserLimit}
        onPresetChange={handlePresetChange}
        onTimeGranularityChange={handleGranularityChange}
        onUserMetricChange={handleMetricChange}
        onTopUserLimitChange={setTopUserLimit}
      />
      <UserCharts
        timeRange={timeRange}
        timeGranularity={timeGranularity}
        topUserLimit={topUserLimit}
        userMetric={userMetric}
      />
    </div>
  )
}
