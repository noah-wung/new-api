import assert from 'node:assert/strict'
import { afterEach, beforeEach, describe, test } from 'node:test'
import { getRequestUserId } from './request-user-id'

class MemoryStorage {
  private values = new Map<string, string>()

  getItem(key: string) {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string) {
    this.values.set(key, value)
  }

  removeItem(key: string) {
    this.values.delete(key)
  }

  clear() {
    this.values.clear()
  }
}

const originalWindow = globalThis.window

beforeEach(() => {
  const storage = new MemoryStorage()
  globalThis.window = {
    localStorage: storage,
  } as Window & typeof globalThis
})

afterEach(() => {
  if (originalWindow) {
    globalThis.window = originalWindow
  } else {
    delete (globalThis as { window?: Window & typeof globalThis }).window
  }
})

describe('getRequestUserId', () => {
  test('returns uid directly when present', () => {
    window.localStorage.setItem('uid', '1')

    assert.equal(getRequestUserId(), '1')
  })

  test('recovers uid from persisted user and backfills uid storage', () => {
    window.localStorage.setItem(
      'user',
      JSON.stringify({ id: 7, username: 'root' })
    )

    assert.equal(getRequestUserId(), '7')
    assert.equal(window.localStorage.getItem('uid'), '7')
  })
})
