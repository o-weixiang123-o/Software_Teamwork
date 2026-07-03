import { describe, expect, it } from 'vitest'

import type { UploadStateData } from '@/components/chat'
import type { QAMessage, QASession, SessionAttachmentSummary } from '@/lib/types'

import {
  finalizeThinkingStepsOnAnswerCompleted,
  isReusableEmptyNewSession,
  sanitizeCitation,
  sanitizeReasoningDelta,
  sanitizeThinkingStep,
  type ToolThinkingStep,
  upsertReasoningStep,
} from './page'

const now = '2026-07-03T00:00:00.000Z'
const idleUploadState: UploadStateData = { phase: 'idle' }

function makeSession(overrides: Partial<QASession> = {}): QASession {
  return {
    id: 'session-1',
    title: '新对话',
    status: 'active',
    messageCount: 0,
    lastMessagePreview: '',
    createdAt: now,
    updatedAt: now,
    ...overrides,
  }
}

function makeMessage(overrides: Partial<QAMessage> = {}): QAMessage {
  return {
    id: 'message-1',
    sessionId: 'session-1',
    role: 'user',
    status: 'completed',
    content: '已有问题',
    createdAt: now,
    ...overrides,
  }
}

function makeAttachment(
  overrides: Partial<SessionAttachmentSummary> = {},
): SessionAttachmentSummary {
  return {
    id: 'attachment-1',
    sessionId: 'session-1',
    filename: 'guide.pdf',
    contentType: 'application/pdf',
    sizeBytes: 128,
    status: 'ready',
    createdAt: now,
    ...overrides,
  }
}

function reusableParams(
  overrides: Partial<Parameters<typeof isReusableEmptyNewSession>[0]> = {},
): Parameters<typeof isReusableEmptyNewSession>[0] {
  return {
    activeId: 'session-1',
    attachmentsBySession: {},
    inputText: '',
    messagesBySession: {},
    sessions: [makeSession()],
    uploadSessionId: null,
    uploadState: idleUploadState,
    ...overrides,
  }
}

describe('isReusableEmptyNewSession', () => {
  it('allows reusing the active empty default conversation', () => {
    expect(isReusableEmptyNewSession(reusableParams())).toBe(true)
  })

  it('rejects missing or renamed active conversations', () => {
    expect(isReusableEmptyNewSession(reusableParams({ activeId: null }))).toBe(false)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          sessions: [makeSession({ title: '变压器巡检' })],
        }),
      ),
    ).toBe(false)
  })

  it('rejects conversations with server or local messages', () => {
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          sessions: [makeSession({ messageCount: 1 })],
        }),
      ),
    ).toBe(false)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          messagesBySession: { 'session-1': [makeMessage()] },
        }),
      ),
    ).toBe(false)
  })

  it('rejects visible attachments but ignores failed or purged attachments', () => {
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          attachmentsBySession: { 'session-1': [makeAttachment()] },
        }),
      ),
    ).toBe(false)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          attachmentsBySession: {
            'session-1': [
              makeAttachment({ id: 'failed-1', status: 'failed' }),
              makeAttachment({ id: 'purged-1', status: 'purged' }),
            ],
          },
        }),
      ),
    ).toBe(true)
  })

  it('rejects draft text, current-session upload progress, and active streaming messages', () => {
    expect(isReusableEmptyNewSession(reusableParams({ inputText: '未发送草稿' }))).toBe(false)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          uploadSessionId: 'session-1',
          uploadState: { phase: 'uploading', filename: 'guide.pdf' },
        }),
      ),
    ).toBe(false)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          messagesBySession: {
            'session-1': [
              makeMessage({
                content: '',
                id: 'streaming-assistant',
                role: 'assistant',
                status: 'streaming',
              }),
            ],
          },
        }),
      ),
    ).toBe(false)
  })

  it('allows reuse while another session owns upload progress', () => {
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          uploadSessionId: 'session-2',
          uploadState: { phase: 'uploading', filename: 'guide.pdf' },
        }),
      ),
    ).toBe(true)
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          uploadState: {
            attachment: makeAttachment({ sessionId: 'session-2', status: 'parsing' }),
            attempts: 0,
            phase: 'polling',
          },
        }),
      ),
    ).toBe(true)
  })

  it('rejects current-session polling progress', () => {
    expect(
      isReusableEmptyNewSession(
        reusableParams({
          uploadState: {
            attachment: makeAttachment({ sessionId: 'session-1', status: 'parsing' }),
            attempts: 0,
            phase: 'polling',
          },
        }),
      ),
    ).toBe(false)
  })
})

