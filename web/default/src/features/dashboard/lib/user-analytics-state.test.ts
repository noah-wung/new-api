import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  createBrowserLocalDayRange,
  createUserAnalyticsPresetRange,
  decodeUsageSources,
  encodeUsageSources,
  getBrowserTimeZoneLabel,
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

  test('creates every supported preset from browser-local day boundaries', () => {
    const now = new Date(2026, 6, 16, 13, 14, 15, 678)

    for (const days of [1, 7, 14, 30, 90] as const) {
      assert.deepEqual(createUserAnalyticsPresetRange(days, now), {
        start_timestamp: seconds(new Date(2026, 6, 17 - days, 0, 0, 0, 0)),
        end_timestamp: seconds(new Date(2026, 6, 16, 23, 59, 59, 999)),
      })
    }
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
        sources: 'gateway,codex',
        model_search: 'gpt-5',
        sort_by: 'external_events',
        sort_order: 'asc',
        p: 3,
        page_size: 100,
      }
    )
  })
})
