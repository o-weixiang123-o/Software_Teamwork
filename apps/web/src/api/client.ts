/**
 * API client aligned with the Gateway OpenAPI specification.
 *
 * Envelope contracts:
 * - Success  : { data: T, requestId: string }
 * - List     : { data: T[], page: { page, pageSize, total }, requestId: string }
 * - Error    : { error: { code: string, message: string, requestId: string, fields?: Record<string,string> } }
 *
 * Auth       : Authorization: Bearer <token> on every business call.
 */

// ---------------------------------------------------------------------------
// Imports
// ---------------------------------------------------------------------------

import type { GatewayPath } from './active-paths'
import { activeGatewayPathSet } from './active-paths'

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_GATEWAY_BASE_URL = '/api/v1'
const DEV_BYPASS_TOKEN = 'dev-token-bypass'
const JSON_CONTENT_TYPE = 'application/json'
const SSE_CONTENT_TYPE = 'text/event-stream'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type GatewayMethod = 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH' | 'HEAD' | 'OPTIONS'

interface GatewaySuccessEnvelope<T> {
  data: T
  requestId: string
}

interface GatewayErrorEnvelope {
  error: {
    code: string
    message: string
    requestId?: string
    fields?: Record<string, string>
  }
}

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

export class ApiError extends Error {
  code: string
  status: number
  requestId?: string
  fields?: Record<string, string>

  constructor(params: {
    code: string
    message: string
    status?: number
    requestId?: string
    fields?: Record<string, string>
  }) {
    super(params.message)
    this.name = 'ApiError'
    this.code = params.code
    this.status = params.status ?? 0
    this.requestId = params.requestId
    this.fields = params.fields
  }

  /** Check if the error is due to missing or invalid authentication. */
  isUnauthorized(): boolean {
    return this.status === 401 || this.code === 'unauthorized'
  }

  /** Check if the error is due to insufficient permissions. */
  isForbidden(): boolean {
    return this.status === 403 || this.code === 'forbidden'
  }

  /** Check if the error is due to a missing resource. */
  isNotFound(): boolean {
    return this.status === 404 || this.code === 'not_found'
  }

  /** Check if the route is active in the contract but not implemented yet. */
  isNotImplemented(): boolean {
    return this.status === 501 || this.code === 'not_implemented' || this.code === 'http_501'
  }

  /** Check if a downstream service or infrastructure dependency failed. */
  isDependencyError(): boolean {
    return this.status === 502 || this.code === 'dependency_error'
  }
}

// ---------------------------------------------------------------------------
// Token management (module-level)
// ---------------------------------------------------------------------------

const AUTH_TOKEN_KEY = 'auth_token'
let _token: string | null = null

function loadToken(): string | null {
  if (_token) return _token
  try {
    const stored = localStorage.getItem(AUTH_TOKEN_KEY)
    if (stored) _token = stored
  } catch {
    // localStorage may be unavailable (SSR, test env)
  }
  return _token
}

// ---------------------------------------------------------------------------
// Exported gateway types
// ---------------------------------------------------------------------------

export type GatewayPaginatedEnvelope<T> = GatewaySuccessEnvelope<T[]> & {
  page: {
    page: number
    pageSize: number
    total: number
  }
}

export type GatewayPage = GatewayPaginatedEnvelope<unknown>['page']

type RequestBody = BodyInit | Record<string, unknown> | unknown[] | null

export type GatewayRequestOptions = Omit<RequestInit, 'body' | 'method'> & {
  body?: RequestBody
  method?: GatewayMethod
  requestId?: string
  token?: string | null
}

type GatewayStreamOptions = GatewayRequestOptions & {
  onEvent: (event: SseEvent) => void
  onError?: (error: ApiError) => void
  onDone?: () => void
}

export type SseEvent = {
  event: string
  data: string
  id?: string
  retry?: number
}

type MockHandler = (request: Request) => Response | Promise<Response>

type MockRoute = {
  method: GatewayMethod
  path: GatewayPath
  handler: MockHandler
}

// ---------------------------------------------------------------------------
// Module-level state for providers and mocks
// ---------------------------------------------------------------------------

let accessTokenProvider: (() => string | null | undefined) | undefined
let requestIdProvider: (() => string | undefined) | undefined
let unauthorizedHandler: (() => void) | undefined
let mockRoutes: MockRoute[] = []

// ---------------------------------------------------------------------------
// Singleton API client object
// ---------------------------------------------------------------------------

