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
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Search01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import type { TimeGranularity } from '@/lib/time'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { DatePicker } from '@/components/date-picker'
import { StatusBadge } from '@/components/status-badge'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  USER_ANALYTICS_PRESET_DAYS,
  selectUserSearchResult,
  type UserAnalyticsPresetDays,
} from '@/features/dashboard/lib'
import type { UserModelUsageTarget } from '@/features/dashboard/types'
import { searchUsers } from '@/features/users/api'
import {
  USER_STATUS,
  USER_STATUSES,
  isUserDeleted,
} from '@/features/users/constants'
import type { User } from '@/features/users/types'
import type { UserUsageMetric } from './user-charts'

const TOP_USER_LIMIT_OPTIONS = [5, 10, 20, 50] as const

const USER_METRIC_OPTIONS: Array<{
  value: UserUsageMetric
  labelKey: string
}> = [
  { value: 'quota', labelKey: 'Quota' },
  { value: 'tokens', labelKey: 'Tokens' },
]

export type UserAnalyticsUserOption = UserModelUsageTarget

interface UserAnalyticsCompactControlsProps {
  variant: 'analytics'
  selectedPresetDays?: UserAnalyticsPresetDays
  timeGranularity: TimeGranularity
  userMetric: UserUsageMetric
  topUserLimit: number
  onPresetChange: (days: UserAnalyticsPresetDays) => void
  onTimeGranularityChange: (granularity: TimeGranularity) => void
  onUserMetricChange: (metric: UserUsageMetric) => void
  onTopUserLimitChange: (limit: number) => void
}

interface UserUsageSummaryControlsProps {
  variant: 'usage-summary'
  selectedPresetDays?: UserAnalyticsPresetDays
  customStartDate?: Date
  customEndDate?: Date
  customRangeError?: string
  selectedTarget?: UserModelUsageTarget
  onPresetChange: (days: UserAnalyticsPresetDays) => void
  onCustomStartDateChange: (date: Date | undefined) => void
  onCustomEndDateChange: (date: Date | undefined) => void
  onUserSelect: (user: UserAnalyticsUserOption) => void
}

type UserAnalyticsControlsProps =
  | UserAnalyticsCompactControlsProps
  | UserUsageSummaryControlsProps

function toUserOption(user: User): UserAnalyticsUserOption {
  return {
    id: user.id,
    username: user.username,
    display_name: user.display_name,
    status: user.status,
    deleted: isUserDeleted(user),
  }
}

function getUserLabel(user: UserAnalyticsUserOption): string {
  if (user.display_name && user.display_name !== user.username) {
    return `${user.display_name} (@${user.username})`
  }
  return user.username
}

function getUserStatus(user: UserAnalyticsUserOption) {
  if (user.deleted) return USER_STATUSES.DELETED
  if (user.status === USER_STATUS.ENABLED) {
    return USER_STATUSES[USER_STATUS.ENABLED]
  }
  return USER_STATUSES[USER_STATUS.DISABLED]
}

