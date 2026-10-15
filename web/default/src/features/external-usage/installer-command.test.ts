import assert from 'node:assert/strict'
import { test } from 'node:test'
import { normalizeInstallerCommandExpiry } from './installer-command'

test('installer expiry accepts API timestamps and rejects invalid expiry values', () => {
  assert.equal(normalizeInstallerCommandExpiry(1700000000), 1700000000)
  assert.equal(normalizeInstallerCommandExpiry('1700000000'), 1700000000)
  assert.equal(
    normalizeInstallerCommandExpiry('2023-11-14T22:13:20Z'),
    1700000000
  )
  for (const value of [undefined, null, '', 'invalid', 0, -1, Infinity]) {
    assert.equal(normalizeInstallerCommandExpiry(value), null)
  }
})
