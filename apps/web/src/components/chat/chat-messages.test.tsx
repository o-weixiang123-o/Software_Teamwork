import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type {
  QACitation,
  QACitationDetail,
  QAMessage,
  QAMessageWithReasoning,
  QAReportArtifact,
  QAThinkingStep,
} from '@/lib/types'
import { renderWithProviders } from '@/test/render'

import ChatMessages from './chat-messages'

type TestThinkingStep = QAThinkingStep & {
  argumentsSummary?: unknown
  completedAt?: number
  errorSummary?: string
  iterationNo?: number
  reportArtifact?: QAReportArtifact
  resultSummary?: unknown
  startedAt?: number
  toolCallId?: string
  toolName?: string
}

const lookupCitations = vi.fn<(ids: string[]) => Promise<QACitationDetail[]>>()
const getDocumentContent = vi.fn<(documentId: string, knowledgeBaseId: string) => Promise<Blob>>()
const downloadFromUrl = vi.fn()

vi.mock('@/api/citations', () => ({
  lookupCitations: (ids: string[]) => lookupCitations(ids),
}))

vi.mock('@/api/knowledge', () => ({
  getDocumentContent: (documentId: string, knowledgeBaseId: string) =>
    getDocumentContent(documentId, knowledgeBaseId),
}))

vi.mock('@/lib/download', () => ({
  downloadFromUrl: (url: string, filename?: string) => downloadFromUrl(url, filename),
}))

function assistantWithThinking(thinking: TestThinkingStep[], content = '回答正文'): QAMessage {
  return {
    content,
    createdAt: '2026-07-03T00:00:00.000Z',
    id: 'assistant-1',
    role: 'assistant',
    sessionId: 'session-1',
    status: 'streaming',
    thinking,
  }
}

function renderMessage(message: QAMessage, onArtifactDownload = vi.fn()) {
  return renderWithProviders(
    <ChatMessages
      messages={[message]}
      streaming={message.status === 'streaming'}
      error={null}
      onArtifactDownload={onArtifactDownload}
    />,
  )
}

function reasoningMessage(
  reasoningContent: string,
  overrides: Partial<QAMessageWithReasoning> = {},
): QAMessageWithReasoning {
  return {
    content: overrides.content ?? '',
    createdAt: '2026-07-03T00:00:00.000Z',
    id: 'assistant-1',
    reasoningContent,
    role: 'assistant',
    sessionId: 'session-1',
    status: 'streaming',
    thinking: [],
    ...overrides,
  }
}

function citation(overrides: Partial<QACitation> = {}): QACitation {
  const citationNo = overrides.citationNo ?? 1
  return {
    citationNo,
    contentPreview: `preview ${citationNo}`,
    documentId: 'doc-1',
    documentName: 'Transformer Manual.pdf',
    id: `cite-${citationNo}`,
    isSourceAvailable: true,
    knowledgeBaseId: 'kb-1',
    messageId: 'msg-1',
    score: 0.92,
    text: `quote ${citationNo}`,
    ...overrides,
  }
}

function assistantMessage(content: string, citations: QACitation[]): QAMessage {
  return {
    citations,
    content,
    createdAt: '2026-07-03T00:00:00Z',
    id: 'msg-1',
    role: 'assistant',
    sessionId: 'sess-1',
    status: 'completed',
  } as QAMessage
}

function renderChat(content: string, citations: QACitation[]) {
  return renderWithProviders(
    <ChatMessages
      error={null}
      messages={[assistantMessage(content, citations)]}
      streaming={false}
    />,
  )
}

function firstCitationTrigger(label: string): HTMLElement {
  const trigger = screen.getAllByLabelText(label)[0]
  if (!trigger) throw new Error(`Missing citation trigger: ${label}`)
  return trigger
}

