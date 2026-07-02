import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { describe, test } from 'node:test'

describe('external usage layout order', () => {
  test('places the Codex installer card before the usage summary card', () => {
    const filePath = path.resolve(import.meta.dirname, 'index.tsx')
    const source = readFileSync(filePath, 'utf8')

    const installerIndex = source.indexOf('installerCommandCopy.title')
    const usageSummaryIndex = source.indexOf("t('Usage Summary')")

    assert.notEqual(installerIndex, -1, 'installer card marker should exist')
    assert.notEqual(
      usageSummaryIndex,
      -1,
      'usage summary card marker should exist'
    )
    assert.ok(
      installerIndex < usageSummaryIndex,
      'installer card should render before usage summary'
    )
  })
})
