import { ArrowUpRight } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { replayEvents, streamChat } from '@/api/chat'
import { gatewayFileRequest } from '@/api/client'
import {
  deleteSessionAttachment,
  getSessionAttachment,
  listSessionAttachments,
  listSessions,
} from '@/api/conversations'
import {
  AttachmentList,
  AttachmentUploadStatus,
  ChatInput,
  ChatMessages,
  ChatSidebar,
  type UploadStateData,
  useAttachmentUpload,
} from '@/components/chat'
import { ConfirmDialog } from '@/components/common'
import {
  useCreateSession,
  useDeleteSession,
  useRenameSession,
  useSessionMessages,
  useSessions,
} from '@/features/qa'
import {
  getToolEventSummary,
  getToolReportArtifact,
  mergeMessageReportArtifact,
} from '@/features/qa/capability'
import { downloadFromUrl } from '@/lib/download'
import { isModelConfigurationError, MODEL_CONFIGURATION_HINT } from '@/lib/model-config-errors'
import { AnimationFrameBatcher, StreamingTextController } from '@/lib/streaming-text'
import type {
  QACitation,
  QACitationDetail,
  QAMessage,
  QAMessageWithArtifacts,
  QAMessageWithReasoning,
  QAReportArtifact,
  QASession,
  QASessionListItem,
  QAThinkingStep,
  SessionAttachmentSummary,
} from '@/lib/types'
import { useChatStore } from '@/stores/chat-store'

// ══════════════════════════════════════════════════════════════════════════════
// Helpers
// ══════════════════════════════════════════════════════════════════════════════

const NEW_QA_SESSION_TITLE = '新对话'
const THINK_PANEL_ANSWER_DELAY_MS = 1000

