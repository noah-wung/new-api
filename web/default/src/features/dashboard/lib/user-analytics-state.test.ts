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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import * as userAnalyticsState from './user-analytics-state'
import {
  changeUserAnalyticsMetric,
  changeUserAnalyticsUser,
  createBrowserLocalDayRange,
  createUserAnalyticsPresetRange,
  decodeUsageSources,
  encodeUsageSources,
  getBrowserTimeZoneLabel,
  initializeUserAnalyticsSearch,
  patchUserAnalyticsSearch,
  resolveUserAnalyticsRange,
  userAnalyticsSearchSchema,
} from './user-analytics-state'

const seconds = (date: Date) => Math.floor(date.getTime() / 1000)

describe('user analytics URL state', () => {
  test('normalizes concrete sources', () => {
    assert.deepEqual(
      decodeUsageSources('Gateway,codex,codex,, cursor ,GATEWAY'),
      ['gateway', 'codex', 'cursor']
    )
    assert.equal(
      encodeUsageSources([' Codex ', 'gateway', 'CODEX', '']),
      'codex,gateway'
    )
  })

  test('uses complete valid URL timestamps', () => {
    assert.deepEqual(
      resolveUserAnalyticsRange(
        { start_timestamp: 100, end_timestamp: 200 },
        { start_timestamp: 1, end_timestamp: 2 }
      ),
      { start_timestamp: 100, end_timestamp: 200 }
    )
  })

  test('falls back when URL timestamps are incomplete or invalid', () => {
    const fallback = { start_timestamp: 100, end_timestamp: 200 }

    assert.deepEqual(
      resolveUserAnalyticsRange({ start_timestamp: 1 }, fallback),
      fallback
    )
    assert.deepEqual(
      resolveUserAnalyticsRange(
        { start_timestamp: 200, end_timestamp: 100 },
        fallback
      ),
      fallback
    )
    assert.deepEqual(
      resolveUserAnalyticsRange(
        { start_timestamp: 1, end_timestamp: 90 * 24 * 60 * 60 + 2 },
        fallback
      ),
      fallback
    )
  })

  test('creates every supported preset as a rolling duration ending at now', () => {
    const now = new Date(2026, 6, 16, 13, 14, 15, 678)
    const endTimestamp = seconds(now)

    for (const days of [1, 7, 14, 30, 90] as const) {
      assert.deepEqual(createUserAnalyticsPresetRange(days, now), {
        start_timestamp: endTimestamp - days * 24 * 60 * 60,
        end_timestamp: endTimestamp,
      })
    }
  })

  test('keeps the 90-day preset exact after a daylight-saving fallback', () => {
    const now = new Date(2026, 10, 15, 12, 34, 56, 789)
    const range = createUserAnalyticsPresetRange(90, now)

    assert.equal(range.end_timestamp, seconds(now))
    assert.equal(range.end_timestamp - range.start_timestamp, 90 * 24 * 60 * 60)
  })

  test('converts custom dates to browser-local start and end of day', () => {
    assert.deepEqual(
      createBrowserLocalDayRange(
        new Date(2026, 6, 2, 18, 30),
        new Date(2026, 6, 4, 7, 15)
      ),
      {
        start_timestamp: seconds(new Date(2026, 6, 2, 0, 0, 0, 0)),
        end_timestamp: seconds(new Date(2026, 6, 4, 23, 59, 59, 999)),
      }
    )
  })

  test('rejects a custom range longer than 90 days', () => {
    assert.throws(
      () =>
        createBrowserLocalDayRange(new Date(2026, 0, 1), new Date(2026, 3, 1)),
      /90 days/
    )
  })

  test('formats an injectable browser timezone label', () => {
    assert.equal(
      getBrowserTimeZoneLabel(new Date('2026-01-15T12:00:00Z'), 'UTC'),
      'UTC'
    )
  })

  test('resets the page when a result filter changes', () => {
    assert.deepEqual(
      patchUserAnalyticsSearch(
        { user_id: 7, p: 4, page_size: 50 },
        { model_search: 'gpt' }
      ),
      { user_id: 7, p: 1, page_size: 50, model_search: 'gpt' }
    )

    const initial = {
      user_id: 7,
      start_timestamp: 100,
      end_timestamp: 200,
      sources: 'gateway',
      model_search: 'gpt',
      sort_by: 'quota' as const,
      sort_order: 'desc' as const,
      p: 4,
      page_size: 50 as const,
    }

    for (const patch of [
      { user_id: 8 },
      { start_timestamp: 101 },
      { end_timestamp: 201 },
      { metric: 'tokens' as const },
      { sort_mode: 'manual' as const },
      { sources: 'codex' },
      { model_search: 'claude' },
      { sort_by: 'model_name' as const },
      { sort_order: 'asc' as const },
    ]) {
      assert.equal(patchUserAnalyticsSearch(initial, patch).p, 1)
    }
  })

  test('preserves explicit page changes and the current page size', () => {
    assert.deepEqual(
      patchUserAnalyticsSearch({ user_id: 7, p: 4, page_size: 50 }, { p: 3 }),
      { user_id: 7, p: 3, page_size: 50 }
    )
    assert.deepEqual(
      patchUserAnalyticsSearch(
        { user_id: 7, p: 4, page_size: 50 },
        { page_size: 100 }
      ),
      { user_id: 7, p: 4, page_size: 100 }
    )
  })

  test('normalizes source patches before comparing filters', () => {
    assert.deepEqual(
      patchUserAnalyticsSearch(
        { user_id: 7, sources: 'gateway,codex', p: 3 },
        { sources: ' GATEWAY, codex, gateway ' }
      ),
      { user_id: 7, sources: 'gateway,codex', p: 3 }
    )
  })

  test('drops invalid optional URL values instead of throwing', () => {
    assert.deepEqual(
      userAnalyticsSearchSchema.parse({
        user_id: -7,
        start_timestamp: 0,
        end_timestamp: Number.NaN,
        sources: [],
        model_search: 42,
        sort_by: 'requests',
        sort_order: 'sideways',
        p: 0,
        page_size: 25,
      }),
      {}
    )
  })

  test('validates and normalizes supported URL filters and pagination', () => {
    assert.deepEqual(
      userAnalyticsSearchSchema.parse({
        user_id: 7,
        start_timestamp: 100,
        end_timestamp: 200,
        metric: 'tokens',
        sort_mode: 'manual',
        sources: ' Gateway, codex, gateway ',
        model_search: ' gpt-5 ',
        sort_by: 'external_events',
        sort_order: 'asc',
        p: 3,
        page_size: 100,
      }),
      {
        user_id: 7,
        start_timestamp: 100,
        end_timestamp: 200,
        metric: 'tokens',
        sort_mode: 'manual',
        sources: 'gateway,codex',
        model_search: 'gpt-5',
        sort_by: 'external_events',
        sort_order: 'asc',
        p: 3,
        page_size: 100,
      }
    )
  })

  test('initializes and preserves metric-linked sorting across remounts', () => {
    const initialized = initializeUserAnalyticsSearch(
      {},
      { start_timestamp: 100, end_timestamp: 200 }
    )

    assert.deepEqual(initialized, {
      start_timestamp: 100,
      end_timestamp: 200,
      metric: 'quota',
      sort_mode: 'metric',
      sort_by: 'quota',
      sort_order: 'desc',
      p: 1,
      page_size: 20,
    })

    const tokens = changeUserAnalyticsMetric(initialized, 'tokens')
    assert.deepEqual(tokens, {
      ...initialized,
      metric: 'tokens',
      sort_by: 'token_usage',
      p: 1,
    })

    assert.deepEqual(
      initializeUserAnalyticsSearch(tokens, {
        start_timestamp: 300,
        end_timestamp: 400,
      }),
      tokens
    )
  })

  test('initializes summary filters without chart-only state', () => {
    const initializeUserUsageSummarySearch = (
      userAnalyticsState as Record<string, unknown>
    ).initializeUserUsageSummarySearch
    assert.equal(typeof initializeUserUsageSummarySearch, 'function')
    if (typeof initializeUserUsageSummarySearch !== 'function') return

    assert.deepEqual(
      initializeUserUsageSummarySearch(
        {},
        { start_timestamp: 100, end_timestamp: 200 }
      ),
      {
        start_timestamp: 100,
        end_timestamp: 200,
        sort_by: 'quota',
        sort_order: 'desc',
        p: 1,
        page_size: 20,
      }
    )
  })

  test('only clears detail filters when the selected user changes', () => {
    const current = {
      user_id: 7,
      sources: 'gateway',
      model_search: 'gpt',
      p: 4,
      page_size: 50 as const,
    }

    assert.deepEqual(changeUserAnalyticsUser(current, 7), current)
    assert.deepEqual(changeUserAnalyticsUser(current, 8), {
      ...current,
      user_id: 8,
      sources: undefined,
      model_search: undefined,
      p: 1,
    })
  })

  test('preserves explicit sorting when the primary metric changes', () => {
    const current = {
      start_timestamp: 100,
      end_timestamp: 200,
      metric: 'quota' as const,
      sort_mode: 'manual' as const,
      sort_by: 'model_name' as const,
      sort_order: 'asc' as const,
      p: 4,
      page_size: 50 as const,
    }

    assert.deepEqual(changeUserAnalyticsMetric(current, 'tokens'), {
      ...current,
      metric: 'tokens',
    })
  })
})
