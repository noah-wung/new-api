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
  rememberDashboardSectionSearch,
  restoreDashboardSectionSearch,
} from './dashboard-section-search'

describe('dashboard section search memory', () => {
  test('restores each section without leaking another section filters', () => {
    let memory = rememberDashboardSectionSearch({}, 'users', {
      user_id: 7,
      metric: 'tokens',
    })
    memory = rememberDashboardSectionSearch(memory, 'user-usage', {
      user_id: 12,
      sources: 'codex',
      p: 3,
    })

    assert.deepEqual(restoreDashboardSectionSearch(memory, 'users'), {
      user_id: 7,
      metric: 'tokens',
    })
    assert.deepEqual(restoreDashboardSectionSearch(memory, 'user-usage'), {
      user_id: 12,
      sources: 'codex',
      p: 3,
    })
    assert.deepEqual(restoreDashboardSectionSearch(memory, 'models'), {})
  })
})