function nextId(): string {
  const cryptoSource = globalThis.crypto
  if (typeof cryptoSource?.randomUUID === 'function') {
    return cryptoSource.randomUUID()
  }
  if (typeof cryptoSource?.getRandomValues !== 'function') {
    throw new Error('Secure random generator unavailable')
  }
  const bytes = cryptoSource.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

type ReusableEmptyNewSessionParams = {
  activeId: string | null
  attachmentsBySession: Record<string, SessionAttachmentSummary[]>
  inputText: string
  messagesBySession: Record<string, QAMessage[]>
  sessions: QASession[]
  uploadSessionId: string | null
  uploadState: UploadStateData
}

function isUploadStateForSession(
  uploadState: UploadStateData,
  uploadSessionId: string | null,
  sessionId: string,
): boolean {
  if (uploadState.phase === 'uploading') return uploadSessionId === sessionId
  if (uploadState.phase === 'polling') return uploadState.attachment.sessionId === sessionId
  return false
}

export function isReusableEmptyNewSession({
  activeId,
  attachmentsBySession,
  inputText,
  messagesBySession,
  sessions,
  uploadSessionId,
  uploadState,
}: ReusableEmptyNewSessionParams): boolean {
  if (!activeId) return false
  if (isUploadStateForSession(uploadState, uploadSessionId, activeId)) return false
  if (inputText.trim().length > 0) return false

  const session = sessions.find((item) => item.id === activeId)
  if (!session || session.status !== 'active') return false

  const title = session.title?.trim() ?? ''
  if (title.length > 0 && title !== NEW_QA_SESSION_TITLE) return false
  if ((session.messageCount ?? 0) > 0) return false
  if ((session.lastMessagePreview ?? '').trim().length > 0) return false

  const localMessages = messagesBySession[activeId] ?? []
  if (localMessages.length > 0) return false

  const visibleAttachments = (attachmentsBySession[activeId] ?? []).filter(
    (attachment) => attachment.status !== 'failed' && attachment.status !== 'purged',
  )
  return visibleAttachments.length === 0
}

function toSessionListItem(s: QASession, messages: QAMessage[]): QASessionListItem {
  const last = messages[messages.length - 1]
  return {
    id: s.id,
    title: s.title,
    status: s.status,
    messageCount: messages.length > 0 ? messages.length : (s.messageCount ?? 0),
    lastMessagePreview: last ? last.content.slice(0, 50) : (s.lastMessagePreview ?? ''),
    createdAt: s.createdAt,
    updatedAt: s.updatedAt,
  }
}

async function listAllSessionIds(): Promise<string[]> {
  const pageSize = 50
  const ids = new Set<string>()

  for (let page = 1; ; page += 1) {
    const result = await listSessions({ page, pageSize })
    for (const session of result.items) {
      ids.add(session.id)
    }
    if (
      result.items.length === 0 ||
      result.items.length < pageSize ||
      ids.size >= result.page.total
    ) {
      break
    }
  }

  return Array.from(ids)
}

// ══════════════════════════════════════════════════════════════════════════════
// SSE data sanitizers — strip internal fields before rendering in UI
// Per OpenAPI: thinking.detail must only contain user-visible summary,
// never chain-of-thought, full prompts, tool args, raw results, internal
// URLs, or object keys.
// ══════════════════════════════════════════════════════════════════════════════

const SENSITIVE_PATTERN =
  /https?:\/\/|s3:\/\/|gs:\/\/|minio:\/\/|localhost|127\.\d+\.\d+\.\d+|10\.\d+\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+|192\.168\.\d+\.\d+|sk-|api_key|token|Bearer\s|secret|password|credential|object_key|internal:/i

function sanitizeLabel(raw: string | undefined): string | undefined {
  if (typeof raw !== 'string' || raw.length === 0) return undefined
  const trimmed = raw.slice(0, 200)
  if (SENSITIVE_PATTERN.test(trimmed)) return undefined
  return trimmed
}

export function sanitizeReasoningDelta(raw: Record<string, unknown>, existingContent = ''): string {
  const value =
    typeof raw.text === 'string'
      ? raw.text
      : typeof raw.content === 'string'
        ? raw.content
        : typeof raw.delta === 'string'
          ? raw.delta
          : typeof raw.reasoning === 'string'
            ? raw.reasoning
            : ''
  if (!value) return ''
  const trimmed = value.slice(0, 4000)
  if (SENSITIVE_PATTERN.test(trimmed)) return ''
  if (SENSITIVE_PATTERN.test(`${existingContent}${trimmed}`)) return ''
  return trimmed
}

function sanitizeDetail(raw: string | undefined): string | undefined {
  if (typeof raw !== 'string' || raw.length === 0) return undefined
  const trimmed = raw.slice(0, 4000)
  if (SENSITIVE_PATTERN.test(trimmed)) return undefined
  return trimmed
}

const VALID_STEP_TYPES = new Set([
  'agent_iteration',
  'tool_call',
  'tool_result',
  'generation',
  'citation',
  'verify',
])

export type ToolThinkingStep = QAThinkingStep & {
  argumentsSummary?: unknown
  completedAt?: number
  errorSummary?: string
  iterationNo?: number
  reasoningStepId?: string
  reportArtifact?: QAReportArtifact
  resultSummary?: unknown
  startedAt?: number
  toolCallId?: string
  toolName?: string
}

type AssistantMessagePatch = {
  artifacts?: QAReportArtifact[]
  citations?: QACitation[]
  content?: string
  id?: string
  reasoningContent?: string
  status?: QAMessage['status']
  thinking?: QAThinkingStep[]
}

export function sanitizeThinkingStep(raw: Record<string, unknown>): ToolThinkingStep {
  const rawType = String(raw.type ?? '')
  // Only allow known step types; discard unknown / internal-only types
  const type = (VALID_STEP_TYPES.has(rawType) ? rawType : 'generation') as QAThinkingStep['type']
  const label = sanitizeLabel(typeof raw.label === 'string' ? raw.label : undefined)
  const status = (
    ['pending', 'running', 'done', 'failed'].includes(String(raw.status))
      ? String(raw.status)
      : 'running'
  ) as QAThinkingStep['status']
  const detail = sanitizeDetail(typeof raw.detail === 'string' ? raw.detail : undefined)
  const iterationNo = getIterationNo(raw)
  const rawReasoningStepId =
    typeof raw.reasoningStepId === 'string'
      ? raw.reasoningStepId
      : typeof raw.stepId === 'string'
        ? raw.stepId
        : typeof raw.id === 'string'
          ? raw.id
          : undefined
  const reasoningStepId = sanitizeLabel(rawReasoningStepId)
  return { type, label, status, detail, iterationNo, reasoningStepId }
}

// ══════════════════════════════════════════════════════════════════════════════
// Safe SSE error message — map internal codes to user-visible Chinese text.
// Never expose raw backend messages, object keys, or internal URLs.
// ══════════════════════════════════════════════════════════════════════════════

const SAFE_ERROR_MAP: Record<string, string> = {
  network_error: '网络连接失败，请检查后端服务是否启动',
  dependency_error: '部分服务暂时不可用，回答可能不完整',
  invalid_sse_event: '收到异常数据，回答已中断',
  stream_ended_without_completion: '连接意外断开，回答不完整',
  no_body: '服务未返回有效响应',
  finalize_failed: '回答保存失败，但内容已生成',
}

function sanitizeErrorMessage(raw: string | undefined): string {
  if (!raw || raw.length === 0) return '请求失败，请稍后重试'
  const trimmed = raw.slice(0, 200)
  // If the message looks like it contains internal data, use a generic fallback
  if (SENSITIVE_PATTERN.test(trimmed)) return '服务异常，请稍后重试'
  return trimmed
}

function formatError(sseErr: {
  code?: string
  fields?: Record<string, string>
  message?: string
}): string {
  if (isModelConfigurationError(sseErr)) return MODEL_CONFIGURATION_HINT
  if (sseErr.code) {
    const mapped = SAFE_ERROR_MAP[sseErr.code]
    if (mapped) return mapped
  }
  return sanitizeErrorMessage(sseErr.message)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function sanitizeCitation(raw: Record<string, unknown>): QACitation {
  const payload = isRecord(raw.citation) ? raw.citation : raw
  const documentId =
    typeof payload.documentId === 'string'
      ? payload.documentId
      : typeof payload.docId === 'string'
        ? payload.docId
        : undefined
  const documentName =
    typeof payload.documentName === 'string'
      ? payload.documentName
      : typeof payload.docName === 'string'
        ? payload.docName
        : undefined

  const isSourceAvailable =
    typeof payload.isSourceAvailable === 'boolean' ? payload.isSourceAvailable : undefined
  const rawSource = isRecord(payload.source) ? payload.source : undefined
  const sourceReason =
    rawSource && typeof rawSource.reason === 'string'
      ? rawSource.reason
      : typeof payload.sourceUnavailableReason === 'string'
        ? payload.sourceUnavailableReason
        : undefined
  const sourceDownloadEndpoint =
    rawSource && typeof rawSource.downloadEndpoint === 'string'
      ? rawSource.downloadEndpoint
      : undefined
  const sourceAvailable =
    rawSource && typeof rawSource.available === 'boolean' ? rawSource.available : isSourceAvailable

  // Keep only display-safe fields per OpenAPI QACitation schema. The backend
  // may include QACitationDetail fields in citation.delta, so preserve the
  // already-sanitized source snapshot for immediate streaming display.
  const citation: QACitation & Pick<QACitationDetail, 'content' | 'source'> = {
    id: String(payload.id ?? ''),
    messageId: String(payload.messageId ?? ''),
    citationNo: typeof payload.citationNo === 'number' ? payload.citationNo : undefined,
    chunkId: typeof payload.chunkId === 'string' ? payload.chunkId : undefined,
    chunkType: typeof payload.chunkType === 'string' ? payload.chunkType : undefined,
    context: typeof payload.context === 'string' ? payload.context : undefined,
    docId: typeof payload.docId === 'string' ? payload.docId : documentId,
    docName: typeof payload.docName === 'string' ? payload.docName : documentName,
    documentName,
    knowledgeBaseId:
      typeof payload.knowledgeBaseId === 'string' ? payload.knowledgeBaseId : undefined,
    pageNumber: typeof payload.pageNumber === 'number' ? payload.pageNumber : undefined,
    rerankScore: typeof payload.rerankScore === 'number' ? payload.rerankScore : undefined,
    sectionPath: typeof payload.sectionPath === 'string' ? payload.sectionPath : undefined,
    text: typeof payload.text === 'string' ? payload.text : undefined,
    score: typeof payload.score === 'number' ? payload.score : undefined,
    contentPreview: typeof payload.contentPreview === 'string' ? payload.contentPreview : undefined,
    documentId,
    isSourceAvailable,
  }
  if (typeof payload.content === 'string') {
    citation.content = payload.content
  }
  if (sourceAvailable !== undefined) {
    citation.source = {
      available: sourceAvailable,
      downloadEndpoint: sourceDownloadEndpoint,
      reason: sourceReason,
    }
  }
  return citation as QACitation
}

function sanitizeToolName(raw: unknown): string {
  if (typeof raw !== 'string' || raw.length === 0) return '工具调用'
  // Truncate and strip sensitive patterns (URLs, IPs, tokens, keys)
  const trimmed = raw.slice(0, 80)
  if (SENSITIVE_PATTERN.test(trimmed)) return '检索工具'
  return trimmed
}

function getToolName(data: Record<string, unknown>): string {
  return sanitizeToolName(data.toolName ?? data.tool)
}

function getIterationNo(data: Record<string, unknown>): number | undefined {
  return typeof data.iterationNo === 'number' && Number.isFinite(data.iterationNo)
    ? data.iterationNo
    : undefined
}

function getToolFailureSummary(data: Record<string, unknown>): string | undefined {
  const raw =
    typeof data.errorMessage === 'string'
      ? data.errorMessage
      : typeof data.error === 'string'
        ? data.error
        : typeof data.summary === 'string'
          ? data.summary
          : undefined
  return sanitizeLabel(raw)
}

function getReasoningStepKey(step: ToolThinkingStep): string | undefined {
  if (!step.reasoningStepId) return undefined
  return `${step.iterationNo ?? 'unknown'}:${step.type}:${step.reasoningStepId}`
}

export function upsertReasoningStep(
  steps: ToolThinkingStep[],
  step: ToolThinkingStep,
): ToolThinkingStep[] {
  const key = getReasoningStepKey(step)
  if (!key) return [...steps, step]

  const idx = steps.findIndex((existing) => getReasoningStepKey(existing) === key)
  if (idx < 0) return [...steps, step]

  const next = [...steps]
  next[idx] = step
  return next
}

export function finalizeThinkingStepsOnAnswerCompleted(
  steps: ToolThinkingStep[],
): ToolThinkingStep[] {
  return steps.map((step) =>
    (step.type === 'agent_iteration' || step.type === 'generation') && step.status === 'running'
      ? { ...step, status: 'done' as const }
      : step,
  )
}

const SUGGESTED_PROMPTS = [
  '变压器巡检有哪些要点？',
  '如何判断变压器油是否需要更换？',
  '电力安全工作规程中关于停电操作的规定是什么？',
]

function createMockAssistantMessage(sessionId: string): QAMessage {
  return {
    id: nextId(),
    sessionId,
    role: 'assistant',
    content: `## 变压器巡检要点

根据《电力变压器运行规程》（DL/T 572-2021），变压器巡检是保障电力系统安全运行的关键环节。

### 主要检查项目

1. **油温检查**：运行中油温不得超过 85°C，温升不得超过 55K
2. **油位检查**：油位计指示应正常，无渗漏现象
3. **呼吸器检查**：硅胶变色不超过 2/3，否则需更换
4. **声音检查**：正常运行声音均匀，无异响

### 注意事项

- 巡检周期：重要变电站每周至少一次
- 异常情况应立即上报并记录
- 巡检数据需录入 PMS 系统

\`\`\`
额定油温上限：85°C
报警温度：95°C
跳闸温度：105°C
\`\`\`

> 以上内容仅供参考，具体操作请以最新规程为准。`,
    thinking: [
      { type: 'agent_iteration', label: 'Agent 迭代 1', status: 'done' },
      { type: 'tool_call', label: '检索变压器巡检规程', status: 'done' },
      { type: 'tool_call', label: '检索油温标准参数', status: 'done' },
    ],
    citations: [
      {
        id: 'mock-1',
        messageId: sessionId,
        citationNo: 1,
        documentName: '电力变压器运行规程 DL/T 572-2021',
        text: '变压器运行中油温不得超过85°C，温升不得超过55K。巡检时应记录油温、油位、呼吸器状态等参数。',
        score: 0.95,
      },
      {
        id: 'mock-2',
        messageId: sessionId,
        citationNo: 2,
        documentName: '变电检修导则 Q/GDW 11224-2023',
        text: '重要变电站每周至少巡检一次，一般变电站每月至少一次。异常情况应立即上报。',
        score: 0.87,
      },
      {
        id: 'mock-3',
        messageId: sessionId,
        citationNo: 3,
        documentName: '油浸式变压器运行维护手册',
        text: '硅胶呼吸器的作用是防止变压器油与空气中的水分接触。硅胶变色超过2/3时应及时更换。',
        score: 0.82,
      },
    ],
    status: 'completed',
    createdAt: new Date().toISOString(),
    artifacts: [
      {
        artifactType: 'report_generation',
        reportName: '变压器巡检报告',
        reportType: 'substation_inspection',
        jobStatus: 'succeeded',
        reportId: 'rpt_mock_01',
        fileStatus: 'succeeded',
        filename: '变压器巡检报告.docx',
        preview: {
          title: '变压器巡检报告',
          summary:
            '本次巡检覆盖变压器油温、油位、呼吸器及运行声音等关键项目，所有指标均在正常范围内。',
          outlineTitles: ['巡检概况', '油温与油位检查', '呼吸器状态', '运行声音分析', '结论与建议'],
          progressPercent: 100,
          statusText: '报告已生成',
        },
      } as QAReportArtifact,
    ],
  } as QAMessage & { artifacts?: QAReportArtifact[] }
}

// ══════════════════════════════════════════════════════════════════════════════
// Component
// ══════════════════════════════════════════════════════════════════════════════

export function ChatPage() {
  // ── React Query: sessions list ──
  const {
    data: sessionsData,
    isLoading: sessionsLoading,
    isError: sessionsError,
    refetch: refetchSessions,
  } = useSessions()

  // ── Zustand store ──
  const sessions = useChatStore((s) => s.sessions)
  const setSessions = useChatStore((s) => s.setSessions)
  const activeId = useChatStore((s) => s.activeId)
  const setActiveId = useChatStore((s) => s.setActiveId)
  const streaming = useChatStore((s) => s.streaming)
  const setStreaming = useChatStore((s) => s.setStreaming)
  const error = useChatStore((s) => s.error)
  const setError = useChatStore((s) => s.setError)
  const lastFailedMsg = useChatStore((s) => s.lastFailedMsg)
  const setLastFailedMsg = useChatStore((s) => s.setLastFailedMsg)
  const clearError = useChatStore((s) => s.clearError)
  const addSession = useChatStore((s) => s.addSession)
  const removeSession = useChatStore((s) => s.removeSession)
  const updateSessionMessages = useChatStore((s) => s.updateSessionMessages)
  const appendSessionMessages = useChatStore((s) => s.appendSessionMessages)
  const messagesBySession = useChatStore((s) => s.messagesBySession)
  const attachmentsBySession = useChatStore((s) => s.attachmentsBySession)
  const excludedAttachmentIds = useChatStore((s) => s.excludedAttachmentIds)
  const setSessionAttachments = useChatStore((s) => s.setSessionAttachments)
  const addAttachment = useChatStore((s) => s.addAttachment)
  const updateAttachment = useChatStore((s) => s.updateAttachment)
  const removeAttachment = useChatStore((s) => s.removeAttachment)
  const toggleAttachmentExcluded = useChatStore((s) => s.toggleAttachmentExcluded)

  // ── React Query: messages for active session (loaded separately from QASession) ──
  const { data: serverMessages, isError: messagesError } = useSessionMessages(activeId ?? '')

  // ── Local input text ──
  const [inputText, setInputText] = useState('')

  // ── Three-phase state machine: empty → transitioning → active ──
  const [chatPhase, setChatPhase] = useState<'empty' | 'active'>('empty')

  // ── Attachment upload hook ──
  const handleAttachmentReady = useCallback(
    (attachment: SessionAttachmentSummary) => {
      const sid = attachment.sessionId || activeId
      if (!sid) return
      const current = useChatStore.getState().attachmentsBySession[sid] ?? []
      const tempIds = current.filter((a) => a.id.startsWith('temp-')).map((a) => a.id)
      for (const tempId of tempIds) {
        removeAttachment(sid, tempId)
      }
      const exists = current.some((a) => a.id === attachment.id)
      if (exists) {
        updateAttachment(sid, attachment.id, attachment)
      } else {
        addAttachment(sid, attachment)
      }
    },
    [activeId, updateAttachment, addAttachment, removeAttachment],
  )

  const handleAttachCleanup = useCallback(
    (uploadSessionId: string) => {
      if (!uploadSessionId) return
      const current = useChatStore.getState().attachmentsBySession[uploadSessionId] ?? []
      const tempIds = current.filter((a) => a.id.startsWith('temp-')).map((a) => a.id)
      for (const tempId of tempIds) {
        removeAttachment(uploadSessionId, tempId)
      }
    },
    [removeAttachment],
  )

  const { uploadState, uploadSessionId, uploadFile, dismissUpload } = useAttachmentUpload(
    activeId,
    handleAttachmentReady,
    handleAttachCleanup,
  )

  // ── Mutations ──
  const createSessionMut = useCreateSession()
  const deleteSessionMut = useDeleteSession()
  const renameSessionMut = useRenameSession()

  // ── SSE cleanup ref ──
  const abortRef = useRef<(() => void) | null>(null)
  const streamUiCleanupRef = useRef<(() => void) | null>(null)

  useEffect(
    () => () => {
      streamUiCleanupRef.current?.()
      streamUiCleanupRef.current = null
      abortRef.current?.()
      abortRef.current = null
    },
    [],
  )

  // ── Event replay: track current responseRunId for reconnect recovery ──
  const responseRunIdRef = useRef<string | null>(null)

  // The clear-all confirmation is for this exact server-confirmed collection.
  const clearAllSessionIdsRef = useRef<string[] | null>(null)

  // ── Report artifact download handler ──
  const handleArtifactDownload = useCallback(
    async (downloadPath: string, filename: string) => {
      try {
        // downloadPath is validated full Gateway path; strip /api/v1 prefix
        // since gatewayFileRequest already prepends the gateway base URL
        const relativePath = downloadPath.startsWith('/api/v1')
          ? downloadPath.slice('/api/v1'.length)
          : downloadPath
        const blob = await gatewayFileRequest(relativePath)
        const url = URL.createObjectURL(blob)
        downloadFromUrl(url, filename)
        setTimeout(() => URL.revokeObjectURL(url), 1000)
      } catch {
        setError('报告文件下载失败')
      }
    },
    [setError],
  )

  // ── FLIP animation: input box from center → bottom ──
  const inputAreaRef = useRef<HTMLDivElement>(null)
  const flipFromRef = useRef<DOMRect | null>(null)

  // ══════════════════════════════════════════════════════════════════════════
  // Refresh recovery: sync server list into store
  // ══════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (sessionsData?.items) {
      const currentSessions = useChatStore.getState().sessions
      const merged: QASession[] = sessionsData.items.map((item) => {
        const existing = currentSessions.find((s) => s.id === item.id)
        if (existing) {
          // Preserve existing metadata; update title/status/updatedAt from server
          return { ...existing, title: item.title, status: item.status, updatedAt: item.updatedAt }
        }
        return {
          id: item.id,
          title: item.title,
          status: item.status,
          messageCount: item.messageCount,
          lastMessagePreview: item.lastMessagePreview,
          createdAt: item.createdAt,
          updatedAt: item.updatedAt,
        }
      })
      setSessions(merged)
    }
  }, [sessionsData, setSessions])

  // ══════════════════════════════════════════════════════════════════════════
  // Fetch active session messages from server (for refresh recovery)
  // ══════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (serverMessages?.items && activeId) {
      const current = useChatStore.getState().messagesBySession[activeId]
      // Only overwrite if local messages are empty (don't clobber streaming data)
      if (!current || current.length === 0) {
        if (serverMessages.items.length > 0) {
          updateSessionMessages(activeId, serverMessages.items)
          // TODO: recover report artifacts from GET /api/v1/response-runs/
          //       {responseRunId}/tool-calls resultSummary.reportArtifact
          //       once responseRunId is persisted on QAMessage.
        }
      }
    }
  }, [serverMessages, activeId, updateSessionMessages])

  // ══════════════════════════════════════════════════════════════════════════
  // Surface messages fetch error when local messages are empty
  // ══════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (messagesError && activeId) {
      const local = useChatStore.getState().messagesBySession[activeId]
      if (!local || local.length === 0) {
        setError('加载会话消息失败，请检查网络连接')
      }
    }
  }, [messagesError, activeId, setError])

  // ══════════════════════════════════════════════════════════════════════════
  // Load attachments when active session changes
  // ══════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (!activeId) return

    listSessionAttachments(activeId)
      .then((result) => {
        // Merge: server list is authoritative for real attachments. Only keep
        // local items that are temp-* or still in-flight (uploaded/parsing)
        // and not yet visible in the server response. Stale ready/failed items
        // missing from server (deleted in another tab, TTL purge, etc.) are
        // dropped so they won't be included in the next message send.
        const serverIds = new Set(result.items.map((a) => a.id))
        const local = useChatStore.getState().attachmentsBySession[activeId] ?? []
        const localOnly = local.filter(
          (a) =>
            !serverIds.has(a.id) &&
            (a.id.startsWith('temp-') || a.status === 'uploaded' || a.status === 'parsing'),
        )
        setSessionAttachments(activeId, [...result.items, ...localOnly])
      })
      .catch(() => {
        // Attachments are optional; don't surface loading errors as critical
      })
  }, [activeId, setSessionAttachments])

  // ══════════════════════════════════════════════════════════════════════════
  // Background polling for non-terminal attachments (uploaded / parsing)
  // ══════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (!activeId) return

    const currentAttachments = attachmentsBySession[activeId] ?? []
    const pollingIds = currentAttachments
      .filter(
        (a) => (a.status === 'uploaded' || a.status === 'parsing') && !a.id.startsWith('temp-'),
      )
      .map((a) => a.id)

    if (pollingIds.length === 0) return

    const interval = setInterval(() => {
      const sessionId = activeId
      if (!sessionId) return

      for (const id of pollingIds) {
        getSessionAttachment(sessionId, id)
          .then((updated) => {
            updateAttachment(sessionId, id, updated)
          })
          .catch(() => {
            // Silently continue polling
          })
      }
    }, 2000)

    return () => clearInterval(interval)
  }, [activeId, attachmentsBySession, updateAttachment])

  // ══════════════════════════════════════════════════════════════════════════
  // Derive sidebar items (merge sessions + messages for display)
  // ══════════════════════════════════════════════════════════════════════════

  const sidebarItems: QASessionListItem[] = useMemo(
    () =>
      sessions.map((s) => {
        const msgs = messagesBySession[s.id] ?? []
        return toSessionListItem(s, msgs)
      }),
    [sessions, messagesBySession],
  )

  // ══════════════════════════════════════════════════════════════════════════
  // Attachment handlers
  // ══════════════════════════════════════════════════════════════════════════

  const handleFileSelect = useCallback(
    (file: File) => {
      if (!activeId) return

      // Clean up any stale temp-* items from a previous aborted upload before
      // adding the new one, so they don't accumulate in the list indefinitely.
      const current = useChatStore.getState().attachmentsBySession[activeId] ?? []
      for (const a of current) {
        if (a.id.startsWith('temp-')) {
          removeAttachment(activeId, a.id)
        }
      }

      // Optimistically add the attachment to the store
      const tempAttachment: SessionAttachmentSummary = {
        id: `temp-${Date.now()}`,
        sessionId: activeId,
        filename: file.name,
        contentType: file.type || 'application/octet-stream',
        sizeBytes: file.size,
        status: 'uploaded',
        createdAt: new Date().toISOString(),
      }
      addAttachment(activeId, tempAttachment)

      // Start the actual upload + polling flow
      uploadFile(file)
    },
    [activeId, addAttachment, removeAttachment, uploadFile],
  )

  const [deleteAttachmentTarget, setDeleteAttachmentTarget] = useState<{
    sessionId: string
    attachmentId: string
  } | null>(null)

  const handleDeleteAttachment = useCallback((sessionId: string, attachmentId: string) => {
    setDeleteAttachmentTarget({ sessionId, attachmentId })
  }, [])

  const confirmDeleteAttachment = useCallback(async () => {
    const target = deleteAttachmentTarget
    setDeleteAttachmentTarget(null)
    if (!target) return
    const { sessionId: targetSid, attachmentId: targetAid } = target
    // temp-* attachments only exist locally — skip backend call and just clean up
    if (targetAid.startsWith('temp-')) {
      removeAttachment(targetSid, targetAid)
      // Only dismiss the upload bar if this temp belongs to the current session
      if (targetSid === activeId) dismissUpload()
      return
    }
    try {
      await deleteSessionAttachment(targetSid, targetAid)
      removeAttachment(targetSid, targetAid)
    } catch {
      setError('删除附件失败')
    }
  }, [activeId, deleteAttachmentTarget, removeAttachment, setError, dismissUpload])

  const handleToggleAttachmentExcluded = useCallback(
    (attachmentId: string) => {
      if (!activeId) return
      toggleAttachmentExcluded(activeId, attachmentId)
    },
    [activeId, toggleAttachmentExcluded],
  )

  // ── Derived attachment data ──
  const activeAttachments = activeId ? (attachmentsBySession[activeId] ?? []) : []
  const activeExcludedIds = activeId ? (excludedAttachmentIds[activeId] ?? []) : []
  const visibleAttachmentCount = activeAttachments.filter(
    (a) => a.status !== 'failed' && a.status !== 'purged',
  ).length

  // ══════════════════════════════════════════════════════════════════════════
  // Create session
  // ══════════════════════════════════════════════════════════════════════════

  const handleCreate = useCallback(async () => {
    if (createSessionMut.isPending) return

    if (
      isReusableEmptyNewSession({
        activeId,
        attachmentsBySession,
        inputText,
        messagesBySession,
        sessions,
        uploadSessionId,
        uploadState,
      })
    ) {
      setActiveId(activeId)
      return
    }

    try {
      const newSession = await createSessionMut.mutateAsync(NEW_QA_SESSION_TITLE)
      addSession(newSession)
      setActiveId(newSession.id)
    } catch {
      setError('创建会话失败，请检查网络连接')
    }
  }, [
    activeId,
    addSession,
    attachmentsBySession,
    createSessionMut,
    inputText,
    messagesBySession,
    sessions,
    setActiveId,
    setError,
    uploadSessionId,
    uploadState,
  ])

  // ══════════════════════════════════════════════════════════════════════════
  // Delete session (only remove from UI on API success)
  // ══════════════════════════════════════════════════════════════════════════

  const handleDelete = useCallback(
    async (sessionId: string) => {
      try {
        await deleteSessionMut.mutateAsync(sessionId)
        removeSession(sessionId)
      } catch {
        setError('删除会话失败，请检查网络连接')
      }
    },
    [deleteSessionMut, removeSession, setError],
  )

  const handlePrepareClearAll = useCallback(async () => {
    const ids = await listAllSessionIds()
    clearAllSessionIdsRef.current = ids
    return ids.length
  }, [])

  const handleClearAll = useCallback(async () => {
    const ids = clearAllSessionIdsRef.current
    if (!ids) {
      setError('无法确认将要删除的对话总数，请稍后重试')
      return
    }

    let failed = 0

    for (const id of ids) {
      try {
        await deleteSessionMut.mutateAsync(id)
        removeSession(id)
      } catch {
        failed++
      }
    }

    clearAllSessionIdsRef.current = null
    if (failed > 0) setError(`${failed} 个对话删除失败，请稍后重试`)
  }, [deleteSessionMut, removeSession, setError])

  // ══════════════════════════════════════════════════════════════════════════
  // Rename session
  // ══════════════════════════════════════════════════════════════════════════

  const handleRename = useCallback(
    async (sessionId: string, newTitle: string) => {
      try {
        await renameSessionMut.mutateAsync({ sessionId, title: newTitle })
        const current = useChatStore.getState().sessions
        setSessions(current.map((s) => (s.id === sessionId ? { ...s, title: newTitle } : s)))
      } catch {
        setError('重命名会话失败')
      }
    },
    [renameSessionMut, setSessions, setError],
  )

  // ══════════════════════════════════════════════════════════════════════════
  // Send message (SSE streaming)
  // ══════════════════════════════════════════════════════════════════════════

  const sendMessage = useCallback(
    async (text: string) => {
      const trimmed = text.trim()
      if (!trimmed || useChatStore.getState().streaming) return

      clearError()

      // Detect whether we are sending from the empty phase (before any message mutations)
      const preSendState = useChatStore.getState()
      const wasEmpty =
        !preSendState.activeId ||
        (preSendState.messagesBySession[preSendState.activeId!]?.length ?? 0) === 0

      let targetId: string | null = useChatStore.getState().activeId

      // ① Auto-create session if none active
      if (!targetId) {
        const title = trimmed.slice(0, 30) + (trimmed.length > 30 ? '…' : '')
        try {
          const newSession = await createSessionMut.mutateAsync(title)
          addSession(newSession)
          targetId = newSession.id
          setActiveId(targetId)
        } catch (err) {
          const code = (err as { code?: string }).code
          if (code === 'network_error') {
            // Genuinely offline — create local session so mock can still fire
            const localId = nextId()
            addSession({
              id: localId,
              title,
              status: 'active',
              messageCount: 0,
              lastMessagePreview: '',
              createdAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            } as QASession)
            targetId = localId
            setActiveId(localId)
          } else {
            // HTTP error — map code to safe message
            const gqlErr = err as { code?: string; message?: string }
            setError(formatError({ code: gqlErr.code, message: gqlErr.message }))
            return
          }
        }
      }

      const uid: string = targetId

      // ② Push user message + empty assistant message into store
      const userMsg: QAMessage = {
        id: nextId(),
        sessionId: uid,
        role: 'user',
        content: trimmed,
        status: 'completed',
        createdAt: new Date().toISOString(),
      }
      const asstMsg: QAMessage = {
        id: nextId(),
        sessionId: uid,
        role: 'assistant',
        content: '',
        status: 'streaming',
        createdAt: new Date().toISOString(),
        thinking: [],
        citations: [],
      }

      appendSessionMessages(uid, [userMsg, asstMsg])

      // Update session metadata (title for first message)
      useChatStore.setState((state) => ({
        sessions: state.sessions.map((s) => {
          if (s.id !== uid) return s
          const msgs = state.messagesBySession[uid] ?? []
          const isFirst = msgs.length <= 2
          return {
            ...s,
            title: isFirst ? trimmed.slice(0, 30) + (trimmed.length > 30 ? '…' : '') : s.title,
            updatedAt: new Date().toISOString(),
          }
        }),
      }))

      // Trigger FLIP animation if the chat was empty
      if (wasEmpty && inputAreaRef.current) {
        flipFromRef.current = inputAreaRef.current.getBoundingClientRect()
        setChatPhase('active')
      }

      setStreaming(true)
      streamUiCleanupRef.current?.()

      // Accumulators for SSE events
      let content = ''
      let reasoningContent = ''
      let steps: ToolThinkingStep[] = []
      let artifacts: QAReportArtifact[] | undefined
      const reasoningByIteration: Record<number, string> = {}
      const toolStepIndex: Record<string, number> = {}
      const cites: QACitation[] = []

      /**
       * Patch the last assistant message in the active session.
       * Uses Zustand setState with functional updater for latest state.
       */
      const patchAssistant = (patch: AssistantMessagePatch) => {
        useChatStore.setState((state) => {
          const msgs = [...(state.messagesBySession[uid] ?? [])]
          const lastIdx = msgs.length - 1
          const last = msgs[lastIdx]
          if (!last || last.role !== 'assistant') return state
          msgs[lastIdx] = { ...last, ...patch } as QAMessageWithReasoning
          return {
            messagesBySession: {
              ...state.messagesBySession,
              [uid]: msgs,
            },
          }
        })
      }

      const frameBatcher = new AnimationFrameBatcher<AssistantMessagePatch>({
        merge: (current, next) => ({ ...current, ...next }),
        onFlush: patchAssistant,
      })
      let answerCompleted = false
      let answerStartDelayApplied = false
      let completedMessageId: string | undefined

      const finalizeVisibleAnswer = () => {
        if (!answerCompleted) return
        frameBatcher.cancel()
        patchAssistant({
          ...(completedMessageId ? { id: completedMessageId } : {}),
          status: 'completed',
        })
        if (streamUiCleanupRef.current === cancelUiSchedulers) {
          streamUiCleanupRef.current = null
        }
        // Keep the existing same-chunk fatal-error override window.
        queueMicrotask(() => {
          setStreaming(false)
          abortRef.current = null
        })
      }

      const textController = new StreamingTextController({
        maxCharsPerFrame: 100,
        minCharsPerFrame: 2,
        onDone: finalizeVisibleAnswer,
        onUpdate: (visibleContent) => patchAssistant({ content: visibleContent }),
      })

      const delayAnswerStartForThinking = () => {
        if (answerStartDelayApplied) return
        answerStartDelayApplied = true
        textController.delayStart(THINK_PANEL_ANSWER_DELAY_MS)
      }

      function cancelUiSchedulers() {
        textController.cancel()
        frameBatcher.cancel()
        if (streamUiCleanupRef.current === cancelUiSchedulers) {
          streamUiCleanupRef.current = null
        }
      }

      streamUiCleanupRef.current = cancelUiSchedulers

      const queueAssistantPatch = (patch: AssistantMessagePatch) => {
        frameBatcher.push(patch)
      }

      const mergeArtifact = (artifact: QAReportArtifact | undefined) => {
        const message = artifacts ? ({ artifacts } as QAMessageWithArtifacts) : undefined
        artifacts = mergeMessageReportArtifact(message, artifact)
      }

      // Seq verification helper
      let lastSeq = -1
      const verifySeq = (seq: number): boolean => {
        if (seq <= lastSeq) {
          console.warn(`[SSE] Out-of-order event: received seq=${seq}, last=${lastSeq}`)
          return false
        }
        lastSeq = seq
        return true
      }

      // Track whether we've received the first token
      let firstToken = false

      // ③ Initiate SSE stream
      const streamHandlers: Parameters<typeof streamChat>[2] = {
        onMessageCreated(data) {
          if (!verifySeq(data.seq)) return
          // Capture the real message id and responseRunId from the server
          const serverMsgId = data.messageId as string | undefined
          if (serverMsgId) {
            patchAssistant({ id: serverMsgId })
          }
          const runId = data.responseRunId as string | undefined
          if (runId) responseRunIdRef.current = runId
        },
        onAgentIterationStarted(data) {
          if (!verifySeq(data.seq)) return
          const iterationNo = getIterationNo(data)
          const label = iterationNo != null ? `Agent 迭代 ${iterationNo}` : 'Agent 分析中'
          const ex = steps.find(
            (s) =>
              s.type === 'agent_iteration' &&
              s.status === 'running' &&
              (iterationNo == null || s.iterationNo === iterationNo),
          )
          if (!ex) {
            steps.push({
              type: 'agent_iteration',
              label,
              status: 'running',
              iterationNo,
            })
          }
          delayAnswerStartForThinking()
          queueAssistantPatch({ thinking: [...steps] })
        },
        onReasoningStep(data) {
          if (!verifySeq(data.seq)) return
          const raw =
            ((data as Record<string, unknown>).step as Record<string, unknown> | undefined) ?? data
          if (!raw) return
          const safe = sanitizeThinkingStep({
            ...raw,
            iterationNo: raw.iterationNo ?? data.iterationNo,
          })
          steps = upsertReasoningStep(steps, safe)
          delayAnswerStartForThinking()
          queueAssistantPatch({ thinking: [...steps] })
        },
        onReasoningDelta(data) {
          if (!verifySeq(data.seq)) return
          const delta = sanitizeReasoningDelta(data, reasoningContent)
          if (!delta) return
          reasoningContent += delta
          const iterationNo = getIterationNo(data) ?? 1
          reasoningByIteration[iterationNo] = `${reasoningByIteration[iterationNo] ?? ''}${delta}`
          const safe = sanitizeThinkingStep({
            type: 'generation',
            label: '模型思考',
            status: 'running',
            detail: reasoningByIteration[iterationNo],
            iterationNo,
            reasoningStepId: `model-reasoning-${iterationNo}`,
          })
          steps = upsertReasoningStep(steps, safe)
          delayAnswerStartForThinking()
          queueAssistantPatch({
            reasoningContent,
            thinking: [...steps],
            status: 'streaming',
          })
        },
        onToolStarted(data) {
          if (!verifySeq(data.seq)) return
          const toolName = getToolName(data)
          const toolCallId = typeof data.toolCallId === 'string' ? data.toolCallId : undefined
          const iterationNo = getIterationNo(data)
          const idx =
            steps.push({
              type: 'tool_call',
              label: `调用: ${toolName}`,
              status: 'running',
              argumentsSummary: getToolEventSummary(data, 'argumentsSummary'),
              iterationNo,
              startedAt: Date.now(),
              toolCallId,
              toolName,
            }) - 1
          if (toolCallId) toolStepIndex[toolCallId] = idx
          delayAnswerStartForThinking()
          queueAssistantPatch({ thinking: [...steps] })
        },
        onToolCompleted(data) {
          if (!verifySeq(data.seq)) return
          const toolName = getToolName(data)
          const toolCallId = typeof data.toolCallId === 'string' ? data.toolCallId : undefined
          const artifact = getToolReportArtifact(data)
          // Match by toolCallId first, fallback to first running
          let idx = -1
          if (toolCallId && toolStepIndex[toolCallId] !== undefined) {
            idx = toolStepIndex[toolCallId]
          } else {
            idx = steps.findIndex((s) => s.type === 'tool_call' && s.status === 'running')
          }
          const existingStep = idx >= 0 ? steps[idx] : undefined
          if (existingStep) {
            steps[idx] = {
              ...existingStep,
              status: 'done' as const,
              label: `${toolName} 完成`,
              completedAt: Date.now(),
              reportArtifact: artifact,
              resultSummary: getToolEventSummary(data, 'resultSummary'),
              toolName,
            }
          }
          mergeArtifact(artifact)
          delayAnswerStartForThinking()
          queueAssistantPatch({
            artifacts,
            thinking: [...steps],
          })
        },
        onToolFailed(data) {
          if (!verifySeq(data.seq)) return
          const toolName = getToolName(data)
          const toolCallId = typeof data.toolCallId === 'string' ? data.toolCallId : undefined
          const failedArtifact = getToolReportArtifact(data)
          let idx = -1
          if (toolCallId && toolStepIndex[toolCallId] !== undefined) {
            idx = toolStepIndex[toolCallId]
          } else {
            idx = steps.findIndex((s) => s.type === 'tool_call' && s.status === 'running')
          }
          const existingStep = idx >= 0 ? steps[idx] : undefined
          if (existingStep) {
            steps[idx] = {
              ...existingStep,
              status: 'failed' as const,
              label: `${toolName} 失败`,
              completedAt: Date.now(),
              errorSummary: getToolFailureSummary(data),
              reportArtifact: failedArtifact,
              resultSummary: getToolEventSummary(data, 'resultSummary'),
              toolName,
            }
          }
          mergeArtifact(failedArtifact)
          delayAnswerStartForThinking()
          queueAssistantPatch({
            artifacts,
            thinking: [...steps],
          })
        },
        onAnswerDelta(data) {
          if (!verifySeq(data.seq)) return
          if (!firstToken) {
            firstToken = true
            patchAssistant({ status: 'streaming' })
          }
          const delta = (data.content as string) ?? ''
          content += delta
          textController.feed(delta)
        },
        onCitationDelta(data) {
          if (!verifySeq(data.seq)) return
          const raw = (data as Record<string, unknown>).citation as
            Record<string, unknown> | undefined
          if (raw) {
            const safe = sanitizeCitation(raw)
            cites.push(safe)
            queueAssistantPatch({ citations: [...cites] })
          }
        },
        onAnswerCompleted(data) {
          const runId = data.responseRunId as string | undefined
          if (runId) responseRunIdRef.current = runId
          steps = finalizeThinkingStepsOnAnswerCompleted(steps)
          const serverMsgId =
            (data.assistantMessageId as string | undefined) ??
            (data.messageId as string | undefined)
          completedMessageId = typeof serverMsgId === 'string' ? serverMsgId : completedMessageId
          answerCompleted = true
          frameBatcher.cancel()
          patchAssistant({
            artifacts,
            thinking: [...steps],
            citations: [...cites],
            reasoningContent,
            ...(completedMessageId ? { id: completedMessageId } : {}),
          })
          textController.finish()
        },
        onError(sseErr) {
          if (!verifySeq(sseErr.seq)) return
          if (sseErr.fatal) {
            cancelUiSchedulers()
            abortRef.current = null
            // Only insert mock for genuine network failures (backend unreachable).
            // HTTP errors (401/403/404/502) mean the backend is alive — surface them.
            const isOffline =
              !firstToken && steps.length === 0 && !content && sseErr.code === 'network_error'
            // Only write mock if store still belongs to this session
            if (isOffline && useChatStore.getState().activeId === uid) {
              useChatStore.setState((prev) => {
                const msgs = [...(prev.messagesBySession[uid] ?? [])]
                const lastIdx = msgs.length - 1
                const mock = createMockAssistantMessage(uid)
                const lastItem = lastIdx >= 0 ? msgs[lastIdx] : undefined
                if (lastItem?.role === 'assistant') {
                  msgs[lastIdx] = { ...mock, id: lastItem!.id }
                } else {
                  msgs.push(mock)
                }
                return {
                  messagesBySession: { ...prev.messagesBySession, [uid]: msgs },
                  streaming: false,
                  sessions: prev.sessions.map((s) =>
                    s.id === uid ? { ...s, updatedAt: new Date().toISOString() } : s,
                  ),
                }
              })
              abort()
              return
            }
            // Real backend error: surface to user (only if session unchanged)
            if (useChatStore.getState().activeId === uid) {
              setError(formatError(sseErr))
              setLastFailedMsg(trimmed)
            }
            patchAssistant({
              artifacts,
              content,
              thinking: [...steps],
              citations: [...cites],
              reasoningContent,
              status: 'failed',
            })
            abort()
          } else {
            // Non-fatal error: surface a brief summary but keep streaming
            if (useChatStore.getState().activeId === uid) setError(formatError(sseErr))
            console.warn(`[SSE] Non-fatal: ${sseErr.code}`)
            const runId = responseRunIdRef.current
            if (runId) {
              replayEvents(uid, runId)
                .then((events) => {
                  console.warn(`[SSE] Replayed ${events.length} events for run ${runId}`)
                  // Future: replay events into the conversation state
                })
                .catch((err) => {
                  console.error('[SSE] Event replay failed:', err)
                })
            }
          }
        },
        onAbort() {
          cancelUiSchedulers()
          // Only apply partial content if the stream was in-flight.
          // When called after mock/fatal-error, the assistant already has a
          // final status — don't overwrite it with empty accumulators.
          useChatStore.setState((prev) => {
            const msgs = [...(prev.messagesBySession[uid] ?? [])]
            const lastIdx = msgs.length - 1
            const last = lastIdx >= 0 ? msgs[lastIdx] : undefined
            if (!last || last.role !== 'assistant') return prev
            // Already finalised by mock or fatal error — skip
            if (
              last.status === 'completed' ||
              last.status === 'failed' ||
              last.status === 'stopped' ||
              (last.content && last.content.length > 0 && last.status !== 'streaming')
            ) {
              return { streaming: false }
            }
            msgs[lastIdx] = {
              ...last,
              artifacts,
              content,
              thinking: [...steps],
              citations: [...cites],
              reasoningContent,
              status: 'stopped',
            } as QAMessageWithReasoning
            return {
              messagesBySession: { ...prev.messagesBySession, [uid]: msgs },
              streaming: false,
            }
          })
          abortRef.current = null
        },
      }

      // Collect ready attachment IDs at call time (latest store state)
      const currentState = useChatStore.getState()
      const currentAttachments = currentState.attachmentsBySession[uid] ?? []
      const currentExcluded = currentState.excludedAttachmentIds[uid] ?? []
      const attachmentIds = currentAttachments
        .filter(
          (a) =>
            a.status === 'ready' && !currentExcluded.includes(a.id) && !a.id.startsWith('temp-'),
        )
        .map((a) => a.id)

      const { abort } = streamChat(
        uid,
        trimmed,
        streamHandlers,
        undefined,
        attachmentIds.length > 0 ? attachmentIds : undefined,
      )

      abortRef.current = abort
    },
    [
      addSession,
      appendSessionMessages,
      clearError,
      createSessionMut,
      setActiveId,
      setError,
      setLastFailedMsg,
      setStreaming,
    ],
  )

  // ══════════════════════════════════════════════════════════════════════════
  // Retry on error
  // ══════════════════════════════════════════════════════════════════════════

  const handleRetry = useCallback(() => {
    if (lastFailedMsg) {
      const msg = lastFailedMsg
      clearError()
      sendMessage(msg)
    }
  }, [lastFailedMsg, clearError, sendMessage])

  // ══════════════════════════════════════════════════════════════════════════
  // Suggested prompt click
  // ══════════════════════════════════════════════════════════════════════════

  const handleSuggested = useCallback((prompt: string) => {
    setInputText(prompt)
  }, [])

  // ══════════════════════════════════════════════════════════════════════════
  // Active session
  // ══════════════════════════════════════════════════════════════════════════

  const activeMessages = activeId ? (messagesBySession[activeId] ?? []) : []

  // ══════════════════════════════════════════════════════════════════════════════
  // FLIP animation: slide input from center (empty) to bottom (active)
  // ══════════════════════════════════════════════════════════════════════════════

  useLayoutEffect(() => {
    const from = flipFromRef.current
    const el = inputAreaRef.current
    if (!from || !el) return

    // Reset the ref immediately so we don't re-trigger
    flipFromRef.current = null

    // Force a layout read so the browser paints the "Last" position first
    const to = el.getBoundingClientRect()

    const deltaY = from.top - to.top
    const deltaX = from.left - to.left
    const scaleW = to.width > 0 ? from.width / to.width : 1

    if (Math.abs(deltaY) < 2 && Math.abs(deltaX) < 2) return

    // Invert: move element back to its old position
    el.style.transform = `translate(${deltaX}px, ${deltaY}px) scaleX(${scaleW})`

    // Force a synchronous paint so the inverted state is rendered
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions
    el.offsetHeight

    // Play: animate to the new position
    el.style.transition = 'transform 500ms cubic-bezier(0.4, 0, 0.2, 1)'
    el.style.transform = ''

    const onEnd = () => {
      el.style.transition = ''
      el.style.transform = ''
      el.removeEventListener('transitionend', onEnd)
    }
    el.addEventListener('transitionend', onEnd)
  }, [chatPhase])

  // ══════════════════════════════════════════════════════════════════════════════
  // Phase recovery: go back to empty when all messages are cleared
  // ══════════════════════════════════════════════════════════════════════════════

  useEffect(() => {
    if (activeMessages.length > 0 && chatPhase === 'empty') {
      setChatPhase('active')
    } else if (activeMessages.length === 0 && chatPhase === 'active') {
      setChatPhase('empty')
    }
  }, [activeMessages.length, chatPhase])

  // ══════════════════════════════════════════════════════════════════════════
  // Render
  // ══════════════════════════════════════════════════════════════════════════

  return (
    <>
      <div className="flex h-full">
        {/* Left: session sidebar */}
        <ChatSidebar
          sessions={sidebarItems}
          activeId={activeId ?? ''}
          isLoading={sessionsLoading}
          fetchError={sessionsError ? '加载会话列表失败，请检查网络连接' : null}
          onRetryFetch={() => refetchSessions()}
          onSelect={setActiveId}
          onCreate={handleCreate}
          onDelete={handleDelete}
          onRename={handleRename}
          onPrepareClearAll={handlePrepareClearAll}
          onClearAll={handleClearAll}
        />

        {/* Right: main chat area — single input DOM node with FLIP animation */}
        <div className="relative flex min-w-0 flex-1 flex-col pb-4 pl-6 pr-4 md:pl-8 md:pr-6">
          {/* Messages — only when active */}
          {chatPhase === 'active' && (
            <div className="page-enter-right flex min-h-0 flex-1 flex-col">
              <ChatMessages
                messages={activeMessages}
                streaming={streaming}
                error={error}
                onRetry={lastFailedMsg ? handleRetry : undefined}
                onArtifactDownload={handleArtifactDownload}
              />
            </div>
          )}

          {/* Input area — ALWAYS the same DOM node (stable ref for FLIP).
            empty: absolutely positioned at center. active/transitioning: static at bottom. */}
          <div
            className={
              chatPhase === 'empty'
                ? 'absolute inset-0 -mt-20 flex flex-col items-center justify-center gap-5 px-6'
                : 'shrink-0'
            }
          >
            {chatPhase === 'empty' && activeMessages.length === 0 && (
              <h1 className="text-center text-2xl font-semibold tracking-tight text-foreground md:text-3xl">
                今天想了解哪些电力知识？
              </h1>
            )}
            <div
              ref={inputAreaRef}
              className={chatPhase === 'empty' ? 'w-full max-w-5xl' : 'w-full'}
            >
              {/* Attachment upload status indicator */}
              <div className="mb-2">
                <AttachmentUploadStatus
                  sessionId={activeId}
                  state={uploadState}
                  onDismiss={dismissUpload}
                />
              </div>

              {/* Attachment list */}
              <AttachmentList
                attachments={activeAttachments}
                excludedIds={activeExcludedIds}
                onToggleExcluded={handleToggleAttachmentExcluded}
                onDelete={handleDeleteAttachment}
                sessionId={activeId}
              />

              <ChatInput
                onSend={sendMessage}
                disabled={streaming}
                streaming={streaming}
                onStop={() => abortRef.current?.()}
                value={inputText}
                onChange={setInputText}
                size={chatPhase === 'empty' ? 'large' : 'normal'}
                onFileSelect={handleFileSelect}
                onAttachError={(msg) => setError(msg)}
                attachmentCount={visibleAttachmentCount}
                disableAttach={!activeId}
              />
            </div>
            {chatPhase === 'empty' && activeMessages.length === 0 && (
              <div className="flex w-full max-w-5xl flex-wrap justify-center gap-2">
                {SUGGESTED_PROMPTS.map((p, i) => (
                  <button
                    key={p}
                    type="button"
                    className="animate-[fade-in-up_0.4s_ease-out_both] inline-flex items-center rounded-full border border-primary/20 bg-primary/5 px-4 py-2.5 text-sm font-medium text-primary shadow-sm transition-colors hover:border-primary/30 hover:bg-primary/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/25"
                    style={{ animationDelay: `${i * 150}ms` }}
                    onClick={() => handleSuggested(p)}
                  >
                    <ArrowUpRight className="mr-1 inline-block size-3.5 shrink-0" />
                    {p}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
      <ConfirmDialog
        cancelLabel="取消"
        confirmLabel="确认删除"
        description="附件删除后本次对话将无法引用，确认删除？"
        onConfirm={() => void confirmDeleteAttachment()}
        onOpenChange={(open) => {
          if (!open) setDeleteAttachmentTarget(null)
        }}
        open={Boolean(deleteAttachmentTarget)}
        title="确定删除该附件？"
        variant="destructive"
      />
    </>
  )
}
