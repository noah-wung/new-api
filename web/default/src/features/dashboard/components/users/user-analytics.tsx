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
import { useTranslation } from 'react-i18next'
import type { TimeGranularity } from '@/lib/time'
import {
  changeUserAnalyticsMetric,
  changeUserAnalyticsUser,
  createBrowserLocalDayRange,
  createUserAnalyticsPresetRange,
  getBrowserTimeZoneLabel,
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
import type {
  UserAnalyticsSearch,
  UserModelUsageTarget,
} from '@/features/dashboard/types'
import {
  UserAnalyticsControls,
  type UserAnalyticsUserOption,
} from './user-analytics-controls'
import { UserCharts, type UserUsageMetric } from './user-charts'

const route = getRouteApi('/_authenticated/dashboard/$section')

export function UserAnalytics() {
  const { t } = useTranslation()
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
  const [selectedTarget, setSelectedTarget] = useState<UserModelUsageTarget>()
  const rangeKey = `${timeRange.start_timestamp}:${timeRange.end_timestamp}`
  const [customDraft, setCustomDraft] = useState<{
    rangeKey: string
    startDate?: Date
    endDate?: Date
    error?: string
  }>()
  const activeCustomDraft =
    customDraft?.rangeKey === rangeKey
      ? customDraft
      : {
          rangeKey,
          startDate: new Date(timeRange.start_timestamp * 1000),
          endDate: new Date(timeRange.end_timestamp * 1000),
          error: undefined,
        }
  const [timeZoneLabel] = useState(() => getBrowserTimeZoneLabel())

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

  const activeSelectedTarget =
    selectedTarget?.id === search.user_id ? selectedTarget : undefined

  const selectedPresetDays = useMemo(() => {
    const seconds = timeRange.end_timestamp - timeRange.start_timestamp
    return USER_ANALYTICS_PRESET_DAYS.find(
      (days) => seconds === days * 24 * 60 * 60
    )
  }, [timeRange.end_timestamp, timeRange.start_timestamp])

  const applyCustomRange = useCallback(
    (
      startDate: Date | undefined,
      endDate: Date | undefined,
      draft: { rangeKey: string; startDate?: Date; endDate?: Date }
    ) => {
      if (!startDate || !endDate) {
        setCustomDraft(draft)
        return
      }

      try {
        const range = createBrowserLocalDayRange(startDate, endDate)
        setCustomDraft(draft)
        updateSearch(range)
      } catch {
        setCustomDraft({
          ...draft,
          error: t(
            'Select an end date on or after the start date, within 90 days.'
          ),
        })
      }
    },
    [t, updateSearch]
  )

  const handlePresetChange = useCallback(
    (days: UserAnalyticsPresetDays) => {
      const range = createUserAnalyticsPresetRange(days)
      setCustomDraft(undefined)
      updateSearch(range)
    },
    [updateSearch]
  )

  const handleCustomStartDateChange = useCallback(
    (date: Date | undefined) => {
      applyCustomRange(date, activeCustomDraft.endDate, {
        rangeKey,
        startDate: date,
        endDate: activeCustomDraft.endDate,
      })
    },
    [activeCustomDraft.endDate, applyCustomRange, rangeKey]
  )

  const handleCustomEndDateChange = useCallback(
    (date: Date | undefined) => {
      applyCustomRange(activeCustomDraft.startDate, date, {
        rangeKey,
        startDate: activeCustomDraft.startDate,
        endDate: date,
      })
    },
    [activeCustomDraft.startDate, applyCustomRange, rangeKey]
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

  const handleUserSelect = useCallback(
    (userId: number, target?: UserModelUsageTarget) => {
      setSelectedTarget(
        (current) => target ?? (current?.id === userId ? current : undefined)
      )
      void navigate({
        replace: true,
        search: (current) => changeUserAnalyticsUser(current, userId),
      })
    },
    [navigate]
  )

  const handleSearchUserSelect = useCallback(
    (user: UserAnalyticsUserOption) => {
      handleUserSelect(user.id, user)
    },
    [handleUserSelect]
  )

  return (
    <div className='flex flex-col gap-3'>
      <UserAnalyticsControls
        selectedPresetDays={
          customDraft?.rangeKey === rangeKey ? undefined : selectedPresetDays
        }
        customStartDate={activeCustomDraft.startDate}
        customEndDate={activeCustomDraft.endDate}
        customRangeError={activeCustomDraft.error}
        timeZoneLabel={timeZoneLabel}
        timeGranularity={timeGranularity}
        userMetric={userMetric}
        topUserLimit={topUserLimit}
        selectedUserId={search.user_id}
        selectedTarget={activeSelectedTarget}
        onPresetChange={handlePresetChange}
        onCustomStartDateChange={handleCustomStartDateChange}
        onCustomEndDateChange={handleCustomEndDateChange}
        onTimeGranularityChange={handleGranularityChange}
        onUserMetricChange={handleMetricChange}
        onTopUserLimitChange={setTopUserLimit}
        onUserSelect={handleSearchUserSelect}
      />
      <UserCharts
        timeRange={timeRange}
        timeGranularity={timeGranularity}
        topUserLimit={topUserLimit}
        userMetric={userMetric}
        onUserSelect={handleUserSelect}
      />
    </div>
  )
}