describe('ChatMessages ThinkPanel', () => {
  it('renders streamed reasoning content incrementally with lightweight Markdown', () => {
    const initial = reasoningMessage('先判断问题类型')
    const view = renderMessage(initial)

    expect(screen.getByText('💭 深度思考')).toBeInTheDocument()
    expect(screen.getByText('先判断问题类型')).toBeInTheDocument()

    view.rerender(
      <ChatMessages
        messages={[
          reasoningMessage('先判断问题类型\n\n**再检索**安全摘要', {
            content: '回答生成中',
          }),
        ]}
        streaming
        error={null}
      />,
    )

    expect(screen.getByText('再检索')).toBeInTheDocument()
    expect(screen.getByText(/安全摘要/)).toBeInTheDocument()
  })

  it('hides the deep reasoning panel when no reasoning content is present', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'running', type: 'agent_iteration' },
        {
          iterationNo: 1,
          label: 'search_knowledge 执行中',
          status: 'running',
          type: 'tool_call',
        },
      ]),
    )

    expect(screen.queryByText('💭 深度思考')).not.toBeInTheDocument()
    expect(screen.getByText('🔧 工具调用')).toBeInTheDocument()
  })

  it('auto-collapses the reasoning panel three seconds after completion', async () => {
    vi.useFakeTimers()
    try {
      const view = renderMessage(reasoningMessage('整理安全推理摘要'))
      const trigger = screen.getByRole('button', { name: /思考过程/ })

      expect(trigger).toHaveAttribute('aria-expanded', 'true')

      await act(async () => {
        view.rerender(
          <ChatMessages
            messages={[
              reasoningMessage('整理安全推理摘要', {
                content: '回答完成',
                status: 'completed',
              }),
            ]}
            streaming={false}
            error={null}
          />,
        )
      })

      expect(trigger).toHaveAttribute('aria-expanded', 'true')

      await act(async () => {
        vi.advanceTimersByTime(3000)
      })

      expect(screen.getByRole('button', { name: /思考过程/ })).toHaveAttribute(
        'aria-expanded',
        'false',
      )
    } finally {
      vi.useRealTimers()
    }
  })

  it('groups a single iteration with one expandable tool call', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'running', type: 'agent_iteration' },
        {
          argumentsSummary: { query: '变压器巡检', topK: 5 },
          completedAt: 1700000000900,
          iterationNo: 1,
          label: 'search_knowledge 完成',
          resultSummary: {
            citations: [{ internalUrl: 'http://10.0.0.2/private' }, { title: '公开摘要' }],
            hitCount: 3,
          },
          startedAt: 1700000000000,
          status: 'done',
          toolCallId: 'tool-1',
          toolName: 'search_knowledge',
          type: 'tool_call',
        },
      ]),
    )

    expect(screen.getByText('第 1 轮')).toBeInTheDocument()
    expect(screen.getByText(/1 个工具调用/)).toBeInTheDocument()
    expect(screen.getByText('耗时 900ms')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /search_knowledge 完成/ }))

    expect(screen.getByText('参数')).toBeInTheDocument()
    expect(screen.getByText('查询词')).toBeInTheDocument()
    expect(screen.getByText('变压器巡检')).toBeInTheDocument()
    expect(screen.getByText('TopK')).toBeInTheDocument()
    expect(screen.getByText('结果')).toBeInTheDocument()
    expect(screen.getByText('命中数')).toBeInTheDocument()
    expect(screen.getByText('引用数')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.queryByText(/10\.0\.0\.2/)).not.toBeInTheDocument()
  })

  it('renders multiple tool calls in the same iteration as a parallel list', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'running', type: 'agent_iteration' },
        {
          iterationNo: 1,
          label: 'search_knowledge 执行中',
          startedAt: 1700000000000,
          status: 'running',
          toolCallId: 'tool-1',
          type: 'tool_call',
        },
        {
          iterationNo: 1,
          label: 'search_session_attachments 执行中',
          startedAt: 1700000000005,
          status: 'running',
          toolCallId: 'tool-2',
          type: 'tool_call',
        },
      ]),
    )

    expect(screen.getByText(/2 个工具调用/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /search_knowledge 执行中/ })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /search_session_attachments 执行中/ }),
    ).toBeInTheDocument()
    expect(screen.getAllByText('▊')).toHaveLength(2)
  })

  it('keeps multiple ReAct iterations visually separated', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'done', type: 'agent_iteration' },
        {
          completedAt: 1700000000300,
          iterationNo: 1,
          label: 'search_knowledge 完成',
          startedAt: 1700000000000,
          status: 'done',
          type: 'tool_call',
        },
        { iterationNo: 2, label: 'Agent 迭代 2', status: 'running', type: 'agent_iteration' },
        {
          completedAt: 1700000000900,
          iterationNo: 2,
          label: 'get_citation_source 完成',
          startedAt: 1700000000500,
          status: 'done',
          type: 'tool_call',
        },
      ]),
    )

    expect(screen.getByText('第 1 轮')).toBeInTheDocument()
    expect(screen.getByText('第 2 轮')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /search_knowledge 完成/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /get_citation_source 完成/ })).toBeInTheDocument()
  })

  it('renders non-tool thinking steps that are included in the step count', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'running', type: 'agent_iteration' },
        {
          detail: '已整理引用快照',
          iterationNo: 1,
          label: '整理引用',
          status: 'done',
          type: 'citation',
        },
        {
          detail: '正在生成回答',
          iterationNo: 1,
          label: '生成回答',
          status: 'running',
          type: 'generation',
        },
      ]),
    )

    expect(screen.getByText('思考过程 (3 步)')).toBeInTheDocument()
    expect(screen.getByText('整理引用')).toBeInTheDocument()
    expect(screen.getByText('已整理引用快照')).toBeInTheDocument()
    expect(screen.getByText('生成回答')).toBeInTheDocument()
    expect(screen.getByText('正在生成回答')).toBeInTheDocument()
  })

  it('shows a live elapsed timer while the assistant response is loading', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-03T00:00:05.000Z'))

    try {
      renderMessage({
        content: '',
        createdAt: '2026-07-03T00:00:00.000Z',
        id: 'assistant-1',
        role: 'assistant',
        sessionId: 'session-1',
        status: 'streaming',
        thinking: [],
      })

      expect(screen.getByText(/正在生成 · 已等待 00:05/)).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('highlights failed tool calls and shows a safe failure summary', () => {
    renderMessage(
      assistantWithThinking([
        { iterationNo: 1, label: 'Agent 迭代 1', status: 'running', type: 'agent_iteration' },
        {
          completedAt: 1700000000400,
          errorSummary: '依赖服务暂时不可用',
          iterationNo: 1,
          label: 'search_knowledge 失败',
          startedAt: 1700000000000,
          status: 'failed',
          toolCallId: 'tool-1',
          type: 'tool_call',
        },
      ]),
    )

    expect(screen.getByText(/有失败/)).toBeInTheDocument()
    const trigger = screen.getByRole('button', { name: /search_knowledge 失败/ })
    expect(within(trigger).getByText('失败')).toBeInTheDocument()

    fireEvent.click(trigger)

    expect(screen.getByText(/失败原因：依赖服务暂时不可用/)).toBeInTheDocument()
  })

  it('omits ThinkPanel when a pure text answer has no tool or reasoning steps', () => {
    renderMessage({
      content: '纯文本回答',
      createdAt: '2026-07-03T00:00:00.000Z',
      id: 'assistant-1',
      role: 'assistant',
      sessionId: 'session-1',
      status: 'completed',
      thinking: [],
    })

    expect(screen.queryByText(/思考过程/)).not.toBeInTheDocument()
    expect(screen.getByText('纯文本回答')).toBeInTheDocument()
  })

  it('keeps report artifacts visible at message level after completion', () => {
    const onArtifactDownload = vi.fn()
    const artifact: QAReportArtifact = {
      artifactType: 'report_generation',
      downloadPath: '/api/v1/report-files/file-1/content',
      fileStatus: 'succeeded',
      filename: '巡检报告.docx',
      jobStatus: 'succeeded',
      reportId: 'report-1',
      reportName: '巡检报告',
    }

    renderMessage(
      {
        ...assistantWithThinking(
          [
            {
              label: 'report_generator 完成',
              reportArtifact: artifact,
              status: 'done',
              type: 'tool_call',
            },
          ],
          '报告已生成',
        ),
        artifacts: [artifact],
        status: 'completed',
      } as QAMessage & { artifacts: QAReportArtifact[] },
      onArtifactDownload,
    )

    expect(screen.getByText('巡检报告')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /下载报告/ }))

    expect(onArtifactDownload).toHaveBeenCalledWith(
      '/api/v1/report-files/file-1/content',
      '巡检报告.docx',
    )
  })
})

