import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { KnowledgeManagement } from './knowledge-management'

function jsonResponse(body: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    status: init?.status ?? 200,
    statusText: init?.statusText,
  })
}

describe('KnowledgeManagement', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('submits runtime docType values while keeping Chinese labels in the form', async () => {
    const postBodies: unknown[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(input, init)
      const url = new URL(request.url)

      if (request.method === 'GET' && url.pathname.endsWith('/api/v1/knowledge-bases')) {
        return jsonResponse({
          data: [],
          page: { page: 1, pageSize: 10, total: 0 },
          requestId: 'req-kb-list',
        })
      }

      if (request.method === 'POST' && url.pathname.endsWith('/api/v1/knowledge-bases')) {
        const body = await request.clone().json()
        postBodies.push(body)
        return jsonResponse(
          {
            data: {
              chunkCount: 0,
              chunkStrategy: {},
              createdAt: '2026-07-03T00:00:00Z',
              description: '用于验证创建参数',
              docType: 'naive',
              documentCount: 0,
              id: 'kb-created',
              name: '创建参数验证库',
              retrievalStrategy: { mode: 'semantic' },
              updatedAt: '2026-07-03T00:00:00Z',
            },
            requestId: 'req-kb-create',
          },
          { status: 201 },
        )
      }

      return jsonResponse(
        { error: { code: 'not_found', message: 'unexpected route' } },
        { status: 404 },
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<KnowledgeManagement />)

    expect(await screen.findByText('暂无知识库，点击新建知识库开始')).toBeVisible()
    const createButton = screen.getAllByRole('button', { name: '新建知识库' })[0]
    if (!createButton) throw new Error('create button not found')
    fireEvent.click(createButton)
    const dialog = document.querySelector('[data-slot="dialog-content"]') as HTMLElement
    expect(within(dialog).getByRole('heading', { name: '新建知识库' })).toBeVisible()
    expect(within(dialog).getByLabelText('文档类型')).toHaveTextContent('通用文档')

    fireEvent.change(within(dialog).getByLabelText(/名称/), {
      target: { value: '创建参数验证库' },
    })
    fireEvent.change(within(dialog).getByLabelText('描述'), {
      target: { value: '用于验证创建参数' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(postBodies).toHaveLength(1))
    expect(postBodies[0]).toMatchObject({
      description: '用于验证创建参数',
      docType: 'naive',
      name: '创建参数验证库',
      retrievalStrategy: { mode: 'semantic' },
    })
  })
})
