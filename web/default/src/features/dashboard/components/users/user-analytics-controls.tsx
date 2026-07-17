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
import { useMemo, useState } from 'react'
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
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/combobox'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { DatePicker } from '@/components/date-picker'
import { StatusBadge } from '@/components/status-badge'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  USER_ANALYTICS_PRESET_DAYS,
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

interface UserFilterProps {
  selectedPresetDays?: UserAnalyticsPresetDays
  customStartDate?: Date
  customEndDate?: Date
  customRangeError?: string
  timeZoneLabel: string
  selectedUserId?: number
  selectedTarget?: UserModelUsageTarget
  onPresetChange: (days: UserAnalyticsPresetDays) => void
  onCustomStartDateChange: (date: Date | undefined) => void
  onCustomEndDateChange: (date: Date | undefined) => void
  onUserSelect: (user: UserAnalyticsUserOption) => void
}

type UserAnalyticsControlsProps = UserFilterProps &
  (
    | {
        variant?: 'analytics'
        timeGranularity: TimeGranularity
        userMetric: UserUsageMetric
        topUserLimit: number
        onTimeGranularityChange: (granularity: TimeGranularity) => void
        onUserMetricChange: (metric: UserUsageMetric) => void
        onTopUserLimitChange: (limit: number) => void
      }
    | {
        variant: 'usage-summary'
      }
  )

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

export function UserAnalyticsControls(props: UserAnalyticsControlsProps) {
  const { t } = useTranslation()
  const [keyword, setKeyword] = useState('')
  const analyticsProps = props.variant === 'usage-summary' ? undefined : props

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
      return response.data?.items ?? []
    },
  })

  const options = useMemo(() => {
    const users = new Map<number, UserAnalyticsUserOption>()
    if (props.selectedTarget) {
      users.set(props.selectedTarget.id, props.selectedTarget)
    }
    for (const user of searchMutation.data ?? []) {
      users.set(user.id, toUserOption(user))
    }
    return [...users.values()]
  }, [props.selectedTarget, searchMutation.data])

  const selectedOption =
    options.find((user) => user.id === props.selectedUserId) ?? null

  const runSearch = () => {
    searchMutation.mutate(keyword.trim())
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t(
            props.variant === 'usage-summary'
              ? 'User Usage Summary Filters'
              : 'User Analytics Filters'
          )}
        </CardTitle>
        <CardDescription>
          {t(
            props.variant === 'usage-summary'
              ? 'Select one user and time range for model usage totals.'
              : 'Use one time range and metric across user analytics.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup className='grid gap-4 lg:grid-cols-2 xl:grid-cols-3'>
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
            <div className='flex flex-wrap items-center gap-2'>
              <div>
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
              <div>
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
            <FieldDescription>
              {t('Browser timezone: {{timezone}}', {
                timezone: props.timeZoneLabel,
              })}
            </FieldDescription>
            <FieldError>{props.customRangeError}</FieldError>
          </Field>

          {analyticsProps && (
            <>
              <Field>
                <FieldTitle id='user-analytics-granularity'>
                  {t('Chart Granularity')}
                </FieldTitle>
                <ToggleGroup
                  aria-labelledby='user-analytics-granularity'
                  variant='outline'
                  size='sm'
                  value={[analyticsProps.timeGranularity]}
                  onValueChange={(values) => {
                    const value = values[0] as TimeGranularity | undefined
                    if (value) {
                      analyticsProps.onTimeGranularityChange(value)
                    }
                  }}
                >
                  {TIME_GRANULARITY_OPTIONS.map((option) => (
                    <ToggleGroupItem key={option.value} value={option.value}>
                      {t(option.label)}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </Field>

              <Field>
                <FieldTitle id='user-analytics-metric'>
                  {t('Metric')}
                </FieldTitle>
                <ToggleGroup
                  aria-labelledby='user-analytics-metric'
                  variant='outline'
                  size='sm'
                  value={[analyticsProps.userMetric]}
                  onValueChange={(values) => {
                    const value = values[0] as UserUsageMetric | undefined
                    if (value) analyticsProps.onUserMetricChange(value)
                  }}
                >
                  {USER_METRIC_OPTIONS.map((option) => (
                    <ToggleGroupItem key={option.value} value={option.value}>
                      {t(option.labelKey)}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </Field>

              <Field>
                <FieldTitle id='user-analytics-top-users'>
                  {t('Top Users')}
                </FieldTitle>
                <ToggleGroup
                  aria-labelledby='user-analytics-top-users'
                  variant='outline'
                  size='sm'
                  value={[String(analyticsProps.topUserLimit)]}
                  onValueChange={(values) => {
                    const value = Number(values[0])
                    if (
                      TOP_USER_LIMIT_OPTIONS.includes(
                        value as (typeof TOP_USER_LIMIT_OPTIONS)[number]
                      )
                    ) {
                      analyticsProps.onTopUserLimitChange(value)
                    }
                  }}
                >
                  {TOP_USER_LIMIT_OPTIONS.map((limit) => (
                    <ToggleGroupItem key={limit} value={String(limit)}>
                      {t('Top {{count}}', { count: limit })}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </Field>
            </>
          )}

          <Field data-invalid={searchMutation.isError}>
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
          </Field>

          <Field>
            <FieldLabel htmlFor='user-analytics-selected-user'>
              {t('Selected User')}
            </FieldLabel>
            <Combobox
              items={options}
              value={selectedOption}
              onValueChange={(user) => {
                if (user) props.onUserSelect(user)
              }}
              itemToStringValue={getUserLabel}
            >
              <ComboboxInput
                id='user-analytics-selected-user'
                aria-label={t('Selected User')}
                placeholder={t('Search to select a user')}
                disabled={options.length === 0}
                showClear={false}
              />
              <ComboboxContent>
                <ComboboxEmpty>{t('No users found')}</ComboboxEmpty>
                <ComboboxList>
                  {(user: UserAnalyticsUserOption) => {
                    const status = user.deleted
                      ? USER_STATUSES.DELETED
                      : user.status === USER_STATUS.ENABLED
                        ? USER_STATUSES[USER_STATUS.ENABLED]
                        : USER_STATUSES[USER_STATUS.DISABLED]

                    return (
                      <ComboboxItem key={user.id} value={user}>
                        <span className='min-w-0 flex-1 truncate'>
                          {getUserLabel(user)}
                        </span>
                        <StatusBadge
                          label={t(status.labelKey)}
                          variant={status.variant}
                          copyable={false}
                        />
                      </ComboboxItem>
                    )
                  }}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
            {!props.selectedUserId && (
              <FieldDescription>
                {t('No user is selected by default.')}
              </FieldDescription>
            )}
          </Field>
        </FieldGroup>
      </CardContent>
    </Card>
  )
}