describe('ChatPage stream thinking helpers', () => {
  it('extracts safe reasoning delta text from supported payload fields', () => {
    expect(sanitizeReasoningDelta({ text: '第一段推理' })).toBe('第一段推理')
    expect(sanitizeReasoningDelta({ content: '第二段推理' })).toBe('第二段推理')
    expect(sanitizeReasoningDelta({ delta: '第三段推理' })).toBe('第三段推理')
  })

  it('drops reasoning delta chunks that contain sensitive internals', () => {
    expect(sanitizeReasoningDelta({ text: 'system prompt: keep API key sk-secret' })).toBe('')
    expect(sanitizeReasoningDelta({ text: 'internal URL http://10.0.0.2/provider' })).toBe('')
  })

  it('drops reasoning delta chunks that become sensitive only after concatenation', () => {
    expect(sanitizeReasoningDelta({ text: '-secret' }, 'sk')).toBe('')
    expect(sanitizeReasoningDelta({ text: '://10.0.0.2/provider' }, 'http')).toBe('')
  })

  it('keeps same-type reasoning steps separated across iterations', () => {
    const first = sanitizeThinkingStep({
      detail: '第 1 轮生成',
      iterationNo: 1,
      label: '生成回答',
      status: 'done',
      type: 'generation',
    })
    const second = sanitizeThinkingStep({
      detail: '第 2 轮生成',
      iterationNo: 2,
      label: '生成回答',
      status: 'done',
      type: 'generation',
    })

    const steps = upsertReasoningStep(upsertReasoningStep([], first), second)

    expect(steps).toHaveLength(2)
    expect(steps.map((step) => step.iterationNo)).toEqual([1, 2])
    expect(steps.map((step) => step.detail)).toEqual(['第 1 轮生成', '第 2 轮生成'])
  })

  it('updates only a matching stable reasoning step in the same iteration', () => {
    const started = sanitizeThinkingStep({
      id: 'reason-1',
      iterationNo: 1,
      label: '校验答案',
      status: 'running',
      type: 'verify',
    })
    const completed = sanitizeThinkingStep({
      id: 'reason-1',
      iterationNo: 1,
      label: '校验答案',
      status: 'done',
      type: 'verify',
    })
    const nextIteration = sanitizeThinkingStep({
      id: 'reason-1',
      iterationNo: 2,
      label: '校验答案',
      status: 'running',
      type: 'verify',
    })

    const updated = upsertReasoningStep(upsertReasoningStep([], started), completed)
    const withNextIteration = upsertReasoningStep(updated, nextIteration)

    expect(updated).toHaveLength(1)
    expect(updated[0]).toMatchObject({ iterationNo: 1, status: 'done' })
    expect(withNextIteration).toHaveLength(2)
    expect(withNextIteration[1]).toMatchObject({ iterationNo: 2, status: 'running' })
  })

  it('does not mark running tool calls as done when the answer completes', () => {
    const steps: ToolThinkingStep[] = [
      {
        iterationNo: 1,
        label: 'Agent 迭代 1',
        status: 'running',
        type: 'agent_iteration',
      },
      {
        iterationNo: 1,
        label: '调用: report_generator',
        status: 'running',
        toolCallId: 'tool-1',
        type: 'tool_call',
      },
    ]

    const finalized = finalizeThinkingStepsOnAnswerCompleted(steps)

    expect(finalized[0]).toMatchObject({ status: 'done', type: 'agent_iteration' })
    expect(finalized[1]).toMatchObject({ status: 'running', type: 'tool_call' })
  })
})

describe('QA chat citation sanitizing', () => {
  it('normalizes legacy citation document aliases', () => {
    const citation = sanitizeCitation({
      citationNo: 1,
      docId: 'doc-legacy',
      docName: 'Legacy Manual.pdf',
      id: 'cite-1',
      isSourceAvailable: true,
      messageId: 'msg-1',
      score: 0.91,
      text: 'quoted text',
    })

    expect(citation.documentId).toBe('doc-legacy')
    expect(citation.documentName).toBe('Legacy Manual.pdf')
    expect(citation.docId).toBe('doc-legacy')
    expect(citation.docName).toBe('Legacy Manual.pdf')
    expect(citation.isSourceAvailable).toBe(true)
  })

  it('keeps canonical citation document fields preferred over aliases', () => {
    const citation = sanitizeCitation({
      docId: 'doc-legacy',
      docName: 'Legacy Manual.pdf',
      documentId: 'doc-canonical',
      documentName: 'Canonical Manual.pdf',
      id: 'cite-1',
      messageId: 'msg-1',
    })

    expect(citation.documentId).toBe('doc-canonical')
    expect(citation.documentName).toBe('Canonical Manual.pdf')
    expect(citation.docId).toBe('doc-legacy')
    expect(citation.docName).toBe('Legacy Manual.pdf')
  })

  it('preserves citation source detail from streamed citation events', () => {
    const citation = sanitizeCitation({
      citationNo: 2,
      content: 'full saved source excerpt',
      documentId: 'doc-1',
      documentName: 'Relay Manual.pdf',
      id: 'cite-2',
      isSourceAvailable: false,
      messageId: 'msg-1',
      source: {
        available: false,
        reason: 'source_deleted_or_forbidden',
      },
    })

    expect((citation as { content?: string }).content).toBe('full saved source excerpt')
    expect((citation as { source?: { available?: boolean; reason?: string } }).source).toEqual({
      available: false,
      reason: 'source_deleted_or_forbidden',
    })
  })

  it('preserves wrapped citation payloads from QA SSE events', () => {
    const citation = sanitizeCitation({
      citation: {
        citationNo: 3,
        chunkId: 'chunk-1',
        content: 'streamed citation excerpt',
        contentPreview: 'streamed citation preview',
        documentId: 'doc-2',
        documentName: 'Wrapped Manual.pdf',
        id: 'cite-3',
        isSourceAvailable: true,
        knowledgeBaseId: 'kb-1',
        messageId: 'msg-2',
        source: {
          available: true,
          downloadEndpoint: '/api/v1/documents/doc-2/content?knowledgeBaseId=kb-1',
        },
      },
    })

    expect(citation.documentId).toBe('doc-2')
    expect(citation.documentName).toBe('Wrapped Manual.pdf')
    expect(citation.chunkId).toBe('chunk-1')
    expect(citation.knowledgeBaseId).toBe('kb-1')
    expect((citation as { content?: string }).content).toBe('streamed citation excerpt')
    expect(
      (citation as { source?: { available?: boolean; downloadEndpoint?: string } }).source,
    ).toEqual({
      available: true,
      downloadEndpoint: '/api/v1/documents/doc-2/content?knowledgeBaseId=kb-1',
      reason: undefined,
    })
  })
})
