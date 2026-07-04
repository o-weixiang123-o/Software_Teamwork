import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  apiClient,
  ApiError,
  gatewayPageRequest,
  gatewayRequest,
  requestBinary,
  requestVoid,
  resetApiClientForTests,
  streamGateway,
} from './client'

function setGatewayBaseUrl() {
  vi.stubEnv('VITE_API_BASE_URL', 'http://gateway.test/api/v1')
}

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    status: init?.status ?? 200,
    statusText: init?.statusText,
  })
}

function streamResponse(body: string, init?: ResponseInit) {
  return new Response(new TextEncoder().encode(body), {
    headers: { 'Content-Type': 'text/event-stream', ...init?.headers },
    status: init?.status ?? 200,
  })
}

describe('gateway API client', () => {
  beforeEach(() => {
    setGatewayBaseUrl()
  })

  it('unwraps success and paginated envelopes', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ data: { id: 'kb-1' }, requestId: 'req-1' }))
      .mockResolvedValueOnce(
        jsonResponse({
          data: [{ id: 'kb-1' }],
          page: { page: 1, pageSize: 20, total: 1 },
          requestId: 'req-2',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    await expect(gatewayRequest('/knowledge-bases/kb-1')).resolves.toEqual({ id: 'kb-1' })
    await expect(gatewayPageRequest('/knowledge-bases')).resolves.toEqual({
      items: [{ id: 'kb-1' }],
      page: { page: 1, pageSize: 20, total: 1 },
    })
  })

  it('maps gateway error envelopes to ApiError details', async () => {
    apiClient.setToken('token-123')
    const fetchMock = vi.fn(
      async (_request: Request) =>
        new Response(
          JSON.stringify({
            error: {
              code: 'validation_error',
              message: 'name is required',
              requestId: 'req-123',
              fields: { name: 'is required' },
            },
          }),
          {
            headers: { 'Content-Type': 'application/json' },
            status: 400,
          },
        ),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      gatewayRequest('/reports', { method: 'POST', body: { name: '' } }),
    ).rejects.toEqual(
      expect.objectContaining({
        code: 'validation_error',
        fields: { name: 'is required' },
        message: 'name is required',
        requestId: 'req-123',
        status: 400,
      }),
    )

    const request = fetchMock.mock.calls[0]?.[0]
    expect(request).toBeInstanceOf(Request)
    if (!(request instanceof Request)) throw new Error('expected fetch to receive a Request')
    expect(request.headers.get('Authorization')).toBe('Bearer token-123')
    expect(request.headers.get('Content-Type')).toBe('application/json')
  })

  it('maps gateway error envelopes and clears stale tokens on unauthorized responses', async () => {
    apiClient.setToken('stale-token')
    const onUnauthorized = vi.fn()
    apiClient.setUnauthorizedHandler(onUnauthorized)
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse(
          {
            error: {
              code: 'unauthorized',
              fields: { token: 'expired' },
              message: 'session expired',
              requestId: 'req-expired',
            },
          },
          { status: 401 },
        ),
      ),
    )

    await expect(requestVoid('/sessions/current', { method: 'DELETE' })).rejects.toMatchObject({
      code: 'unauthorized',
      fields: { token: 'expired' },
      message: 'session expired',
      requestId: 'req-expired',
      status: 401,
    })
    expect(apiClient.getToken()).toBeNull()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem('auth_token')).toBeNull()
  })

  it('keeps the local development bypass token when real gateway endpoints reject it', async () => {
    apiClient.setToken('dev-token-bypass')
    const onUnauthorized = vi.fn()
    apiClient.setUnauthorizedHandler(onUnauthorized)
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse(
          {
            error: {
              code: 'unauthorized',
              message: 'invalid development token',
              requestId: 'req-dev-bypass',
            },
          },
          { status: 401 },
        ),
      ),
    )

    await expect(gatewayRequest('/knowledge-bases')).rejects.toMatchObject({
      code: 'unauthorized',
      message: 'invalid development token',
      requestId: 'req-dev-bypass',
      status: 401,
    })
    expect(apiClient.getToken()).toBe('dev-token-bypass')
    expect(onUnauthorized).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('dev-token-bypass')
  })

  it('does not force JSON content-type for FormData uploads', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        data: { id: 'doc-1' },
        requestId: 'req-upload',
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const form = new FormData()
    form.set('file', new File(['hello'], 'guide.txt', { type: 'text/plain' }))

    await gatewayRequest('/knowledge-bases/kb-1/documents', {
      body: form,
      method: 'POST',
    })

    const request = fetchMock.mock.calls[0]?.[0] as Request
    expect(request.headers.get('Content-Type')).not.toBe('application/json')
    expect(request.headers.get('Content-Type')).toContain('multipart/form-data')
    expect(request.headers.get('Accept')).toBe('application/json')
  })

  it('returns binary responses without parsing JSON envelopes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        new Response('docx', {
          headers: { 'Content-Type': 'application/octet-stream' },
          status: 200,
        }),
      ),
    )

    const blob = await requestBinary('/report-files/file-1/content')

    expect(await blob.text()).toBe('docx')
  })

  it('parses managed SSE events and supports abortable streams', async () => {
    const onEvent = vi.fn()
    const onDone = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          streamResponse(
            [
              'id: event-1',
              'event: answer.delta',
              'retry: 1500',
              'data: {"content":"hello"}',
              '',
              ': heartbeat',
              'event: answer.completed',
              'data: {"done":true}',
              '',
              '',
            ].join('\n'),
          ),
        ),
    )

    const stream = streamGateway('/qa-sessions/session-1/messages', {
      body: { content: 'hello', stream: true },
      onDone,
      onEvent,
    })

    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))

    expect(stream.signal.aborted).toBe(false)
    expect(onEvent).toHaveBeenNthCalledWith(1, {
      data: '{"content":"hello"}',
      event: 'answer.delta',
      id: 'event-1',
      retry: 1500,
    })
    expect(onEvent).toHaveBeenNthCalledWith(2, {
      data: '{"done":true}',
      event: 'answer.completed',
      id: undefined,
      retry: undefined,
    })

    stream.abort()
    expect(stream.signal.aborted).toBe(true)
  })

  it('reports invalid stream responses through the provided error callback', async () => {
    const onError = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ data: {}, requestId: 'req-json' })),
    )

    streamGateway('/qa-sessions/session-1/messages', {
      onError,
      onEvent: vi.fn(),
    })

    await vi.waitFor(() => expect(onError).toHaveBeenCalledTimes(1))
    expect(onError.mock.calls[0]?.[0]).toBeInstanceOf(ApiError)
    expect(onError.mock.calls[0]?.[0]).toMatchObject({ code: 'invalid_stream_response' })
  })

  it('resets module-level client state for test isolation', async () => {
    vi.stubEnv('VITE_API_MOCKS', 'true')
    const unauthorizedHandler = vi.fn()
    apiClient.setToken('stored-token')
    apiClient.setAccessTokenProvider(() => 'provider-token')
    apiClient.setRequestIdProvider(() => 'req-provider')
    apiClient.setUnauthorizedHandler(unauthorizedHandler)
    apiClient.setMockRoutes([
      {
        handler: () =>
          new Response(JSON.stringify({ data: { id: 'mock-user' }, requestId: 'req-mock' }), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          }),
        method: 'GET',
        path: '/api/v1/users/me',
      },
    ])

    resetApiClientForTests()

    const fetchMock = vi.fn(
      async (_request: Request) =>
        new Response(
          JSON.stringify({
            error: {
              code: 'unauthorized',
              message: 'session expired',
              requestId: 'req-auth',
            },
          }),
          {
            headers: { 'Content-Type': 'application/json' },
            status: 401,
          },
        ),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(gatewayRequest('/api/v1/users/me')).rejects.toBeInstanceOf(ApiError)

    const request = fetchMock.mock.calls[0]?.[0]
    expect(request).toBeInstanceOf(Request)
    if (!(request instanceof Request)) throw new Error('expected fetch to receive a Request')
    expect(request.headers.get('Authorization')).toBeNull()
    expect(request.headers.get('X-Request-Id')).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(unauthorizedHandler).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBeNull()
  })
})
