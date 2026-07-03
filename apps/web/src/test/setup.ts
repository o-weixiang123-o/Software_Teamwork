import * as matchers from '@testing-library/jest-dom/matchers'
import { cleanup } from '@testing-library/react'
import { afterEach, beforeEach, expect, vi } from 'vitest'

import { resetApiClientForTests } from '@/api/client'

expect.extend(matchers)

function createTestStorage(): Storage {
  const items = new Map<string, string>()
  return {
    get length() {
      return items.size
    },
    clear() {
      items.clear()
    },
    getItem(key: string) {
      return items.get(key) ?? null
    },
    key(index: number) {
      return Array.from(items.keys())[index] ?? null
    },
    removeItem(key: string) {
      items.delete(key)
    },
    setItem(key: string, value: string) {
      items.set(key, String(value))
    },
  }
}

const localTestStorage = createTestStorage()
const sessionTestStorage = createTestStorage()

function installTestStorage() {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: localTestStorage,
  })
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    value: localTestStorage,
  })
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: sessionTestStorage,
  })
  Object.defineProperty(window, 'sessionStorage', {
    configurable: true,
    value: sessionTestStorage,
  })
}

installTestStorage()

beforeEach(() => {
  installTestStorage()
  localTestStorage.clear()
  sessionTestStorage.clear()
  resetApiClientForTests()
  vi.stubEnv('VITE_API_BASE_URL', 'http://127.0.0.1/api/v1')
})

afterEach(async () => {
  cleanup()
  resetApiClientForTests()
  window.localStorage.clear()
  window.sessionStorage.clear()
  vi.unstubAllEnvs()
  vi.unstubAllGlobals()
  const { useAuthStore } = await import('@/stores/auth-store')
  useAuthStore.setState({
    accessToken: null,
    error: null,
    status: 'anonymous',
    user: null,
    userName: null,
  })
})
