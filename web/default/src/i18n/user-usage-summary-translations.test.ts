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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import { STATIC_I18N_KEYS } from './static-keys'

const keys = [
  'Search for a user to view model usage.',
  'Select one user and time range for model usage totals.',
  'User Usage Summary',
  'User Usage Summary Filters',
] as const
const locales = ['en', 'zh', 'fr', 'ja', 'ru', 'vi'] as const

describe('user usage summary translations', () => {
  test('keeps the dynamically rendered section title discoverable', () => {
    assert.equal(STATIC_I18N_KEYS.includes('User Usage Summary'), true)
  })

  test('defines every new key in all supported locales', () => {
    for (const locale of locales) {
      const content = JSON.parse(
        readFileSync(
          new URL(`./locales/${locale}.json`, import.meta.url),
          'utf8'
        )
      ) as { translation: Record<string, string> }

      for (const key of keys) {
        assert.equal(
          typeof content.translation[key],
          'string',
          `${locale} is missing ${key}`
        )
        assert.notEqual(content.translation[key].trim(), '')
      }
    }
  })
})
