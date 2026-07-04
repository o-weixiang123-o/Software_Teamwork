import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/client'
import type { KnowledgeBaseSummary, KnowledgeQuerySummary } from '@/api/knowledge'
import { listKnowledgeBases, runKnowledgeQuery } from '@/api/knowledge'
import { renderWithProviders } from '@/test/render'

import { KnowledgeSearchPage } from './page'

vi.mock('@/api/knowledge', () => ({
  listKnowledgeBases: vi.fn(),
  runKnowledgeQuery: vi.fn(),
}))

const knowledgeBases: KnowledgeBaseSummary[] = [
  {
    chunkCount: 4,
    chunkStrategy: { chunkSize: 800, chunkOverlap: 100 },
    createdAt: '2026-07-01T00:00:00Z',
    description: '真实 Gateway 返回的知识库',
    docType: 'GENERAL',
    documentCount: 1,
    id: 'kb-real',
    name: '真实规程库',
    retrievalStrategy: { mode: 'semantic' },
    updatedAt: '2026-07-01T00:00:00Z',
  },
]

const queryResult: KnowledgeQuerySummary = {
  id: 'query-1',
  query: '变压器',
  results: [
    {
      chunkId: 'chunk-1',
      contentPreview: '变压器运行规程内容',
      documentId: 'doc-1',
      documentName: '变压器运行规程.pdf',
      knowledgeBaseId: 'kb-real',
      score: 0.91,
      tags: ['规程'],
    },
  ],
  trace: {
    embeddingDimension: 1024,
    embeddingModel: 'bge-m3',
    embeddingProvider: 'ai-gateway',
    hitCount: 1,
    rerank: false,
    searchTopK: 10,
    scoreThreshold: 0,
  },
}

describe('KnowledgeSearchPage', () => {
  beforeEach(() => {
    vi.mocked(listKnowledgeBases).mockResolvedValue({
      items: knowledgeBases,
      page: { page: 1, pageSize: 100, total: 1 },
    })
    vi.mocked(runKnowledgeQuery).mockResolvedValue(queryResult)
  })

  it('renders real knowledge query results from Gateway wrapper data', async () => {
    renderWithProviders(<KnowledgeSearchPage />)

    expect(await screen.findByText('真实规程库')).toBeVisible()
    fireEvent.change(screen.getByPlaceholderText('输入检索关键词或问题...'), {
      target: { value: '变压器' },
    })
    fireEvent.click(screen.getByRole('button', { name: '检索' }))

    expect(await screen.findByText('变压器运行规程.pdf')).toBeVisible()
    expect(screen.getByText('变压器运行规程内容')).toBeVisible()
    expect(screen.getByText(/命中 1 条结果/)).toBeVisible()
    expect(runKnowledgeQuery).toHaveBeenCalledWith({
      knowledgeBaseIds: undefined,
      query: '变压器',
      rerank: false,
      rerankTopN: undefined,
      scoreThreshold: 0,
      tags: undefined,
      topK: 10,
    })
  })

  it('clears stale results and shows capability errors when a later query fails', async () => {
    vi.mocked(runKnowledgeQuery)
      .mockResolvedValueOnce(queryResult)
      .mockRejectedValueOnce(
        new ApiError({
          code: 'not_implemented',
          message: 'knowledge query route is staged',
          requestId: 'req-query-501',
          status: 501,
        }),
      )

    renderWithProviders(<KnowledgeSearchPage />)

    await screen.findByText('真实规程库')
    fireEvent.change(screen.getByPlaceholderText('输入检索关键词或问题...'), {
      target: { value: '变压器' },
    })
    fireEvent.click(screen.getByRole('button', { name: '检索' }))
    expect(await screen.findByText('变压器运行规程.pdf')).toBeVisible()

    fireEvent.change(screen.getByPlaceholderText('输入检索关键词或问题...'), {
      target: { value: '断路器' },
    })
    fireEvent.click(screen.getByRole('button', { name: '检索' }))

    expect(await screen.findByText(/知识检索暂未就绪/)).toBeVisible()
    expect(screen.getByText(/req-query-501/)).toBeVisible()
    await waitFor(() => {
      expect(screen.queryByText('变压器运行规程.pdf')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('变压器运行规程内容')).not.toBeInTheDocument()
  })

  it('shows knowledge-base loading failures without rendering fake options', async () => {
    vi.mocked(listKnowledgeBases).mockRejectedValue(
      new ApiError({
        code: 'dependency_error',
        message: 'knowledge service unavailable',
        requestId: 'req-kb-down',
        status: 502,
      }),
    )

    renderWithProviders(<KnowledgeSearchPage />)

    expect(await screen.findByText(/knowledge service unavailable/)).toBeVisible()
    expect(screen.getByText(/req-kb-down/)).toBeVisible()
    expect(screen.queryByText('真实规程库')).not.toBeInTheDocument()
    expect(screen.queryByText('示例知识库')).not.toBeInTheDocument()
  })
})
