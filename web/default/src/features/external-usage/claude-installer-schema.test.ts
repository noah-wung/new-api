import assert from 'node:assert/strict'
import { test } from 'node:test'
import { claudeInstallerSchema } from './claude-installer-schema'

test('Claude installer accepts explicit upstream routes and rejects credentials or unsupported protocols', () => {
  const input = {
    platform: 'macos',
    upstream_url: ' https://api.anthropic.com/v1 ',
    upstream_is_gateway: false,
  }
  assert.equal(
    claudeInstallerSchema.parse(input).upstream_url,
    'https://api.anthropic.com/v1'
  )
  for (const upstream_url of [
    '',
    'invalid',
    'ftp://example.com',
    'https://key@example.com',
    'https://example.com?key=secret',
    'https://example.com#secret',
  ]) {
    assert.equal(
      claudeInstallerSchema.safeParse({ ...input, upstream_url }).success,
      false,
      upstream_url
    )
  }
  assert.equal(
    claudeInstallerSchema.safeParse({ ...input, platform: 'unknown' }).success,
    false
  )
  assert.equal(
    claudeInstallerSchema.parse({
      ...input,
      platform: 'windows',
      upstream_is_gateway: true,
    }).upstream_is_gateway,
    true
  )
})
