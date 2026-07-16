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
import z from 'zod'
import type {
  UserAnalyticsMetric,
  UserAnalyticsSearch,
  UserModelUsageSortField,
} from '../types'

export const USER_ANALYTICS_PRESET_DAYS = [1, 7, 14, 30, 90] as const

export type UserAnalyticsPresetDays =
  (typeof USER_ANALYTICS_PRESET_DAYS)[number]

export const MAX_USER_ANALYTICS_RANGE_SECONDS = 90 * 24 * 60 * 60

export interface UserAnalyticsRange {
  start_timestamp: number
  end_timestamp: number
}

const positiveInteger = z.number().int().positive()
const optionalPositiveInteger = positiveInteger.optional().catch(undefined)
const optionalNonEmptyString = z
  .string()
  .trim()
  .min(1)
  .optional()
  .catch(undefined)
const optionalSources = z
  .string()
  .transform((value) => encodeUsageSources(decodeUsageSources(value)))
  .pipe(z.string().min(1))
  .optional()
  .catch(undefined)

const rawUserAnalyticsSearchSchema = z.object({
  user_id: optionalPositiveInteger,
  start_timestamp: optionalPositiveInteger,
  end_timestamp: optionalPositiveInteger,
  metric: z.enum(['quota', 'tokens']).optional().catch(undefined),
  sort_mode: z.enum(['metric', 'manual']).optional().catch(undefined),
  sources: optionalSources,
  model_search: optionalNonEmptyString,
  sort_by: z
    .enum([
      'model_name',
      'token_usage',
      'quota',
      'gateway_requests',
      'external_events',
    ])
    .optional()
    .catch(undefined),
  sort_order: z.enum(['asc', 'desc']).optional().catch(undefined),
  p: optionalPositiveInteger,
  page_size: z
    .union([z.literal(20), z.literal(50), z.literal(100)])
    .optional()
    .catch(undefined),
})

export const userAnalyticsSearchSchema = rawUserAnalyticsSearchSchema.transform(
  (search): UserAnalyticsSearch =>
    Object.fromEntries(
      Object.entries(search).filter(([, value]) => value !== undefined)
    ) as UserAnalyticsSearch
)

export function decodeUsageSources(sources?: string): string[] {
  if (!sources) return []

  const seen = new Set<string>()
  for (const item of sources.split(',')) {
    const source = item.trim().toLowerCase()
    if (source) seen.add(source)
  }
  return [...seen]
}

export function encodeUsageSources(sources: readonly string[]): string {
  return decodeUsageSources(sources.join(',')).join(',')
}

function isValidTimestampRange(
  range: Partial<UserAnalyticsRange>
): range is UserAnalyticsRange {
  const { start_timestamp: start, end_timestamp: end } = range
  return (
    Number.isInteger(start) &&
    Number.isInteger(end) &&
    start != null &&
    end != null &&
    start > 0 &&
    end >= start &&
    end - start <= MAX_USER_ANALYTICS_RANGE_SECONDS
  )
}

export function resolveUserAnalyticsRange(
  search: Pick<UserAnalyticsSearch, 'start_timestamp' | 'end_timestamp'>,
  fallback: UserAnalyticsRange
): UserAnalyticsRange {
  if (!isValidTimestampRange(fallback)) {
    throw new RangeError('Fallback range must be valid and at most 90 days')
  }
  return isValidTimestampRange(search) ? { ...search } : { ...fallback }
}

