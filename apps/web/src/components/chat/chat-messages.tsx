import { useQuery } from '@tanstack/react-query'
import { Brain, Check, ChevronDown, ChevronRight, Download, Loader2, Wrench } from 'lucide-react'
import { Children, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { lookupCitations } from '@/api/citations'
import { getDocumentContent } from '@/api/knowledge'
import ReportArtifactCard from '@/components/chat/report-artifact-card'
import { InlineNotice } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { downloadFromUrl } from '@/lib/download'
import type {
  QACitation,
  QACitationDetail,
  QAMessage,
  QAMessageWithReasoning,
  QAReportArtifact,
  QAThinkingStep,
} from '@/lib/types'
import { cn } from '@/lib/utils'

import { createCitationMap, parseCitationMarkers } from './citation-markers'

// ══════════════════════════════════════════════════════════════════════════════
// Sub-components
// ══════════════════════════════════════════════════════════════════════════════

type CitationLike = QACitation | QACitationDetail

type MarkdownComponentProps = {
  children?: ReactNode
} & Record<string, unknown>

function citationDocumentId(citation: CitationLike): string {
  return citation.documentId ?? citation.docId ?? ''
}

function citationKnowledgeBaseId(citation: CitationLike): string {
  return citation.knowledgeBaseId ?? ''
}

function citationDocumentName(citation: CitationLike): string {
  return citation.documentName ?? citation.docName ?? '未知文档'
}

function citationPreview(citation: CitationLike): string {
  return citation.text ?? citation.contentPreview ?? ''
}

function citationContent(citation: CitationLike): string {
  return (citation as QACitationDetail).content ?? citationPreview(citation)
}

function truncateText(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text
  return `${text.slice(0, maxLength)}...`
}

function formatPercent(value: number | null | undefined): string | undefined {
  if (typeof value !== 'number') return undefined
  return `${Math.round(value * 100)}%`
}

function downloadFilename(citation: CitationLike): string {
  const name = citationDocumentName(citation).trim()
  if (!name || name === '未知文档') return 'citation-source'
  return name.replace(/[\\/:*?"<>|]+/g, '_')
}

function renderCitationChildren(
  children: ReactNode,
  citationsByNo: Map<number, QACitation>,
): ReactNode {
  return Children.toArray(children).flatMap((child, childIndex) => {
    if (typeof child !== 'string') return child

    return parseCitationMarkers(child, citationsByNo).map((token, tokenIndex) => {
      const key = `${childIndex}-${tokenIndex}`
      if (token.kind === 'text') return token.text
      return <CitationTooltip key={key} citations={token.citations} label={token.label} />
    })
  })
}

/* ── Citation tooltip ── */
function CitationTooltip({
  c,
  citations,
  label,
}: {
  c?: QACitation
  citations?: QACitation[]
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const [downloadError, setDownloadError] = useState<string | null>(null)
  const [downloadingId, setDownloadingId] = useState<string | null>(null)

  const baseCitations = useMemo(() => citations ?? (c ? [c] : []), [c, citations])
  const ids = useMemo(() => baseCitations.map((citation) => citation.id), [baseCitations])
  const shouldLoadDetails =
    open && baseCitations.some((citation) => citation.id && !(citation as QACitationDetail).content)

  const detailsQuery = useQuery({
    enabled: shouldLoadDetails,
    queryFn: () => lookupCitations(ids),
    queryKey: ['qa', 'citation-details', ids],
    staleTime: 60_000,
  })

  const detailsById = useMemo(() => {
    const map = new Map<string, QACitationDetail>()
    for (const detail of detailsQuery.data ?? []) {
      map.set(detail.id, detail)
    }
    return map
  }, [detailsQuery.data])

  const effectiveCitations = baseCitations.map(
    (citation) => detailsById.get(citation.id) ?? citation,
  )
  const onlyCitation = effectiveCitations.length === 1 ? effectiveCitations[0] : undefined
  const displayId =
    label ??
    (onlyCitation?.citationNo != null
      ? `[${onlyCitation.citationNo}]`
      : `[${effectiveCitations
          .map((citation) => citation.citationNo)
          .filter(Boolean)
          .join(',')}]`)

  async function handleDownload(citation: CitationLike) {
    const documentId = citationDocumentId(citation)
    const knowledgeBaseId = citationKnowledgeBaseId(citation)
    if (!documentId || !knowledgeBaseId || downloadingId) return

    setDownloadError(null)
    setDownloadingId(citation.id)
    try {
      const blob = await getDocumentContent(documentId, knowledgeBaseId)
      const url = URL.createObjectURL(blob)
      downloadFromUrl(url, downloadFilename(citation))
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch {
      setDownloadError('下载失败，请稍后重试')
    } finally {
      setDownloadingId(null)
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={`查看引用 ${displayId}`}
        className="mx-0.5 inline-flex align-super rounded-sm bg-accent px-1.5 py-0.5 text-[0.65rem] font-medium leading-none text-primary transition-colors hover:bg-primary hover:text-primary-foreground"
        onClick={(e) => {
          e.stopPropagation()
        }}
      >
        {displayId}
      </PopoverTrigger>
      <PopoverContent className="w-[360px] max-w-[calc(100vw-2rem)] p-0">
        <div className="max-h-[520px] overflow-y-auto p-4">
          {effectiveCitations.length > 1 && (
            <div className="mb-3 rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
              已合并 {effectiveCitations.length} 条引用片段
            </div>
          )}
          {detailsQuery.isLoading && (
            <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="size-3 animate-spin" />
              正在加载引用详情
            </div>
          )}
          {detailsQuery.isError && (
            <div className="mb-3 text-xs text-destructive">引用详情加载失败，已展示摘要。</div>
          )}

          <div className="space-y-4">
            {effectiveCitations.map((citation) => {
              const documentId = citationDocumentId(citation)
              const knowledgeBaseId = citationKnowledgeBaseId(citation)
              const source = (citation as QACitationDetail).source
              const sourceAvailability = source?.available ?? citation.isSourceAvailable
              const sourceAvailable = sourceAvailability === true
              const sourceUnavailable = sourceAvailability === false
              const score = formatPercent(citation.score)
              const rerankScore = formatPercent(citation.rerankScore)
              const content = citationContent(citation)
              const preview = citationPreview(citation)

              return (
                <div
                  key={citation.id}
                  className="space-y-2 border-b border-border/60 pb-3 last:border-0 last:pb-0"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-foreground">
                        {citationDocumentName(citation)}
                      </div>
                      <div className="mt-0.5 text-xs text-muted-foreground">
                        引用{' '}
                        {citation.citationNo != null ? `[${citation.citationNo}]` : citation.id}
                      </div>
                    </div>
                    {sourceAvailable && documentId && knowledgeBaseId && (
                      <Button
                        aria-label="下载原文"
                        className="h-7 shrink-0 px-2"
                        disabled={downloadingId === citation.id}
                        onClick={() => void handleDownload(citation)}
                        size="sm"
                        variant="outline"
                      >
                        {downloadingId === citation.id ? (
                          <Loader2 className="size-3 animate-spin" />
                        ) : (
                          <Download className="size-3" />
                        )}
                        下载原文
                      </Button>
                    )}
                  </div>

                  {content && (
                    <div className="rounded-md bg-muted/60 p-2 text-sm text-foreground">
                      {truncateText(content, 300)}
                    </div>
                  )}
                  {!content && preview && (
                    <div className="rounded-md bg-muted/60 p-2 text-sm text-foreground">
                      {truncateText(preview, 300)}
                    </div>
                  )}
                  {citation.context && (
                    <details className="rounded-md border border-border/60 px-2 py-1 text-xs text-muted-foreground">
                      <summary className="cursor-pointer text-foreground">上下文</summary>
                      <div className="mt-1">{truncateText(citation.context, 500)}</div>
                    </details>
                  )}

                  <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    {score && <span>相关度 {score}</span>}
                    {rerankScore && <span>重排 {rerankScore}</span>}
                    {citation.pageNumber != null && <span>页码 {citation.pageNumber}</span>}
                    {citation.chunkType && <span>类型 {citation.chunkType}</span>}
                    {citation.sectionPath && (
                      <span className="col-span-2">段落路径 {citation.sectionPath}</span>
                    )}
                  </div>

                  {sourceUnavailable && (
                    <div className="text-xs text-muted-foreground">
                      原文不可下载：{source?.reason ?? '来源不可用'}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
          {downloadError && <div className="mt-3 text-xs text-destructive">{downloadError}</div>}
        </div>
      </PopoverContent>
    </Popover>
  )
}

/* ── Thinking panel ── */
type ThinkPanelStep = QAThinkingStep & {
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

type IterationGroup = {
  durationMs?: number
  iterationNo: number
  reasoningSteps: ThinkPanelStep[]
  status: QAThinkingStep['status']
  steps: ThinkPanelStep[]
  title?: string
  toolSteps: ThinkPanelStep[]
}

const SUMMARY_LIMIT = 500
const SUMMARY_KEY_LABELS: Record<string, string> = {
  citationCount: '引用数',
  citations: '引用数',
  chunkCount: '片段数',
  hitCount: '命中数',
  hits: '命中数',
  knowledgeBaseCount: '知识库数',
  query: '查询词',
  queryCount: '查询数',
  queryText: '查询词',
  rerankTopN: '重排序 TopN',
  resultCount: '结果数',
  topK: 'TopK',
}
const SENSITIVE_SUMMARY_PATTERN =
  /https?:\/\/|s3:\/\/|gs:\/\/|minio:\/\/|localhost|127\.|10\.|172\.(1[6-9]|2\d|3[01])\.|192\.168\.|api[_-]?key|authorization|bearer\s|credential|object[_-]?key|password|prompt|secret|token/i

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function safeSummaryValue(value: unknown): string | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  if (!trimmed || SENSITIVE_SUMMARY_PATTERN.test(trimmed)) return undefined
  return trimmed
}

function collectSummaryRows(value: unknown): Array<{ label: string; value: string }> {
  if (!isRecord(value)) return []
  const rows: Array<{ label: string; value: string }> = []
  for (const [key, entry] of Object.entries(value)) {
    const label = SUMMARY_KEY_LABELS[key]
    if (label) {
      const displayValue =
        Array.isArray(entry) && (key === 'citations' || key === 'hits')
          ? String(entry.length)
          : safeSummaryValue(entry)
      if (displayValue) rows.push({ label, value: displayValue })
      continue
    }
    if (isRecord(entry)) rows.push(...collectSummaryRows(entry))
  }
  return rows.slice(0, 8)
}

function SummarySection({ title, value }: { title: string; value: unknown }) {
  const [expanded, setExpanded] = useState(false)
  const rows = collectSummaryRows(value)

  if (rows.length === 0) return null

  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      <dl className="space-y-1">
        {rows.map((row, index) => {
          const shouldTruncate = row.value.length > SUMMARY_LIMIT
          const display =
            shouldTruncate && !expanded ? row.value.slice(0, SUMMARY_LIMIT) : row.value
          return (
            <div key={`${row.label}-${index}`} className="grid grid-cols-[5rem_1fr] gap-2 text-xs">
              <dt className="text-muted-foreground">{row.label}</dt>
              <dd className="break-words text-foreground/90">
                {display}
                {shouldTruncate && !expanded && '...'}
              </dd>
            </div>
          )
        })}
      </dl>
      {rows.some((row) => row.value.length > SUMMARY_LIMIT) && (
        <button
          className="text-xs font-medium text-primary hover:underline"
          type="button"
          onClick={() => setExpanded((current) => !current)}
        >
          {expanded ? '收起' : '展开更多'}
        </button>
      )}
    </div>
  )
}

function getStepIteration(step: ThinkPanelStep, fallback: number): number {
  return typeof step.iterationNo === 'number' && Number.isFinite(step.iterationNo)
    ? step.iterationNo
    : fallback
}

function groupThinkingSteps(steps: QAThinkingStep[]): IterationGroup[] {
  const groups = new Map<number, IterationGroup>()
  let currentIteration = 1

  for (const rawStep of steps as ThinkPanelStep[]) {
    if (rawStep.type === 'agent_iteration') {
      currentIteration = getStepIteration(rawStep, currentIteration)
    }
    const iterationNo = getStepIteration(rawStep, currentIteration)
    currentIteration = iterationNo
    const group =
      groups.get(iterationNo) ??
      ({
        iterationNo,
        reasoningSteps: [],
        status: 'done',
        steps: [],
        toolSteps: [],
      } satisfies IterationGroup)

    group.steps.push(rawStep)
    if (rawStep.type === 'agent_iteration') {
      group.title = rawStep.label
      group.status = rawStep.status
    }
    if (rawStep.type === 'tool_call') group.toolSteps.push(rawStep)
    if (rawStep.type !== 'agent_iteration' && rawStep.type !== 'tool_call') {
      group.reasoningSteps.push(rawStep)
    }
    groups.set(iterationNo, group)
  }

  return [...groups.values()].map((group) => {
    const starts = group.toolSteps
      .map((step) => step.startedAt)
      .filter((value): value is number => typeof value === 'number')
    const ends = group.toolSteps
      .map((step) => step.completedAt)
      .filter((value): value is number => typeof value === 'number')
    const durationMs =
      starts.length > 0 && ends.length > 0 ? Math.max(...ends) - Math.min(...starts) : undefined
    return { ...group, durationMs }
  })
}

function statusText(group: IterationGroup): string {
  if (group.steps.some((step) => step.status === 'failed')) return '有失败'
  if (group.steps.some((step) => step.status === 'running') || group.status === 'running') {
    return '执行中'
  }
  return '已完成'
}

function formatDuration(ms: number | undefined): string | undefined {
  if (ms == null || ms < 0) return undefined
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatElapsedClock(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

function StreamingElapsed({ startedAt }: { startedAt?: string }) {
  const startTime = startedAt ? Date.parse(startedAt) : Number.NaN
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])

  const elapsed = Number.isFinite(startTime) ? now - startTime : 0

  return (
    <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
      正在生成 · 已等待 {formatElapsedClock(elapsed)}
    </span>
  )
}

function ToolCallStep({
  onArtifactDownload,
  step,
}: {
  onArtifactDownload?: (reportFileId: string, filename: string) => void
  step: ThinkPanelStep
}) {
  const [open, setOpen] = useState(false)
  const hasDetails =
    Boolean(step.argumentsSummary) ||
    Boolean(step.resultSummary) ||
    Boolean(step.errorSummary) ||
    Boolean(step.reportArtifact)

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        className={cn(
          'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-background/80',
          step.status === 'failed' && 'bg-red-500/10 text-red-600 hover:bg-red-500/15',
        )}
      >
        {open ? (
          <ChevronDown className="size-3 shrink-0" />
        ) : (
          <ChevronRight className="size-3 shrink-0" />
        )}
        <span
          className={cn(
            'size-1.5 shrink-0 rounded-full',
            step.status === 'done' && 'bg-green-500',
            step.status === 'running' && 'bg-primary animate-pulse',
            step.status === 'pending' && 'bg-muted-foreground/40 animate-pulse',
            step.status === 'failed' && 'bg-red-500',
          )}
        />
        <span className="min-w-0 flex-1 truncate">{step.label ?? step.toolName ?? '工具调用'}</span>
        {step.status === 'done' && <Check className="size-3 shrink-0 text-green-500" />}
        {step.status === 'running' && <span className="animate-pulse text-xs text-primary">▊</span>}
        {step.status === 'failed' && <span className="shrink-0 text-xs text-red-600">失败</span>}
      </CollapsibleTrigger>
      {hasDetails && (
        <CollapsibleContent className="ml-5 space-y-3 border-l border-border/60 py-2 pl-3">
          <SummarySection title="参数" value={step.argumentsSummary} />
          <SummarySection title="结果" value={step.resultSummary} />
          {step.errorSummary && (
            <p className="text-xs leading-relaxed text-red-600">失败原因：{step.errorSummary}</p>
          )}
          {step.reportArtifact && (
            <ReportArtifactCard artifact={step.reportArtifact} onDownload={onArtifactDownload} />
          )}
        </CollapsibleContent>
      )}
    </Collapsible>
  )
}

function ReasoningStep({ step }: { step: ThinkPanelStep }) {
  return (
    <div className="flex items-start gap-2 rounded-md px-2 py-1.5 text-sm text-muted-foreground">
      <span
        className={cn(
          'mt-1.5 size-1.5 shrink-0 rounded-full',
          step.status === 'done' && 'bg-green-500',
          step.status === 'running' && 'bg-primary animate-pulse',
          step.status === 'pending' && 'bg-muted-foreground/40 animate-pulse',
          step.status === 'failed' && 'bg-red-500',
        )}
      />
      <span className="min-w-0 flex-1">
        <span className="text-foreground/90">{step.label ?? '思考步骤'}</span>
        {step.detail && <span className="ml-2 text-xs leading-relaxed">{step.detail}</span>}
      </span>
      {step.status === 'running' && <span className="animate-pulse text-xs text-primary">▊</span>}
      {step.status === 'failed' && <span className="shrink-0 text-xs text-red-600">失败</span>}
    </div>
  )
}

function ThinkPanel({
  done,
  onArtifactDownload,
  reasoningContent,
  steps,
}: {
  done: boolean
  onArtifactDownload?: (reportFileId: string, filename: string) => void
  reasoningContent?: string
  steps: QAThinkingStep[]
}) {
  const [open, setOpen] = useState(!done)
  const groups = groupThinkingSteps(steps)
  const hasReasoningContent = Boolean(reasoningContent?.trim())
  const reasoningMarkdownComponents = useMemo(() => createReasoningMarkdownComponents(), [])

  useEffect(() => {
    if (done) {
      const t = setTimeout(() => setOpen(false), 3000)
      return () => clearTimeout(t)
    }
    setOpen(true)
  }, [done])

  if (!hasReasoningContent && steps.length === 0) return null

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1 py-1 text-sm text-muted-foreground transition-colors hover:text-foreground">
        {open ? (
          <ChevronDown className="size-3 shrink-0" />
        ) : (
          <ChevronRight className="size-3 shrink-0" />
        )}
        <span>
          思考过程
          {hasReasoningContent && steps.length > 0 ? ` · ${steps.length} 步` : ''}
          {!hasReasoningContent && steps.length > 0 ? ` (${steps.length} 步)` : ''}
        </span>
        {done && <Check className="size-3 shrink-0 text-green-500" />}
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-1 space-y-3 rounded-md border border-border/50 bg-muted/50 p-3">
        {hasReasoningContent && (
          <section className="space-y-2">
            <div className="flex items-center gap-2 text-sm font-medium text-foreground">
              <Brain className="size-3.5 shrink-0 text-primary" />
              <span>💭 深度思考</span>
            </div>
            <div className="rounded-md bg-background/70 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
              <ReactMarkdown components={reasoningMarkdownComponents} remarkPlugins={[remarkGfm]}>
                {reasoningContent}
              </ReactMarkdown>
            </div>
          </section>
        )}
        {steps.length > 0 && (
          <section className="space-y-2">
            <div className="flex items-center gap-2 text-sm font-medium text-foreground">
              <Wrench className="size-3.5 shrink-0 text-primary" />
              <span>🔧 工具调用</span>
            </div>
            <div className="space-y-3">
              {groups.map((group) => (
                <section key={group.iterationNo} className="space-y-2">
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-medium text-foreground">第 {group.iterationNo} 轮</span>
                    <span className="text-xs text-muted-foreground">
                      {group.toolSteps.length} 个工具调用 · {statusText(group)}
                    </span>
                    {formatDuration(group.durationMs) && (
                      <span className="text-xs text-muted-foreground">
                        耗时 {formatDuration(group.durationMs)}
                      </span>
                    )}
                  </div>
                  {group.reasoningSteps.length > 0 && (
                    <div className="space-y-1">
                      {group.reasoningSteps.map((step, index) => (
                        <ReasoningStep
                          key={`${group.iterationNo}-${step.type}-${index}`}
                          step={step}
                        />
                      ))}
                    </div>
                  )}
                  {group.toolSteps.length > 0 && (
                    <div className="space-y-1">
                      {group.toolSteps.map((step, index) => (
                        <ToolCallStep
                          key={step.toolCallId ?? `${group.iterationNo}-${index}`}
                          onArtifactDownload={onArtifactDownload}
                          step={step}
                        />
                      ))}
                    </div>
                  )}
                  {group.reasoningSteps.length === 0 && group.toolSteps.length === 0 && (
                    <div className="flex items-center gap-2 px-2 py-1.5 text-sm text-muted-foreground">
                      <span
                        className={cn(
                          'size-1.5 shrink-0 rounded-full',
                          group.status === 'running' ? 'bg-primary animate-pulse' : 'bg-green-500',
                        )}
                      />
                      <span>{group.title ?? '直接生成回答'}</span>
                      {group.status === 'running' && (
                        <span className="animate-pulse text-xs text-primary">▊</span>
                      )}
                    </div>
                  )}
                </section>
              ))}
            </div>
          </section>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

/* ── Markdown content ── */
function createMarkdownComponents(citations: QACitation[]) {
  const citationsByNo = createCitationMap(citations)

  return {
    h1: ({ children, ...rest }: MarkdownComponentProps) => (
      <h1 className="mb-4 mt-6 text-xl font-bold text-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </h1>
    ),
    h2: ({ children, ...rest }: MarkdownComponentProps) => (
      <h2 className="mb-3 mt-5 text-lg font-semibold text-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </h2>
    ),
    h3: ({ children, ...rest }: MarkdownComponentProps) => (
      <h3 className="mb-2 mt-4 text-base font-semibold text-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </h3>
    ),
    p: ({ children, ...rest }: MarkdownComponentProps) => (
      <p className="my-2" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </p>
    ),
    ul: ({ children, ...rest }: MarkdownComponentProps) => (
      <ul className="my-2 list-disc pl-6" {...rest}>
        {children}
      </ul>
    ),
    ol: ({ children, ...rest }: MarkdownComponentProps) => (
      <ol className="my-2 list-decimal pl-6" {...rest}>
        {children}
      </ol>
    ),
    li: ({ children, ...rest }: MarkdownComponentProps) => (
      <li className="my-1" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </li>
    ),
    strong: ({ children, ...rest }: MarkdownComponentProps) => (
      <strong className="font-semibold text-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </strong>
    ),
    em: ({ children, ...rest }: MarkdownComponentProps) => (
      <em {...rest}>{renderCitationChildren(children, citationsByNo)}</em>
    ),
    code: ({
      className: cls,
      children,
      ...rest
    }: { className?: string; children?: ReactNode } & Record<string, unknown>) => {
      const isInline = !cls?.includes('language-')
      if (isInline) {
        return (
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono" {...rest}>
            {children}
          </code>
        )
      }
      return (
        <code className={cls} {...rest}>
          {children}
        </code>
      )
    },
    pre: ({ children, ...rest }: MarkdownComponentProps) => (
      <pre
        className="my-2 overflow-x-auto rounded-md bg-zinc-950 p-4 text-sm text-zinc-50"
        {...rest}
      >
        {children}
      </pre>
    ),
    table: ({ children, ...rest }: MarkdownComponentProps) => (
      <div className="my-3 overflow-x-auto">
        <table className="w-full border-collapse border border-border text-sm" {...rest}>
          {children}
        </table>
      </div>
    ),
    thead: ({ children, ...rest }: MarkdownComponentProps) => (
      <thead className="bg-muted/50" {...rest}>
        {children}
      </thead>
    ),
    tbody: ({ children, ...rest }: MarkdownComponentProps) => <tbody {...rest}>{children}</tbody>,
    tr: ({ children, ...rest }: MarkdownComponentProps) => (
      <tr className="border-b border-border last:border-b-0" {...rest}>
        {children}
      </tr>
    ),
    th: ({ children, ...rest }: MarkdownComponentProps) => (
      <th className="px-3 py-2 text-left font-semibold text-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </th>
    ),
    td: ({ children, ...rest }: MarkdownComponentProps) => (
      <td className="px-3 py-2 text-muted-foreground" {...rest}>
        {renderCitationChildren(children, citationsByNo)}
      </td>
    ),
    a: ({ children, href, ...rest }: MarkdownComponentProps & { href?: string }) => (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-primary underline hover:text-primary/80"
        {...rest}
      >
        {children}
      </a>
    ),
    blockquote: ({ children, ...rest }: MarkdownComponentProps) => (
      <blockquote
        className="my-2 border-l-4 border-primary/30 pl-4 italic text-muted-foreground"
        {...rest}
      >
        {children}
      </blockquote>
    ),
    hr: (_props: MarkdownComponentProps) => <hr className="my-4 border-border" />,
  }
}

function createReasoningMarkdownComponents(): Components {
  const components: Components = {
    h1: ({ children, ...rest }) => (
      <h1 className="my-2 text-sm font-semibold text-foreground/90" {...rest}>
        {children}
      </h1>
    ),
    h2: ({ children, ...rest }) => (
      <h2 className="my-2 text-sm font-semibold text-foreground/90" {...rest}>
        {children}
      </h2>
    ),
    h3: ({ children, ...rest }) => (
      <h3 className="my-1.5 text-xs font-semibold text-foreground/90" {...rest}>
        {children}
      </h3>
    ),
    p: ({ children, ...rest }) => (
      <p className="my-1" {...rest}>
        {children}
      </p>
    ),
    ul: ({ children, ...rest }) => (
      <ul className="my-1 list-disc pl-4" {...rest}>
        {children}
      </ul>
    ),
    ol: ({ children, ...rest }) => (
      <ol className="my-1 list-decimal pl-4" {...rest}>
        {children}
      </ol>
    ),
    li: ({ children, ...rest }) => (
      <li className="my-0.5" {...rest}>
        {children}
      </li>
    ),
    strong: ({ children, ...rest }) => (
      <strong className="font-medium text-foreground/90" {...rest}>
        {children}
      </strong>
    ),
    code: ({ children, ...rest }) => (
      <code className="rounded bg-muted px-1 py-0.5 text-[0.7rem]" {...rest}>
        {children}
      </code>
    ),
    pre: ({ children, ...rest }) => (
      <pre className="my-1 overflow-x-auto rounded-md bg-background p-2 text-xs" {...rest}>
        {children}
      </pre>
    ),
  }
  return components
}

/* ── Status label for assistant messages ── */
function StatusLabel({ status }: { status: QAMessage['status'] }) {
  if (!status || status === 'completed') return null
  if (status === 'streaming') return null
  if (status === 'stopped' || status === 'cancelled') {
    return (
      <span className="ml-2 text-xs text-muted-foreground" aria-label="回复已停止">
        已停止
      </span>
    )
  }
  if (status === 'failed') {
    return (
      <span className="ml-2 text-xs text-destructive" aria-label="发送失败">
        发送失败
      </span>
    )
  }
  return null
}

function StreamingContent({
  content,
  streaming,
  components,
}: {
  content: string
  streaming: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  components: Record<string, any>
}) {
  const markdownElement = useMemo(
    () => (
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      <ReactMarkdown components={components as any} remarkPlugins={[remarkGfm]}>
        {content}
      </ReactMarkdown>
    ),
    [content, components],
  )

  return (
    <div className={streaming && content.trim() ? 'streaming-cursor' : undefined}>
      {markdownElement}
      {streaming && !content.trim() && (
        <span
          className="ml-0.5 inline-block h-[1em] w-[0.1em] animate-pulse bg-primary align-middle select-none"
          aria-label="助手正在回复中"
        />
      )}
    </div>
  )
}

/* ── Single message bubble ── */
function MessageBubble({
  msg,
  isStreaming,
  onArtifactDownload,
}: {
  msg: QAMessage
  isStreaming: boolean
  onArtifactDownload?: (reportFileId: string, filename: string) => void
}) {
  const isUser = msg.role === 'user'
  const hasThinking = msg.thinking && msg.thinking.length > 0
  const reasoningContent = (msg as QAMessageWithReasoning).reasoningContent
  const hasReasoningContent = Boolean(reasoningContent?.trim())
  const hasCitations = msg.citations && msg.citations.length > 0
  const markdownComponents = useMemo(
    () => createMarkdownComponents(msg.citations ?? []),
    [msg.citations],
  )

  // Report artifacts stored as a dynamic property (not in the QAMessage schema yet)
  const artifacts = (msg as Record<string, unknown>).artifacts as QAReportArtifact[] | undefined
  const hasArtifacts = artifacts && artifacts.length > 0

  // Determine effective streaming state
  const effectiveStreaming = msg.status === 'streaming' || (!msg.status && isStreaming)

  // Determine thinking done state
  const thinkingDone =
    msg.status === 'completed' ||
    msg.status === 'stopped' ||
    msg.status === 'cancelled' ||
    msg.status === 'failed' ||
    (!msg.status && !isStreaming)

  return (
    <div className={cn('flex gap-2', isUser ? 'flex-row-reverse' : '')}>
      {/* Avatar */}
      {isUser ? (
        <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/20 text-xs font-bold text-primary">
          我
        </div>
      ) : (
        <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary text-xs font-bold text-primary-foreground">
          电
        </div>
      )}

      {/* Bubble */}
      <div
        className={cn(
          'min-w-0 px-4 py-3',
          isUser
            ? 'rounded-lg rounded-br-sm bg-primary text-primary-foreground'
            : 'rounded-lg rounded-bl-sm border border-border bg-muted',
        )}
      >
        {/* Thinking steps (assistant only) */}
        {(hasThinking || hasReasoningContent) && (
          <div className="mb-2">
            <ThinkPanel
              steps={msg.thinking ?? []}
              reasoningContent={reasoningContent}
              done={thinkingDone}
              onArtifactDownload={onArtifactDownload}
            />
          </div>
        )}

        {/* Message content */}
        <div className="leading-relaxed">
          {isUser ? (
            <p className="whitespace-pre-wrap">{msg.content}</p>
          ) : msg.content ? (
            <span>
              <StreamingContent
                content={msg.content}
                streaming={effectiveStreaming}
                components={markdownComponents}
              />
              <StatusLabel status={msg.status} />
              {effectiveStreaming && (
                <span className="mt-2 block">
                  <StreamingElapsed startedAt={msg.createdAt} />
                </span>
              )}
            </span>
          ) : effectiveStreaming ? (
            <span className="inline-flex items-center gap-2">
              <span
                className="ml-0.5 inline-block h-[1em] w-[0.1em] animate-pulse bg-primary align-middle select-none"
                aria-label="助手正在回复中"
              />
              <StreamingElapsed startedAt={msg.createdAt} />
              <StatusLabel status={msg.status} />
            </span>
          ) : msg.status === 'stopped' || msg.status === 'cancelled' || msg.status === 'failed' ? (
            <span>
              <span className="italic text-muted-foreground">（无内容）</span>
              <StatusLabel status={msg.status} />
            </span>
          ) : (
            <span className="italic text-muted-foreground">（无内容）</span>
          )}
        </div>

        {/* Citations (assistant only) */}
        {hasCitations && (
          <div className="mt-4 border-t border-border/50 pt-2">
            <p className="mb-1 text-xs font-semibold text-muted-foreground">引用来源</p>
            <div className="flex flex-wrap gap-1">
              {msg.citations!.map((c) => (
                <CitationTooltip key={c.id} c={c} />
              ))}
            </div>
          </div>
        )}

        {/* Report artifacts (assistant only) */}
        {hasArtifacts &&
          artifacts!.map((artifact) => (
            <ReportArtifactCard
              key={artifact.reportId ?? artifact.jobId ?? artifact.reportName ?? 'artifact'}
              artifact={artifact}
              onDownload={onArtifactDownload}
            />
          ))}
      </div>
    </div>
  )
}

// ══════════════════════════════════════════════════════════════════════════════
// Main component
// ══════════════════════════════════════════════════════════════════════════════

type ChatMessagesProps = {
  messages: QAMessage[]
  streaming: boolean
  error: string | null
  onRetry?: () => void
  onArtifactDownload?: (reportFileId: string, filename: string) => void
}

export default function ChatMessages({
  messages,
  streaming,
  error,
  onRetry,
  onArtifactDownload,
}: ChatMessagesProps) {
  const scrollRef = useRef<HTMLDivElement>(null)

  // Coalesce continuous streaming updates into at most one scroll per frame.
  useEffect(() => {
    const frameId = requestAnimationFrame(() => {
      const element = scrollRef.current
      if (element) element.scrollTop = element.scrollHeight
    })

    return () => cancelAnimationFrame(frameId)
  }, [messages, streaming])

  return (
    <div ref={scrollRef} className="flex flex-1 flex-col gap-6 overflow-y-auto px-3 py-6">
      {/* ── Message list ── */}
      {messages.map((msg, i) => {
        const isLast = i === messages.length - 1
        const isStreamingAsst = isLast && msg.role === 'assistant' && streaming
        return (
          <div
            key={msg.id}
            className={cn('msg-enter max-w-[85%]', msg.role === 'user' ? 'self-end' : 'self-start')}
            style={{ animationDelay: `${Math.min(i * 50, 600)}ms` }}
          >
            <MessageBubble
              msg={msg}
              isStreaming={isStreamingAsst}
              onArtifactDownload={onArtifactDownload}
            />
          </div>
        )
      })}

      {/* ── Error ── */}
      {error && (
        <InlineNotice
          action={
            onRetry ? (
              <Button variant="destructive" size="sm" onClick={onRetry}>
                重试
              </Button>
            ) : undefined
          }
          className="mx-4"
          variant="error"
        >
          {error}
        </InlineNotice>
      )}
    </div>
  )
}
