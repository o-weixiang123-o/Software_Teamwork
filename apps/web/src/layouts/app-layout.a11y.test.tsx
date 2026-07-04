import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AnchorHTMLAttributes, ReactNode, Ref } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { resetAppVersionFreshnessCacheForTests } from '@/api/app-version'
import { AppVersionBadge } from '@/components/common/app-version-badge'
import { APP_UPDATE_COMMAND } from '@/lib/app-version'
import type { UserSummary } from '@/lib/types'
import { useAuthStore } from '@/stores/auth-store'
import { renderWithProviders } from '@/test/render'

import { AppLayout } from './app-layout'

const routerMocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  pathname: '/chat',
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    activeProps: _activeProps,
    children,
    inactiveProps: _inactiveProps,
    ref,
    to,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & {
    activeProps?: unknown
    children?: ReactNode
    inactiveProps?: unknown
    ref?: Ref<HTMLAnchorElement>
    to: string
  }) => (
    <a
      {...props}
      href={to}
      ref={ref}
      onClick={(event) => {
        event.preventDefault()
        routerMocks.navigate({ to })
      }}
    >
      {children}
    </a>
  ),
  useRouter: () => ({ navigate: routerMocks.navigate }),
  useRouterState: () => ({ location: { pathname: routerMocks.pathname } }),
}))

const user: UserSummary = {
  id: 'user-1',
  permissions: [
    'qa:use',
    'report:read',
    'report:write',
    'knowledge:read',
    'knowledge:write',
    'system:admin',
  ],
  roles: ['system:admin'],
  username: 'operator',
}

const standardUser: UserSummary = {
  id: 'user-2',
  permissions: ['qa:use', 'knowledge:read', 'document:upload'],
  roles: ['standard'],
  username: 'standard',
}

const reportUser: UserSummary = {
  ...standardUser,
  permissions: ['qa:use', 'knowledge:read', 'report:read'],
}

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    status: init?.status ?? 200,
    statusText: init?.statusText,
  })
}

