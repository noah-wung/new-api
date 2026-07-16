import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  createManualSortPatch,
  encodeSelectedSources,
  resolveSelectedSources,
} from './user-model-usage-summary'
import {
  formatSourceMetric,
  SOURCE_TABLE_HEADER_KEYS,
} from './user-model-usage-table'

describe('user model usage summary helpers', () => {
  test('selects every available source when the URL parameter is absent', () => {
    assert.deepEqual(
      resolveSelectedSources(undefined, ['gateway', 'cursor', 'codex']),
      ['gateway', 'cursor', 'codex']
    )
  })

  test('removes the URL parameter when every source is selected', () => {
    assert.equal(
      encodeSelectedSources(
        ['codex', 'gateway', 'cursor'],
        ['gateway', 'cursor', 'codex']
      ),
      undefined
    )
  })

  test('marks a table-header sort as manual', () => {
    assert.deepEqual(createManualSortPatch('model_name', 'asc'), {
      sort_mode: 'manual',
      sort_by: 'model_name',
      sort_order: 'asc',
    })
  })

  test('renders nullable source metrics as an em dash', () => {
    assert.equal(formatSourceMetric(null, String), '—')
  })

  test('preserves a real zero source metric', () => {
    assert.equal(formatSourceMetric(0, String), '0')
  })

  test('defines every semantic column in the nested source table', () => {
    assert.deepEqual(SOURCE_TABLE_HEADER_KEYS, [
      'Source',
      'Token usage',
      'Quota',
      'Gateway requests',
      'External events',
    ])
  })
})
