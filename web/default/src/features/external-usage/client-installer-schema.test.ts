import assert from 'node:assert/strict'
import { test } from 'node:test'
import { clientInstallerSchema } from './client-installer-schema'

const input = {
  platform: 'windows',
  sources: ['codex', 'claude_code'],
  upstream_url: 'https://api.anthropic.com',
  upstream_is_gateway: false,
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

test('Claude routes are required only when Claude is selected', () => {
  for (const upstream_url of [
    '',
    'ftp://example.com',
    'https://key@example.com',
    'https://example.com?key=secret',
    'https://example.com#key',
  ]) {
    assert.equal(
      clientInstallerSchema.safeParse({ ...input, upstream_url }).success,
      false
    )
    assert.equal(
      clientInstallerSchema.safeParse({
        ...input,
        sources: ['codex'],
        upstream_url,
      }).success,
      true
    )
  }
})
