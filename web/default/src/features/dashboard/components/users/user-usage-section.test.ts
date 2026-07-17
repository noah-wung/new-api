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
const usageUrl = new URL('./user-usage-summary.tsx', import.meta.url)

describe('user dashboard section composition', () => {
  test('keeps model usage out of User Analytics', () => {
    assert.doesNotMatch(analytics, /UserModelUsageSummary/)
  })

  test('renders model usage in the dedicated section', () => {
    assert.equal(existsSync(usageUrl), true)
    const usage = readFileSync(usageUrl, 'utf8')
    assert.match(usage, /<UserModelUsageSummary/)
    assert.match(usage, /variant='usage-summary'/)
  })
})
