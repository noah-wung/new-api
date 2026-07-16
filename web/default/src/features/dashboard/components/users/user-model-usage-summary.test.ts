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
