import { useAuthStore } from '@/stores/auth-store'

const USER_ID_STORAGE_KEY = 'uid'
const USER_STORAGE_KEY = 'user'

function readUserIdFromPersistedUser(): string | null {
  try {
    const authUserId = useAuthStore.getState().auth.user?.id
    if (typeof authUserId === 'number' && Number.isFinite(authUserId)) {
      return String(authUserId)
    }
  } catch {
    /* empty */
  }

  try {
    const rawUser = window.localStorage.getItem(USER_STORAGE_KEY)
    if (!rawUser) return null

    const parsed = JSON.parse(rawUser) as { id?: unknown }
    if (typeof parsed.id === 'number' && Number.isFinite(parsed.id)) {
      return String(parsed.id)
    }
    if (typeof parsed.id === 'string' && parsed.id.trim() !== '') {
      return parsed.id.trim()
    }
  } catch {
    /* empty */
  }

  return null
}

export function getRequestUserId(): string | null {
  if (typeof window === 'undefined') return null

  try {
    const stored = window.localStorage.getItem(USER_ID_STORAGE_KEY)
    if (stored && stored.trim() !== '') return stored

    const fallback = readUserIdFromPersistedUser()
    if (!fallback) return null

    window.localStorage.setItem(USER_ID_STORAGE_KEY, fallback)
    return fallback
  } catch {
    return null
  }
}
