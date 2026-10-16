import assert from 'node:assert/strict'
import { test } from 'node:test'
import { claudeInstallerSchema } from './claude-installer-schema'

test('Claude installer needs only a supported platform', () => {
  for (const platform of ['macos', 'windows']) {
    assert.deepEqual(claudeInstallerSchema.parse({ platform }), { platform })
  }
  assert.equal(
    claudeInstallerSchema.safeParse({ platform: 'unknown' }).success,
    false
  )
})
