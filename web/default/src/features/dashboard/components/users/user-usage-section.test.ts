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
import { existsSync, readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const analytics = readFileSync(
  new URL('./user-analytics.tsx', import.meta.url),
  'utf8'
)
const controls = readFileSync(
  new URL('./user-analytics-controls.tsx', import.meta.url),
  'utf8'
)
const usageUrl = new URL('./user-usage-summary.tsx', import.meta.url)

describe('user dashboard section composition', () => {
  test('keeps model usage out of User Analytics', () => {
    assert.doesNotMatch(analytics, /UserModelUsageSummary/)
  })

  test('keeps User Analytics on the original compact filter bar', () => {
    assert.match(analytics, /variant='analytics'/)
    assert.match(controls, /function UserAnalyticsCompactControls/)

    const compactControls = controls.slice(
      controls.indexOf('function UserAnalyticsCompactControls'),
      controls.indexOf('function UserUsageSummaryControls')
    )

    assert.match(
      compactControls,
      /flex items-center gap-1\.5 overflow-x-auto pb-1 sm:gap-2/
    )
    assert.doesNotMatch(compactControls, /<Card/)
    assert.doesNotMatch(compactControls, /DatePicker/)
    assert.doesNotMatch(compactControls, /Find User/)
  })

  test('renders model usage in the dedicated section', () => {
    assert.equal(existsSync(usageUrl), true)
    const usage = readFileSync(usageUrl, 'utf8')
    assert.match(usage, /<UserModelUsageSummary/)
    assert.match(usage, /variant='usage-summary'/)
  })

  test('keeps usage summary search direct and the selected user read-only', () => {
    const usageControls = controls.slice(
      controls.indexOf('function UserUsageSummaryControls'),
      controls.indexOf('export function UserAnalyticsControls')
    )

    assert.match(usageControls, /lg:col-span-2/)
    assert.match(usageControls, /grid grid-cols-2 gap-2/)
    assert.match(usageControls, /selectUserSearchResult/)
    assert.match(usageControls, /props\.onUserSelect/)
    assert.match(usageControls, /props\.selectedTarget/)
    assert.match(usageControls, /<StatusBadge/)
    assert.doesNotMatch(usageControls, /Browser timezone/)
    assert.doesNotMatch(usageControls, /<Combobox/)
  })
})