describe('ChatMessages citations', () => {
  it('renders GitHub-flavored Markdown tables', () => {
    renderChat('| 项目 | 标准 |\n| --- | --- |\n| 油温 | 不超过 85°C |', [])

    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '项目' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: '不超过 85°C' })).toBeInTheDocument()
  })

  it('renders answer citation markers as inline buttons', () => {
    renderChat('巡检需要记录油温 [1]', [citation()])

    expect(screen.getAllByLabelText('查看引用 [1]').length).toBeGreaterThan(0)
  })

  it('renders discrete markers and leaves invalid markers as text', () => {
    renderChat('参考 [1][3][5]，不要链接 [99]', [
      citation({ citationNo: 1, id: 'cite-1' }),
      citation({ citationNo: 3, id: 'cite-3' }),
      citation({ citationNo: 5, id: 'cite-5' }),
    ])

    expect(screen.getAllByLabelText('查看引用 [1]').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('查看引用 [3]').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('查看引用 [5]').length).toBeGreaterThan(0)
    expect(screen.queryByLabelText('查看引用 [99]')).not.toBeInTheDocument()
    expect(screen.getByText(/不要链接 \[99\]/)).toBeInTheDocument()
  })

  it('merges adjacent same-document citation markers', () => {
    renderChat('连续引用 [1][2][3]', [
      citation({ citationNo: 1, id: 'cite-1' }),
      citation({ citationNo: 2, id: 'cite-2' }),
      citation({ citationNo: 3, id: 'cite-3' }),
    ])

    expect(screen.getByLabelText('查看引用 [1-3]')).toBeInTheDocument()
  })

  it('does not merge adjacent citations from different documents', () => {
    renderChat('不同来源 [1][2]', [
      citation({ citationNo: 1, documentId: 'doc-1', id: 'cite-1' }),
      citation({ citationNo: 2, documentId: 'doc-2', id: 'cite-2' }),
    ])

    expect(screen.queryByLabelText('查看引用 [1-2]')).not.toBeInTheDocument()
    expect(screen.getAllByLabelText('查看引用 [1]').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('查看引用 [2]').length).toBeGreaterThan(0)
  })

  it('loads citation detail when the popover opens', async () => {
    lookupCitations.mockResolvedValueOnce([
      {
        ...citation(),
        content: '完整原文内容',
        context: '上下文内容',
        pageNumber: 7,
        source: { available: true, downloadEndpoint: '/api/v1/documents/doc-1/content' },
      },
    ])

    renderChat('查看详情 [1]', [citation({ text: undefined })])
    fireEvent.click(firstCitationTrigger('查看引用 [1]'))

    expect(await screen.findByText('完整原文内容')).toBeInTheDocument()
    expect(screen.getByText('页码 7')).toBeInTheDocument()
  })

  it('downloads original document when the citation source is available', async () => {
    lookupCitations.mockResolvedValueOnce([
      {
        ...citation(),
        content: '完整原文内容',
        source: { available: true, downloadEndpoint: '/api/v1/documents/doc-1/content' },
      },
    ])
    getDocumentContent.mockResolvedValueOnce(new Blob(['source']))
    vi.stubGlobal('URL', {
      createObjectURL: vi.fn(() => 'blob:source'),
      revokeObjectURL: vi.fn(),
    })

    renderChat('可下载 [1]', [citation()])
    fireEvent.click(firstCitationTrigger('查看引用 [1]'))
    fireEvent.click(await screen.findByRole('button', { name: '下载原文' }))

    await waitFor(() => {
      expect(getDocumentContent).toHaveBeenCalledWith('doc-1', 'kb-1')
      expect(downloadFromUrl).toHaveBeenCalledWith('blob:source', 'Transformer Manual.pdf')
    })
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()

    await waitFor(
      () => {
        expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:source')
      },
      { timeout: 1500 },
    )
  })

  it('hides the download button and shows the reason when the source is unavailable', async () => {
    lookupCitations.mockResolvedValueOnce([
      {
        ...citation({ isSourceAvailable: false }),
        source: { available: false, reason: 'source_deleted_or_forbidden' },
      },
    ])

    renderChat('不可下载 [1]', [citation({ isSourceAvailable: false })])
    fireEvent.click(firstCitationTrigger('查看引用 [1]'))

    expect(await screen.findByText(/source_deleted_or_forbidden/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载原文' })).not.toBeInTheDocument()
  })

  it('lets citation details override stale source availability summaries', async () => {
    lookupCitations.mockResolvedValueOnce([
      {
        ...citation({ isSourceAvailable: true }),
        content: '瀹屾暣鍘熸枃鍐呭',
        source: { available: false, reason: 'source_revoked' },
      },
    ])

    renderChat('鏉冮檺宸插彉鏇?[1]', [citation({ isSourceAvailable: true, text: undefined })])
    fireEvent.click(screen.getAllByLabelText('查看引用 [1]')[0]!)

    expect(await screen.findByText(/source_revoked/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载原文' })).not.toBeInTheDocument()
  })
})

describe('ChatMessages auto-scroll', () => {
  it('keeps at most one pending scroll frame and cancels it on unmount', () => {
    let nextFrameId = 1
    const frames = new Map<number, FrameRequestCallback>()
    const requestAnimationFrame = vi.fn((callback: FrameRequestCallback) => {
      const frameId = nextFrameId++
      frames.set(frameId, callback)
      return frameId
    })
    const cancelAnimationFrame = vi.fn((frameId: number) => {
      frames.delete(frameId)
    })
    vi.stubGlobal('requestAnimationFrame', requestAnimationFrame)
    vi.stubGlobal('cancelAnimationFrame', cancelAnimationFrame)

    const view = renderChat('第一段', [])
    expect(frames.size).toBe(1)

    view.rerender(
      <ChatMessages
        error={null}
        messages={[assistantMessage('第一段和第二段', [])]}
        streaming={false}
      />,
    )

    expect(requestAnimationFrame).toHaveBeenCalledTimes(2)
    expect(cancelAnimationFrame).toHaveBeenCalledTimes(1)
    expect(frames.size).toBe(1)

    view.unmount()
    expect(cancelAnimationFrame).toHaveBeenCalledTimes(2)
    expect(frames.size).toBe(0)
  })
})
