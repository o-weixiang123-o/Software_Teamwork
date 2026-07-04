import { screen } from '@testing-library/react'
import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { UserSummary } from '@/lib/types'
import { useAuthStore } from '@/stores/auth-store'
import { renderWithProviders } from '@/test/render'

import { AdminSidebar } from './admin-sidebar'

const routerMocks = vi.hoisted(() => ({
  pathname: '/admin/knowledge/search',
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & {
    children?: ReactNode
    to: string
  }) => (
    <a {...props} href={to}>
      {children}
    </a>
  ),
  useRouterState: (options?: {
    select?: (state: { location: { pathname: string } }) => unknown
  }) => {
    const state = { location: { pathname: routerMocks.pathname } }
    return options?.select ? options.select(state) : state
  },
}))

const knowledgeReader: UserSummary = {
  id: 'reader-1',
  permissions: ['knowledge:read'],
  roles: ['standard'],
  username: 'reader',
}

describe('AdminSidebar knowledge permissions', () => {
  beforeEach(() => {
    routerMocks.pathname = '/admin/knowledge/search'
    useAuthStore.setState({
      accessToken: 'opaque-test-token',
      error: null,
      status: 'authenticated',
      user: knowledgeReader,
      userName: knowledgeReader.username,
    })
  })

  it('shows read-only knowledge entries without write management actions', () => {
    renderWithProviders(<AdminSidebar />)

    expect(screen.getByRole('link', { name: /知识检索/ })).toBeVisible()
    expect(screen.getByRole('link', { name: /文档管理/ })).toBeVisible()
    expect(screen.queryByRole('link', { name: /知识管理/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /知识配置/ })).not.toBeInTheDocument()
  })
})