export const apiClient = {
  get baseUrl() {
    return getGatewayBaseUrl()
  },
  getToken(): string | null {
    return loadToken()
  },
  setToken(token: string | null): void {
    _token = token
    if (token) {
      try {
        localStorage.setItem(AUTH_TOKEN_KEY, token)
      } catch {
        // noop
      }
    } else {
      try {
        localStorage.removeItem(AUTH_TOKEN_KEY)
      } catch {
        // noop
      }
    }
  },
  setAccessTokenProvider(provider: typeof accessTokenProvider) {
    accessTokenProvider = provider
  },
  setRequestIdProvider(provider: typeof requestIdProvider) {
    requestIdProvider = provider
  },
  setUnauthorizedHandler(handler: typeof unauthorizedHandler) {
    unauthorizedHandler = handler
  },
  setMockRoutes(routes: readonly MockRoute[]) {
    mockRoutes = routes.map((route) => {
      assertActiveGatewayPath(route.path)
      return route
    })
  },
  clearMockRoutes() {
    mockRoutes = []
  },
}

export function resetApiClientForTests(): void {
  _token = null
  accessTokenProvider = undefined
  requestIdProvider = undefined
  unauthorizedHandler = undefined
  mockRoutes = []
  try {
    localStorage.removeItem(AUTH_TOKEN_KEY)
  } catch {
    // localStorage may be unavailable outside browser-like test environments.
  }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function getGatewayBaseUrl(): string {
  const configured = import.meta.env?.VITE_API_BASE_URL as string | undefined
  return stripTrailingSlash(configured || DEFAULT_GATEWAY_BASE_URL)
}

function stripTrailingSlash(value: string): string {
  return value.endsWith('/') ? value.slice(0, -1) : value
}

function toSearchParams(
  query?: Record<string, string | number | boolean | null | undefined>,
): URLSearchParams | undefined {
  if (!query) return undefined
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value == null) continue
    params.set(key, String(value))
  }
  return params
}

function joinUrl(
  path: string,
  query?: URLSearchParams | Record<string, string | number | boolean | null | undefined>,
): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const url = `${getGatewayBaseUrl()}${normalizedPath}`
  const params = query instanceof URLSearchParams ? query : toSearchParams(query)
  const queryString = params?.toString()
  return queryString ? `${url}?${queryString}` : url
}

function buildHeaders(options: GatewayRequestOptions, hasJsonBody: boolean): Headers {
  const headers = new Headers(options.headers)
  const token =
    options.token !== undefined ? options.token : (accessTokenProvider?.() ?? loadToken())
  const requestId = options.requestId ?? requestIdProvider?.()

  if (hasJsonBody && !headers.has('Content-Type')) {
    headers.set('Content-Type', JSON_CONTENT_TYPE)
  }
  if (!headers.has('Accept')) {
    headers.set('Accept', JSON_CONTENT_TYPE)
  }
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  if (requestId) {
    headers.set('X-Request-Id', requestId)
  }

  return headers
}

function prepareBody(body: RequestBody | undefined): { body?: BodyInit; hasJsonBody: boolean } {
  if (body == null) return { hasJsonBody: false }
  if (
    body instanceof FormData ||
    body instanceof Blob ||
    body instanceof ArrayBuffer ||
    body instanceof URLSearchParams ||
    typeof body === 'string'
  ) {
    return { body, hasJsonBody: false }
  }
  return { body: JSON.stringify(body), hasJsonBody: true }
}

function isGatewayErrorEnvelope(value: unknown): value is GatewayErrorEnvelope {
  return Boolean(
    value &&
    typeof value === 'object' &&
    'error' in value &&
    (value as { error?: unknown }).error &&
    typeof (value as { error: { message?: unknown } }).error.message === 'string',
  )
}

async function readJsonSafely(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) return undefined
  try {
    return JSON.parse(text) as unknown
  } catch {
    return text
  }
}

async function toApiError(response: Response): Promise<ApiError> {
  const token = accessTokenProvider?.() ?? loadToken()
  const isDevBypassUnauthorized =
    import.meta.env.DEV && token === DEV_BYPASS_TOKEN && response.status === 401

  // Clear stale real tokens on 401. The development bypass token is only a
  // local shell shortcut and can receive 401s from real Gateway endpoints.
  if (response.status === 401 && !isDevBypassUnauthorized) {
    apiClient.setToken(null)
    unauthorizedHandler?.()
  }

  const body = await readJsonSafely(response)
  if (isGatewayErrorEnvelope(body)) {
    return new ApiError({
      code: body.error.code,
      message: body.error.message,
      status: response.status,
      requestId: body.error.requestId ?? response.headers.get('X-Request-Id') ?? undefined,
      fields: body.error.fields,
    })
  }

  return new ApiError({
    code: response.status ? `http_${response.status}` : 'network_error',
    message: typeof body === 'string' && body ? body : response.statusText || 'Request failed',
    status: response.status,
    requestId: response.headers.get('X-Request-Id') ?? undefined,
  })
}