describe('AppLayout accessibility smoke', () => {
  beforeEach(() => {
    resetAppVersionFreshnessCacheForTests()
    routerMocks.navigate.mockReset()
    routerMocks.pathname = '/chat'
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
      window.setTimeout(() => callback(0), 0),
    )
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id))
    useAuthStore.setState({
      accessToken: 'opaque-test-token',
      error: null,
      status: 'authenticated',
      user,
      userName: user.username,
    })
  })

  it('keeps top navigation links reachable and activatable from the keyboard', async () => {
    const keyboard = userEvent.setup()

    renderWithProviders(
      <AppLayout>
        <section aria-label="workspace">Workspace</section>
      </AppLayout>,
    )

    const nav = screen.getByRole('navigation')
    const navLinks = within(nav).getAllByRole('link')
    const currentLabel = screen.getByText('智能问答')
    const versionButton = screen.getByRole('button', { name: /^前端版本 v\d+\.\d+\.\d+/ })
    const helpButton = screen.getByRole('button', { name: '打开帮助' })
    const logoutButton = screen.getByRole('button', { name: '退出登录' })

    expect(navLinks).toHaveLength(5)
    navLinks.forEach((link) => {
      expect(link).toHaveAccessibleName(/.+/)
    })
    expect(currentLabel.compareDocumentPosition(versionButton)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )
    expect(versionButton).toHaveTextContent(/^v\d+\.\d+\.\d+$/)
    expect(logoutButton).toHaveAccessibleName(/.+/)

    await keyboard.tab()
    expect(versionButton).toHaveFocus()
    await keyboard.tab()
    expect(navLinks[0]).toHaveFocus()
    await keyboard.tab()
    expect(navLinks[1]).toHaveFocus()
    await keyboard.keyboard('{Enter}')
    expect(routerMocks.navigate).toHaveBeenCalledWith({ to: '/qa/retrieval-test' })
    await keyboard.tab()
    expect(navLinks[2]).toHaveFocus()
    await keyboard.keyboard('{Enter}')
    expect(routerMocks.navigate).toHaveBeenCalledWith({ to: '/knowledge/search' })
    await keyboard.tab()
    expect(navLinks[3]).toHaveFocus()
    await keyboard.keyboard('{Enter}')
    expect(routerMocks.navigate).toHaveBeenCalledWith({ to: '/reports' })
    await keyboard.tab()
    expect(navLinks[4]).toHaveFocus()
    await keyboard.tab()
    expect(helpButton).toHaveFocus()
    await keyboard.tab()
    expect(screen.getByRole('link', { name: '打开个人资料' })).toHaveFocus()
    await keyboard.tab()
    expect(logoutButton).toHaveFocus()
  })

  it('exposes the admin shell to standard users with read-only knowledge access', () => {
    useAuthStore.setState({
      accessToken: 'opaque-test-token',
      error: null,
      status: 'authenticated',
      user: standardUser,
      userName: standardUser.username,
    })

    renderWithProviders(
      <AppLayout>
        <section aria-label="workspace">Workspace</section>
      </AppLayout>,
    )

    const nav = screen.getByRole('navigation')
    expect(within(nav).getByRole('link', { name: '问答' })).toBeVisible()
    expect(within(nav).getByRole('link', { name: '检索' })).toBeVisible()
    expect(within(nav).getByRole('link', { name: '知识' })).toBeVisible()
    expect(within(nav).getByRole('link', { name: '管理' })).toBeVisible()
  })

  it('exposes the admin shell to users with admin report routes', () => {
    useAuthStore.setState({
      accessToken: 'opaque-test-token',
      error: null,
      status: 'authenticated',
      user: reportUser,
      userName: reportUser.username,
    })

    renderWithProviders(
      <AppLayout>
        <section aria-label="workspace">Workspace</section>
      </AppLayout>,
    )

    const nav = screen.getByRole('navigation')
    expect(within(nav).getByRole('link', { name: '管理' })).toBeVisible()
  })

  it('uses a stable fallback label when the version source is empty', () => {
    renderWithProviders(<AppVersionBadge version="" />)

    expect(screen.getByRole('button', { name: /^前端版本 v0\.0\.0/ })).toHaveTextContent('v0.0.0')
  })

  it('checks version freshness through Gateway without requesting the GitHub API', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        data: {
          checkedAt: '2026-07-03T12:00:00Z',
          currentSha: 'abcdef123456',
          latestSha: 'abcdef123456',
          latestUrl: 'https://github.com/example/repo/commit/abcdef123456',
          status: 'current',
        },
        requestId: 'req-app-version',
      }),
    )
    vi.stubGlobal('fetch', fetcher)
    const pointer = userEvent.setup()

    renderWithProviders(<AppVersionBadge currentSha="abcdef123456" />)

    await pointer.click(screen.getByRole('button', { name: /^前端版本/ }))

    expect(await screen.findByText('本地构建版本')).toBeVisible()
    expect(await screen.findByText('已包含 develop 最新提交')).toBeVisible()
    expect(screen.getByText('打开 GitHub 对比')).toBeVisible()
    expect(screen.getByText(APP_UPDATE_COMMAND)).toBeVisible()
    expect(fetcher).toHaveBeenCalledTimes(1)

    const request = fetcher.mock.calls[0]?.[0]
    expect(request).toBeInstanceOf(Request)
    if (!(request instanceof Request)) throw new Error('expected fetch to receive a Request')
    expect(request.url).toBe(
      'http://127.0.0.1/api/v1/app-version/freshness?currentSha=abcdef123456',
    )
    expect(request.url).not.toContain('api.github.com')
    expect(request.headers.get('Authorization')).toBeNull()

    await pointer.click(screen.getByRole('button', { name: /^前端版本/ }))
    await pointer.click(screen.getByRole('button', { name: /^前端版本/ }))
    await waitFor(() => expect(screen.getByText('已包含 develop 最新提交')).toBeVisible())
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('falls back gracefully when the Gateway freshness route is not ok', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
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
    vi.stubGlobal('fetch', fetcher)
    const pointer = userEvent.setup()

    renderWithProviders(<AppVersionBadge currentSha="abcdef123456" />)

    await pointer.click(screen.getByRole('button', { name: /^前端版本/ }))

    expect(await screen.findByText('无法判断当前构建状态')).toBeVisible()
    expect(screen.getByText('Gateway 没有返回可用提交号，稍后会自动重试。')).toBeVisible()
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(warnSpy).toHaveBeenCalledWith('[app-version] freshness check fallback: http_403')
  })
})
