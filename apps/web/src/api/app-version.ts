import { ApiError, buildQuery, gatewayRequest } from './client'
import type { components } from './generated/gateway'

export type GatewayAppVersionFreshness = components['schemas']['AppVersionFreshness']

const APP_VERSION_FRESHNESS_CACHE_TTL_MS = 5 * 60 * 1000
const APP_VERSION_FRESHNESS_CACHE_PREFIX = 'app-version:freshness:v1:'

type CacheValue = GatewayAppVersionFreshness | null

type CacheEntry = {
  expiresAt: number
  value: CacheValue
}

const memoryCache = new Map<string, CacheEntry>()
const inFlightRequests = new Map<string, Promise<CacheValue>>()
const warnedReasons = new Set<string>()

export function fetchAppVersionFreshness(currentSha: string): Promise<GatewayAppVersionFreshness> {
  return gatewayRequest<GatewayAppVersionFreshness>(
    `/app-version/freshness${buildQuery({ currentSha: normalizeCommitSha(currentSha) })}`,
    { token: null },
  )
}

export function getCachedAppVersionFreshness(currentSha: string): Promise<CacheValue> {
  const normalizedSha = normalizeCommitSha(currentSha)

  if (!normalizedSha) {
    const freshness = unknownFreshness('', 'missing_current_sha')
    warnAppVersionFallback('missing_current_sha')
    return Promise.resolve(freshness)
  }

  const cached = readCachedFreshness(normalizedSha)
  if (cached !== undefined) {
    return Promise.resolve(cached)
  }

  const inFlight = inFlightRequests.get(normalizedSha)
  if (inFlight) return inFlight

  const request = fetchAppVersionFreshness(normalizedSha)
    .then((freshness) => {
      const value = normalizeFreshness(freshness, normalizedSha)
      if (value?.status === 'unknown') {
        warnAppVersionFallback(value.reason ?? 'unknown')
      }
      writeCachedFreshness(normalizedSha, value)
      return value
    })
    .catch((error: unknown) => {
      const reason = fallbackReasonFromError(error)
      warnAppVersionFallback(reason)
      writeCachedFreshness(normalizedSha, null)
      return null
    })
    .finally(() => {
      inFlightRequests.delete(normalizedSha)
    })

  inFlightRequests.set(normalizedSha, request)
  return request
}

export function resetAppVersionFreshnessCacheForTests(options?: { keepSessionStorage?: boolean }) {
  memoryCache.clear()
  inFlightRequests.clear()
  warnedReasons.clear()
  if (options?.keepSessionStorage) return
  clearSessionCache()
}

function normalizeCommitSha(value: string) {
  return value.trim().toLowerCase()
}

function normalizeFreshness(
  freshness: GatewayAppVersionFreshness,
  currentSha: string,
): GatewayAppVersionFreshness {
  return {
    ...freshness,
    currentSha: normalizeCommitSha(freshness.currentSha ?? currentSha),
    latestSha: freshness.latestSha ? normalizeCommitSha(freshness.latestSha) : undefined,
  }
}

function unknownFreshness(
  currentSha: string,
  reason: NonNullable<GatewayAppVersionFreshness['reason']>,
): GatewayAppVersionFreshness {
  return {
    checkedAt: new Date().toISOString(),
    currentSha: normalizeCommitSha(currentSha),
    reason,
    status: 'unknown',
  }
}

function readCachedFreshness(currentSha: string): CacheValue | undefined {
  const now = Date.now()
  const memory = memoryCache.get(currentSha)
  if (memory && memory.expiresAt > now) return memory.value

  const session = readSessionCache(currentSha)
  if (session && session.expiresAt > now) {
    memoryCache.set(currentSha, session)
    return session.value
  }

  memoryCache.delete(currentSha)
  removeSessionCache(currentSha)
  return undefined
}

function writeCachedFreshness(currentSha: string, value: CacheValue) {
  const entry: CacheEntry = {
    expiresAt: Date.now() + APP_VERSION_FRESHNESS_CACHE_TTL_MS,
    value,
  }
  memoryCache.set(currentSha, entry)
  writeSessionCache(currentSha, entry)
}

function sessionCacheKey(currentSha: string) {
  return `${APP_VERSION_FRESHNESS_CACHE_PREFIX}${currentSha}`
}

function readSessionCache(currentSha: string): CacheEntry | null {
  try {
    const raw = sessionStorage.getItem(sessionCacheKey(currentSha))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!isCacheEntry(parsed)) return null
    return parsed
  } catch {
    return null
  }
}

function writeSessionCache(currentSha: string, entry: CacheEntry) {
  try {
    sessionStorage.setItem(sessionCacheKey(currentSha), JSON.stringify(entry))
  } catch {
    // Browser storage is a best-effort cache only.
  }
}

function removeSessionCache(currentSha: string) {
  try {
    sessionStorage.removeItem(sessionCacheKey(currentSha))
  } catch {
    // Browser storage is a best-effort cache only.
  }
}

function clearSessionCache() {
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = sessionStorage.key(index)
      if (key?.startsWith(APP_VERSION_FRESHNESS_CACHE_PREFIX)) {
        sessionStorage.removeItem(key)
      }
    }
  } catch {
    // Browser storage is a best-effort cache only.
  }
}

function isCacheEntry(value: unknown): value is CacheEntry {
  if (!value || typeof value !== 'object') return false
  const entry = value as { expiresAt?: unknown; value?: unknown }
  return typeof entry.expiresAt === 'number' && 'value' in entry
}

function fallbackReasonFromError(error: unknown) {
  if (error instanceof ApiError) {
    return error.status ? `http_${error.status}` : error.code || 'gateway_error'
  }
  if (error instanceof TypeError) return 'network_error'
  return 'gateway_error'
}

function warnAppVersionFallback(reason: string) {
  if (warnedReasons.has(reason)) return
  warnedReasons.add(reason)
  console.warn(`[app-version] freshness check fallback: ${reason}`)
}