function assertActiveGatewayPath(path: GatewayPath): void {
  if (!activeGatewayPathSet.has(path)) {
    throw new Error(`Mock path is not an active gateway OpenAPI path: ${path}`)
  }
}

function matchMock(method: GatewayMethod, path: string): MockRoute | undefined {
  if (import.meta.env?.VITE_API_MOCKS !== 'true') return undefined
  return mockRoutes.find((route) => route.method === method && route.path === path)
}

async function fetchGateway(path: string, options: GatewayRequestOptions = {}): Promise<Response> {
  const method = options.method ?? 'GET'
  const mock = matchMock(method, path)
  const { body, hasJsonBody } = prepareBody(options.body)
  const headers = buildHeaders(options, hasJsonBody)
  const request = new Request(joinUrl(path), {
    ...options,
    method,
    headers,
    body,
  })

  if (mock) return mock.handler(request)
  return fetch(request)
}

function withJsonHeaders(options?: GatewayRequestOptions): GatewayRequestOptions {
  return {
    ...options,
    headers: {
      Accept: JSON_CONTENT_TYPE,
      ...options?.headers,
    },
  }
}

// ---------------------------------------------------------------------------
// SSE stream reader
// ---------------------------------------------------------------------------

async function readSseStream(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: SseEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = ''
  let currentData = ''
  let currentId: string | undefined
  let currentRetry: number | undefined

  const flush = () => {
    if (currentEvent || currentData) {
      onEvent({
        event: currentEvent || 'message',
        data: currentData,
        id: currentId,
        retry: currentRetry,
      })
      currentEvent = ''
      currentData = ''
      currentId = undefined
      currentRetry = undefined
    }
  }

  const processLine = (line: string) => {
    const trimmed = line.endsWith('\r') ? line.slice(0, -1) : line

    if (trimmed === '') {
      flush()
    } else if (trimmed.startsWith('event:')) {
      currentEvent = trimmed.slice(6).trimStart()
    } else if (trimmed.startsWith('data:')) {
      currentData = trimmed.slice(5).trimStart()
    } else if (trimmed.startsWith('id:')) {
      currentId = trimmed.slice(3).trimStart() || undefined
    } else if (trimmed.startsWith('retry:')) {
      const val = parseInt(trimmed.slice(6).trimStart(), 10)
      if (!isNaN(val)) currentRetry = val
    }
    // Lines starting with ':' are SSE comments — silently ignored
  }

  try {
    for (;;) {
      if (signal?.aborted) break
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        processLine(line)
      }
    }

    // Flush decoder remainder
    buffer += decoder.decode()
    if (buffer.trim()) {
      for (const line of buffer.split('\n')) {
        processLine(line)
      }
      flush()
    }
  } finally {
    reader.releaseLock()
  }
}

// ---------------------------------------------------------------------------
// Core request primitives
// ---------------------------------------------------------------------------

export async function requestEnvelope<T>(
  path: string,
  options?: GatewayRequestOptions,
): Promise<GatewaySuccessEnvelope<T>> {
  const response = await fetchGateway(path, options)
  if (!response.ok) throw await toApiError(response)
  return (await response.json()) as GatewaySuccessEnvelope<T>
}

export async function requestPaginated<T>(
  path: string,
  options?: GatewayRequestOptions,
): Promise<GatewayPaginatedEnvelope<T>> {
  const response = await fetchGateway(path, options)
  if (!response.ok) throw await toApiError(response)
  return (await response.json()) as GatewayPaginatedEnvelope<T>
}

export async function requestJson<T>(path: string, options?: GatewayRequestOptions): Promise<T> {
  const envelope = await requestEnvelope<T>(path, options)
  return envelope.data
}

export async function requestVoid(path: string, options?: GatewayRequestOptions): Promise<void> {
  const response = await fetchGateway(path, options)
  if (!response.ok) throw await toApiError(response)
}

