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
import {
  changeUserAnalyticsUser,
  createBrowserLocalDayRange,
  createUserAnalyticsPresetRange,
  initializeUserUsageSummarySearch,
  patchUserAnalyticsSearch,
  resolveUserAnalyticsRange,
  USER_ANALYTICS_PRESET_DAYS,
  type UserAnalyticsPresetDays,
} from '@/features/dashboard/lib'
import type {
  UserAnalyticsSearch,
  UserModelUsageTarget,
} from '@/features/dashboard/types'
import {
  UserAnalyticsControls,
  type UserAnalyticsUserOption,
} from './user-analytics-controls'
import { UserModelUsageSummary } from './user-model-usage-summary'

const route = getRouteApi('/_authenticated/dashboard/$section')

export function UserUsageSummary() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [initialRange] = useState(() => createUserAnalyticsPresetRange(7))
  const timeRange = resolveUserAnalyticsRange(search, initialRange)
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
    const initializedSearch = initializeUserUsageSummarySearch(
      search,
      initialRange
    )
    const needsInitialization = Object.entries(initializedSearch).some(
      ([key, value]) => search[key as keyof UserAnalyticsSearch] !== value
    )

    if (!needsInitialization) return

    updateSearch(initializedSearch)
  }, [initialRange, search, updateSearch])

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
      setCustomDraft(undefined)
      updateSearch(createUserAnalyticsPresetRange(days))
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

  const handleUserSelect = useCallback(
    (user: UserAnalyticsUserOption) => {
      setSelectedTarget(user)
      void navigate({
        replace: true,
        search: (current) => changeUserAnalyticsUser(current, user.id),
      })
    },
    [navigate]
  )

  const handleUserMetadata = useCallback((target: UserModelUsageTarget) => {
    setSelectedTarget(target)
  }, [])

  return (
    <div className='flex flex-col gap-3'>
      <UserAnalyticsControls
        variant='usage-summary'
        selectedPresetDays={
          customDraft?.rangeKey === rangeKey ? undefined : selectedPresetDays
        }
        customStartDate={activeCustomDraft.startDate}
        customEndDate={activeCustomDraft.endDate}
        customRangeError={activeCustomDraft.error}
        selectedTarget={activeSelectedTarget}
        onPresetChange={handlePresetChange}
        onCustomStartDateChange={handleCustomStartDateChange}
        onCustomEndDateChange={handleCustomEndDateChange}
        onUserSelect={handleUserSelect}
      />
      <UserModelUsageSummary
        userId={search.user_id}
        timeRange={timeRange}
        search={search}
        onSearchChange={updateSearch}
        onUserMetadata={handleUserMetadata}
      />
    </div>
  )
}
