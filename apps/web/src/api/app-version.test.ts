import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  fetchAppVersionFreshness,
  getCachedAppVersionFreshness,
  resetAppVersionFreshnessCacheForTests,
} from './app-version'

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    status: init?.status ?? 200,
    statusText: init?.statusText,
  })
}

describe('app version freshness API', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://gateway.test/api/v1')
    resetAppVersionFreshnessCacheForTests()
  })

  it('checks freshness through Gateway without exposing a token or calling GitHub directly', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        data: {
          checkedAt: '2026-07-03T12:00:00Z',
          currentSha: 'abcdef123456',
          latestSha: 'abcdef123456',
          status: 'current',
        },
        requestId: 'req-app-version',
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchAppVersionFreshness('ABCDEF123456')).resolves.toMatchObject({
      currentSha: 'abcdef123456',
      status: 'current',
    })

    const request = fetchMock.mock.calls[0]?.[0]
    expect(request).toBeInstanceOf(Request)
    if (!(request instanceof Request)) throw new Error('expected fetch to receive a Request')
    expect(request.url).toBe(
      'http://gateway.test/api/v1/app-version/freshness?currentSha=abcdef123456',
    )
    expect(request.url).not.toContain('api.github.com')
    expect(request.headers.get('Authorization')).toBeNull()
  })

  it('caches freshness checks and reuses session storage across reload-like resets', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        data: {
          checkedAt: '2026-07-03T12:00:00Z',
          currentSha: 'abcdef123456',
          latestSha: 'abcdef123456',
          status: 'current',
        },
        requestId: 'req-app-version',
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(getCachedAppVersionFreshness('abcdef123456')).resolves.toMatchObject({
      status: 'current',
    })
    await expect(getCachedAppVersionFreshness('abcdef123456')).resolves.toMatchObject({
      status: 'current',
    })
    resetAppVersionFreshnessCacheForTests({ keepSessionStorage: true })
    await expect(getCachedAppVersionFreshness('abcdef123456')).resolves.toMatchObject({
      status: 'current',
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('returns null and warns briefly when Gateway returns a non-ok response', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse(
        {
          error: {
            code: 'forbidden',
            message: 'forbidden',
            requestId: 'req-forbidden',
          },
        },
        { status: 403 },
      ),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(getCachedAppVersionFreshness('abcdef123456')).resolves.toBeNull()
    await expect(getCachedAppVersionFreshness('abcdef123456')).resolves.toBeNull()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(warnSpy).toHaveBeenCalledTimes(1)
    expect(warnSpy).toHaveBeenCalledWith('[app-version] freshness check fallback: http_403')
  })

  it('returns unknown without a network request when the current build has no commit SHA', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetchMock = vi.fn<typeof fetch>()
    vi.stubGlobal('fetch', fetchMock)

    await expect(getCachedAppVersionFreshness('')).resolves.toMatchObject({
      reason: 'missing_current_sha',
      status: 'unknown',
    })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(warnSpy).toHaveBeenCalledWith(
      '[app-version] freshness check fallback: missing_current_sha',
    )
  })
})
