import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildExternalUsageSourceOptions,
  getDefaultDeviceAllowedSources,
} from './source-options'

describe('external usage source options', () => {
  test('prepends cursor to configured client sources and removes duplicates', () => {
    assert.deepEqual(
      buildExternalUsageSourceOptions({
        external_usage_allowed_sources: [' zcode ', 'codex', 'codex', ''],
      }),
      ['cursor', 'zcode', 'codex']
    )
  })

  test('falls back to codex when status omits configured client sources', () => {
    assert.deepEqual(buildExternalUsageSourceOptions({}), ['cursor', 'codex'])
    assert.deepEqual(getDefaultDeviceAllowedSources({}), ['codex'])
  })
})