export async function requestBinary(path: string, options?: GatewayRequestOptions): Promise<Blob> {
  const response = await fetchGateway(path, {
    ...options,
    headers: {
      Accept: 'application/octet-stream',
      ...options?.headers,
    },
  })
  if (!response.ok) throw await toApiError(response)
  return response.blob()
}

// ---------------------------------------------------------------------------
// Public convenience API
// ---------------------------------------------------------------------------

/**
 * Build a URL query string from a params object.
 * Returns an empty string if no params are provided, otherwise `?key=value&...`.
 */
export function buildQuery(
  params: Record<string, string | number | boolean | undefined | null>,
): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value))
    }
  }

  const query = search.toString()
  return query ? `?${query}` : ''
}

/**
 * Single-resource JSON request.
 * Unwraps the gateway success envelope and returns the `data` payload.
 */
export function gatewayRequest<T>(path: string, options?: GatewayRequestOptions): Promise<T> {
  return requestJson<T>(path, withJsonHeaders(options))
}

/**
 * Paginated-list JSON request.
 * Unwraps the gateway paginated envelope and returns `{ items, page }`.
 */
export async function gatewayPageRequest<T>(
  path: string,
  options?: GatewayRequestOptions,
): Promise<{ items: T[]; page: GatewayPage }> {
  const envelope = await requestPaginated<T>(path, withJsonHeaders(options))
  return { items: envelope.data, page: envelope.page }
}

/**
 * Binary file download request.
 * Returns the response body as a Blob.
 */
export function gatewayFileRequest(path: string, options?: GatewayRequestOptions): Promise<Blob> {
  return requestBinary(path, {
    ...options,
    headers: {
      Accept:
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document, application/octet-stream',
      ...options?.headers,
    },
  })
}

/**
 * Streaming request — returns raw Response for SSE / event-stream consumption.
 *
 * Attaches auth and request-id headers but does NOT parse the response body.
 * The caller is responsible for reading the stream and handling errors.
 */
export async function gatewayStreamRequest(
  path: string,
  options?: GatewayRequestOptions,
): Promise<Response> {
  const response = await fetchGateway(path, {
    ...options,
    headers: {
      Accept: SSE_CONTENT_TYPE,
      ...options?.headers,
    },
  })

  if (!response.ok) {
    throw await toApiError(response)
  }

  return response
}

/**
 * Managed SSE streaming — reads the response body and dispatches parsed
 * SSE events to the provided callbacks.
 *
 * Returns an `abort` function and the active `AbortSignal`.
 */
export function streamGateway(
  path: string,
  options: GatewayStreamOptions,
): { abort: () => void; signal: AbortSignal } {
  const controller = new AbortController()
  const signal = mergeAbortSignals(controller.signal, options.signal)

  void (async () => {
    try {
      const response = await fetchGateway(path, {
        ...options,
        method: options.method ?? 'POST',
        signal,
        headers: {
          Accept: SSE_CONTENT_TYPE,
          ...options.headers,
        },
      })

      if (!response.ok) throw await toApiError(response)
      const contentType = response.headers.get('Content-Type') ?? ''
      if (!contentType.includes(SSE_CONTENT_TYPE)) {
        throw new ApiError({
          code: 'invalid_stream_response',
          message: 'Expected text/event-stream response',
          status: response.status,
          requestId: response.headers.get('X-Request-Id') ?? undefined,
        })
      }
      if (!response.body) {
        throw new ApiError({
          code: 'empty_stream_response',
          message: 'Response body is not readable',
          status: response.status,
          requestId: response.headers.get('X-Request-Id') ?? undefined,
        })
      }

      await readSseStream(response.body, options.onEvent, signal)
      options.onDone?.()
    } catch (error) {
      if (signal.aborted) return
      options.onError?.(
        error instanceof ApiError
          ? error
          : new ApiError({
              code: 'network_error',
              message: error instanceof Error ? error.message : 'Network error',
              status: 0,
            }),
      )
    }
  })()

  return { abort: () => controller.abort(), signal }
}

function mergeAbortSignals(primary: AbortSignal, secondary?: AbortSignal | null): AbortSignal {
  if (!secondary) return primary
  const controller = new AbortController()
  const abort = (signal: AbortSignal) => controller.abort(signal.reason)

  if (primary.aborted) {
    abort(primary)
    return controller.signal
  }
  if (secondary.aborted) {
    abort(secondary)
    return controller.signal
  }

  primary.addEventListener('abort', () => abort(primary), { once: true })
  secondary.addEventListener('abort', () => abort(secondary), { once: true })
  return controller.signal
}