function toUnixSeconds(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

function assertValidDate(date: Date, field: string): void {
  if (Number.isNaN(date.getTime())) {
    throw new RangeError(`${field} must be a valid date`)
  }
}

export function createBrowserLocalDayRange(
  startDate: Date,
  endDate: Date
): UserAnalyticsRange {
  assertValidDate(startDate, 'startDate')
  assertValidDate(endDate, 'endDate')

  const range = {
    start_timestamp: toUnixSeconds(
      new Date(
        startDate.getFullYear(),
        startDate.getMonth(),
        startDate.getDate(),
        0,
        0,
        0,
        0
      )
    ),
    end_timestamp: toUnixSeconds(
      new Date(
        endDate.getFullYear(),
        endDate.getMonth(),
        endDate.getDate(),
        23,
        59,
        59,
        999
      )
    ),
  }

  if (range.end_timestamp < range.start_timestamp) {
    throw new RangeError('End date must not precede start date')
  }
  if (
    range.end_timestamp - range.start_timestamp >
    MAX_USER_ANALYTICS_RANGE_SECONDS
  ) {
    throw new RangeError('Custom range must not exceed 90 days')
  }
  return range
}

export function createUserAnalyticsPresetRange(
  days: UserAnalyticsPresetDays,
  now = new Date()
): UserAnalyticsRange {
  assertValidDate(now, 'now')
  const endTimestamp = toUnixSeconds(now)
  return {
    start_timestamp: endTimestamp - days * 24 * 60 * 60,
    end_timestamp: endTimestamp,
  }
}

export function getBrowserTimeZoneLabel(
  date = new Date(),
  timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
): string {
  assertValidDate(date, 'date')
  try {
    const label = new Intl.DateTimeFormat('en-US', {
      timeZone,
      timeZoneName: 'short',
    })
      .formatToParts(date)
      .find((part) => part.type === 'timeZoneName')?.value
    return label || timeZone
  } catch {
    return timeZone
  }
}

function getMetricSortField(
  metric: UserAnalyticsMetric
): UserModelUsageSortField {
  return metric === 'quota' ? 'quota' : 'token_usage'
}

export function initializeUserAnalyticsSearch(
  current: UserAnalyticsSearch,
  fallbackRange: UserAnalyticsRange
): UserAnalyticsSearch {
  const range = resolveUserAnalyticsRange(current, fallbackRange)
  const metric = current.metric ?? 'quota'
  const hasExplicitSort = current.sort_by != null || current.sort_order != null
  const sortMode = current.sort_mode ?? (hasExplicitSort ? 'manual' : 'metric')
  const metricSortField = getMetricSortField(metric)

  return {
    ...current,
    ...range,
    metric,
    sort_mode: sortMode,
    sort_by:
      sortMode === 'metric'
        ? metricSortField
        : (current.sort_by ?? metricSortField),
    sort_order: sortMode === 'metric' ? 'desc' : (current.sort_order ?? 'desc'),
    p: current.p ?? 1,
    page_size: current.page_size ?? 20,
  }
}

const pageResetKeys = [
  'user_id',
  'start_timestamp',
  'end_timestamp',
  'metric',
  'sort_mode',
  'sources',
  'model_search',
  'sort_by',
  'sort_order',
] as const satisfies readonly (keyof UserAnalyticsSearch)[]

const owns = (value: object, key: PropertyKey) =>
  Object.prototype.hasOwnProperty.call(value, key)

export function patchUserAnalyticsSearch(
  current: UserAnalyticsSearch,
  patch: Partial<UserAnalyticsSearch>
): UserAnalyticsSearch {
  const normalizedPatch = { ...patch }
  if (owns(patch, 'sources')) {
    const sources = encodeUsageSources(decodeUsageSources(patch.sources))
    normalizedPatch.sources = sources || undefined
  }

  const resetPage = pageResetKeys.some(
    (key) => owns(normalizedPatch, key) && normalizedPatch[key] !== current[key]
  )
  const next = { ...current, ...normalizedPatch }
  if (resetPage) next.p = 1
  return next
}

export function changeUserAnalyticsUser(
  current: UserAnalyticsSearch,
  userId: number
): UserAnalyticsSearch {
  if (current.user_id === userId) return current

  return patchUserAnalyticsSearch(current, {
    user_id: userId,
    sources: undefined,
    model_search: undefined,
  })
}

export function changeUserAnalyticsMetric(
  current: UserAnalyticsSearch,
  metric: UserAnalyticsMetric
): UserAnalyticsSearch {
  if (current.sort_mode === 'manual') {
    return { ...current, metric }
  }

  return patchUserAnalyticsSearch(current, {
    metric,
    sort_mode: 'metric',
    sort_by: getMetricSortField(metric),
    sort_order: 'desc',
  })
}