function UserAnalyticsCompactControls(
  props: UserAnalyticsCompactControlsProps
) {
  const { t } = useTranslation()

  return (
    <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
      <Tabs
        value={props.selectedPresetDays ? String(props.selectedPresetDays) : ''}
        onValueChange={(value) => {
          const days = Number(value)
          if (
            USER_ANALYTICS_PRESET_DAYS.includes(days as UserAnalyticsPresetDays)
          ) {
            props.onPresetChange(days as UserAnalyticsPresetDays)
          }
        }}
        className='shrink-0'
      >
        <TabsList>
          {TIME_RANGE_PRESETS.map((preset) => (
            <TabsTrigger
              key={preset.days}
              value={String(preset.days)}
              className='px-2.5 text-xs'
            >
              {t(preset.label)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <Tabs
        value={props.timeGranularity}
        onValueChange={(value) =>
          props.onTimeGranularityChange(value as TimeGranularity)
        }
        className='shrink-0'
      >
        <TabsList>
          {TIME_GRANULARITY_OPTIONS.map((option) => (
            <TabsTrigger
              key={option.value}
              value={option.value}
              className='px-2.5 text-xs'
            >
              {t(option.label)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className='flex shrink-0 items-center gap-1.5 rounded-lg border p-0.5'>
        {USER_METRIC_OPTIONS.map((option) => (
          <button
            key={option.value}
            type='button'
            onClick={() => props.onUserMetricChange(option.value)}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              props.userMetric === option.value
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            }`}
          >
            {t(option.labelKey)}
          </button>
        ))}
      </div>

      <Tabs
        value={String(props.topUserLimit)}
        onValueChange={(value) => {
          const limit = Number(value)
          if (
            TOP_USER_LIMIT_OPTIONS.includes(
              limit as (typeof TOP_USER_LIMIT_OPTIONS)[number]
            )
          ) {
            props.onTopUserLimitChange(limit)
          }
        }}
        className='shrink-0'
      >
        <TabsList>
          <span className='text-muted-foreground px-2 text-xs font-medium whitespace-nowrap'>
            {t('Top Users')}
          </span>
          {TOP_USER_LIMIT_OPTIONS.map((limit) => (
            <TabsTrigger
              key={limit}
              value={String(limit)}
              className='px-2.5 text-xs'
            >
              {t('Top {{count}}', { count: limit })}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    </div>
  )
}

function UserUsageSummaryControls(props: UserUsageSummaryControlsProps) {
  const { t } = useTranslation()
  const [keyword, setKeyword] = useState('')

  const searchMutation = useMutation({
    mutationFn: async (value: string) => {
      const response = await searchUsers({
        keyword: value,
        p: 1,
        page_size: 20,
      })
      if (!response.success) {
        throw new Error(response.message || 'Failed to search users')
      }
      return (response.data?.items ?? []).map(toUserOption)
    },
  })

  const runSearch = () => {
    const searchKeyword = keyword.trim()
    searchMutation.mutate(searchKeyword, {
      onSuccess: (users) => {
        const selectedUser = selectUserSearchResult(users, searchKeyword)
        if (selectedUser) props.onUserSelect(selectedUser)
      },
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('User Usage Summary Filters')}</CardTitle>
        <CardDescription>
          {t('Select one user and time range for model usage totals.')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup className='grid gap-4 lg:grid-cols-2'>
          <Field>
            <FieldTitle id='user-analytics-range'>{t('Time Range')}</FieldTitle>
            <div className='overflow-x-auto pb-1'>
              <ToggleGroup
                aria-labelledby='user-analytics-range'
                variant='outline'
                size='sm'
                value={
                  props.selectedPresetDays
                    ? [String(props.selectedPresetDays)]
                    : []
                }
                onValueChange={(values) => {
                  const days = Number(values[0])
                  if (
                    USER_ANALYTICS_PRESET_DAYS.includes(
                      days as UserAnalyticsPresetDays
                    )
                  ) {
                    props.onPresetChange(days as UserAnalyticsPresetDays)
                  }
                }}
              >
                {TIME_RANGE_PRESETS.map((preset) => (
                  <ToggleGroupItem
                    key={preset.days}
                    value={String(preset.days)}
                  >
                    {t(preset.label)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          </Field>

          <Field data-invalid={Boolean(props.customRangeError)}>
            <FieldTitle>{t('Custom Date Range')}</FieldTitle>
            <div className='grid grid-cols-2 gap-2'>
              <div className='min-w-0'>
                <FieldLabel
                  htmlFor='user-analytics-start-date'
                  className='sr-only'
                >
                  {t('Start date')}
                </FieldLabel>
                <DatePicker
                  id='user-analytics-start-date'
                  ariaLabel={t('Start date')}
                  selected={props.customStartDate}
                  onSelect={props.onCustomStartDateChange}
                  placeholder={t('Start date')}
                />
              </div>
              <div className='min-w-0'>
                <FieldLabel
                  htmlFor='user-analytics-end-date'
                  className='sr-only'
                >
                  {t('End date')}
                </FieldLabel>
                <DatePicker
                  id='user-analytics-end-date'
                  ariaLabel={t('End date')}
                  selected={props.customEndDate}
                  onSelect={props.onCustomEndDateChange}
                  placeholder={t('End date')}
                />
              </div>
            </div>
            <FieldError>{props.customRangeError}</FieldError>
          </Field>

          <Field
            className='lg:col-span-2'
            data-invalid={searchMutation.isError}
          >
            <FieldLabel htmlFor='user-analytics-user-search'>
              {t('Find User')}
            </FieldLabel>
            <InputGroup>
              <InputGroupInput
                id='user-analytics-user-search'
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    event.preventDefault()
                    runSearch()
                  }
                }}
                placeholder={t('Search username or display name')}
                aria-invalid={searchMutation.isError}
              />
              <InputGroupAddon align='inline-end'>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  disabled={searchMutation.isPending}
                  onClick={runSearch}
                >
                  {searchMutation.isPending ? (
                    <Spinner data-icon='inline-start' />
                  ) : (
                    <HugeiconsIcon
                      icon={Search01Icon}
                      strokeWidth={2}
                      data-icon='inline-start'
                    />
                  )}
                  {t('Search')}
                </Button>
              </InputGroupAddon>
            </InputGroup>
            <FieldError>
              {searchMutation.isError ? t('Failed to search users') : undefined}
            </FieldError>
            {searchMutation.isSuccess && searchMutation.data.length === 0 && (
              <FieldDescription>{t('No users found')}</FieldDescription>
            )}
            <div className='mt-2 space-y-2'>
              <FieldTitle>{t('Selected User')}</FieldTitle>
              {props.selectedTarget ? (
                <div className='flex min-h-9 items-center justify-between gap-3 rounded-md border px-3 py-2'>
                  <span className='min-w-0 flex-1 truncate text-sm'>
                    {getUserLabel(props.selectedTarget)}
                  </span>
                  <StatusBadge
                    label={t(getUserStatus(props.selectedTarget).labelKey)}
                    variant={getUserStatus(props.selectedTarget).variant}
                    copyable={false}
                  />
                </div>
              ) : (
                <FieldDescription>
                  {t('No user is selected by default.')}
                </FieldDescription>
              )}
            </div>
          </Field>
        </FieldGroup>
      </CardContent>
    </Card>
  )
}

export function UserAnalyticsControls(props: UserAnalyticsControlsProps) {
  if (props.variant === 'analytics') {
    return <UserAnalyticsCompactControls {...props} />
  }

  return <UserUsageSummaryControls {...props} />
}
