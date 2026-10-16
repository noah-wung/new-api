import assert from 'node:assert/strict'
import { test } from 'node:test'
import { clientInstallerSchema } from './client-installer-schema'

const input = {
  platform: 'windows',
  sources: ['codex', 'claude_code'],
}

test('unified installer requires supported, unique client selections', () => {
  assert.equal(clientInstallerSchema.safeParse(input).success, true)
  for (const sources of [[], ['cursor'], ['codex', 'codex']]) {
    assert.equal(
      clientInstallerSchema.safeParse({ ...input, sources }).success,
      false
    )
  }
  for (const sources of [['codex'], ['claude_code']]) {
    assert.equal(
      clientInstallerSchema.safeParse({ ...input, sources }).success,
      true
    )
  }
})

test('all client selections work without upstream fields', () => {
  for (const sources of [
    ['codex'],
    ['claude_code'],
    ['codex', 'claude_code'],
  ]) {
    const values = { platform: 'windows', sources }
    assert.deepEqual(clientInstallerSchema.parse(values), values)
  }
})
