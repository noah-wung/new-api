import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  canRequestExternalUsageInstallerCommand,
  initializeExternalUsageSelection,
  shouldLoadExternalUsageData,
  syncExternalUsageSelection,
} from './selection'

describe('external usage user selection', () => {
  test('keeps summary and import selectors aligned when user changes', () => {
    assert.deepEqual(syncExternalUsageSelection(7), {
      selectedUserId: 7,
      importTargetUserId: 7,
    })
  })

  test('initializes both selectors from the first available user', () => {
    assert.deepEqual(
      initializeExternalUsageSelection(
        {
          selectedUserId: null,
          importTargetUserId: null,
        },
        [3, 5]
      ),
      {
        selectedUserId: 3,
        importTargetUserId: 3,
      }
    )
  })

  test('does not load admin external usage data before a target user is chosen', () => {
    assert.equal(shouldLoadExternalUsageData(true, null), false)
    assert.equal(shouldLoadExternalUsageData(true, 2), true)
    assert.equal(shouldLoadExternalUsageData(false, null), true)
  })

  test('prevents admins from requesting installer commands for another user', () => {
    assert.equal(
      canRequestExternalUsageInstallerCommand({
        isAdmin: true,
        currentUserId: 7,
        selectedUserId: 3,
      }),
      false
    )
    assert.equal(
      canRequestExternalUsageInstallerCommand({
        isAdmin: true,
        currentUserId: 7,
        selectedUserId: 7,
      }),
      true
    )
    assert.equal(
      canRequestExternalUsageInstallerCommand({
        isAdmin: false,
        currentUserId: 7,
        selectedUserId: null,
      }),
      true
    )
  })
})
