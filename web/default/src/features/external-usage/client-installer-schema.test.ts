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

test('signed installs preserve explicit automatic update choices', () => {
  for (const auto_update of [true, false]) {
    const values = { ...input, managed_install: true, auto_update }
    assert.deepEqual(clientInstallerSchema.parse(values), values)
  }
})

test('interactive commands retain authorized source scope and installation mode', () => {
  const values = {
    ...input,
    interactive: true,
    managed_install: true,
    auto_update: false,
  }
  assert.deepEqual(clientInstallerSchema.parse(values), values)
  assert.equal(
    clientInstallerSchema.safeParse({ ...values, interactive: 'yes' }).success,
    false
  )
})
