import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { buildInstallerCommandCardCopy } from './installer-command'

describe('external usage installer command', () => {
  test('renders macOS and Windows platform labels', () => {
    const copy = buildInstallerCommandCardCopy((value) => value)

    assert.deepEqual(copy.platformOptions, [
      { value: 'macos', label: 'macOS' },
      { value: 'windows', label: 'Windows' },
    ])
  })

  test('does not reuse the obsolete usage reporter setup copy', () => {
    const copy = buildInstallerCommandCardCopy((value) => value)

    assert.notEqual(copy.title, 'Usage Reporter Setup')
    assert.doesNotMatch(copy.description, /usage-reporter/i)
    assert.doesNotMatch(copy.commandLabel, /Build Binary|Run Once|Cron Example/)
    assert.doesNotMatch(copy.emptyState, /usage reporter/i)
  })
})
