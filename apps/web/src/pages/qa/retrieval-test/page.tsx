import { AlertCircle, CheckCircle2, Loader2, RefreshCw, Search } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { z } from 'zod'

import { ApiError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  useCreateQARetrievalTestRunMutation,
  useQARetrievalTestRunQuery,
} from '@/features/qa-admin/qa-admin.queries'
import type {
  CreateQARetrievalTestRunRequest,
  QARetrievalTestResult,
  QARetrievalTestRun,
} from '@/features/qa-admin/qa-admin.types'

type RetrievalFormState = {
  question: string
  knowledgeBaseIds: string
  topK: string
  scoreThreshold: string
  enableRerank: boolean
  rerankThreshold: string
  rerankTopN: string
}

const initialForm: RetrievalFormState = {
  question: '',
  knowledgeBaseIds: '',
  topK: '5',
  scoreThreshold: '',
  enableRerank: true,
  rerankThreshold: '',
  rerankTopN: '',
}

const retrievalTestPayloadSchema = z.object({
  question: z.string().min(1, 'query 不能为空'),
  knowledgeBaseIds: z.array(z.string()).optional(),
  retrieval: z
    .object({
      topK: z.number().int().min(1).max(100).optional(),
      scoreThreshold: z.number().min(0).max(1).optional(),
      enableRerank: z.boolean().optional(),
      rerankThreshold: z.number().min(0).max(1).optional(),
      rerankTopN: z.number().int().min(1).optional(),
    })
    .optional(),
}) satisfies z.ZodType<CreateQARetrievalTestRunRequest>

function optionalNumber(
  value: string,
  label: string,
  min?: number,
  max?: number,
): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined

  const parsed = Number(normalized)
  if (!Number.isFinite(parsed)) {
    throw new Error(`${label} 必须是数字`)
  }
  if (min !== undefined && parsed < min) {
    throw new Error(`${label} 不能小于 ${min}`)
  }
  if (max !== undefined && parsed > max) {
    throw new Error(`${label} 不能大于 ${max}`)
  }

  return parsed
}

function optionalInteger(
  value: string,
  label: string,
  min?: number,
  max?: number,
): number | undefined {
  const parsed = optionalNumber(value, label, min, max)
  if (parsed !== undefined && !Number.isInteger(parsed)) {
    throw new Error(`${label} 必须是整数`)
  }

  return parsed
}

function splitIds(value: string): string[] | undefined {
  const ids = value
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean)

  return ids.length > 0 ? ids : undefined
}

function buildPayload(form: RetrievalFormState): CreateQARetrievalTestRunRequest {
  const parsed = retrievalTestPayloadSchema.safeParse({
    question: form.question.trim(),
    knowledgeBaseIds: splitIds(form.knowledgeBaseIds),
    retrieval: {
      topK: optionalInteger(form.topK, 'Top K', 1, 100),
      scoreThreshold: optionalNumber(form.scoreThreshold, '阈值', 0, 1),
      enableRerank: form.enableRerank,
      rerankThreshold: optionalNumber(form.rerankThreshold, 'Rerank 阈值', 0, 1),
      rerankTopN: optionalInteger(form.rerankTopN, 'Rerank Top N', 1, 100),
    },
  })

  if (!parsed.success) {
    throw new Error(parsed.error.issues.map((issue) => issue.message).join('；'))
  }

  return parsed.data
}

function getErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId ? `${error.message}（requestId: ${error.requestId}）` : error.message
  }

  return error instanceof Error ? error.message : '未知错误'
}

function formatNumber(value: number | null | undefined, digits = 4): string {
  if (value === null || value === undefined) return '-'
  return Number.isInteger(value) ? String(value) : value.toFixed(digits)
}

function formatDateTime(value: string | null | undefined): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function getLatencyMs(run: QARetrievalTestRun): number | undefined {
  if (!run.finishedAt) return undefined
  const started = new Date(run.createdAt).getTime()
  const finished = new Date(run.finishedAt).getTime()
  if (!Number.isFinite(started) || !Number.isFinite(finished) || finished < started) {
    return undefined
  }

  return finished - started
}

function getResultDocumentId(result: QARetrievalTestResult): string {
  return result.documentId ?? result.docId ?? '-'
}

function getResultDocumentName(result: QARetrievalTestResult): string {
  return result.documentName ?? result.docName ?? '未命名文档'
}

function getResultText(result: QARetrievalTestResult): string {
  return result.contentPreview ?? result.text ?? ''
}

function StateMessage({ type, message }: { type: 'success' | 'error' | 'empty'; message: string }) {
  const Icon = type === 'success' ? CheckCircle2 : AlertCircle
  const className =
    type === 'success'
      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700'
      : type === 'error'
        ? 'border-destructive/30 bg-destructive/10 text-destructive'
        : 'border-border bg-muted/30 text-muted-foreground'

  return (
    <div className={`flex items-start gap-2 rounded-lg border p-3 text-sm ${className}`}>
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <span>{message}</span>
    </div>
  )
}

function ResultsList({ results }: { results: QARetrievalTestResult[] }) {
  const orderedResults = [...results].sort((left, right) => left.rankNo - right.rankNo)

  return (
    <div className="space-y-3">
      {orderedResults.map((result) => (
        <article
          key={`${result.rankNo}-${getResultDocumentId(result)}-${result.chunkId ?? 'chunk'}`}
          className="rounded-lg border border-border bg-background p-4"
        >
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Badge variant="secondary">#{result.rankNo}</Badge>
            <span className="min-w-0 break-words text-sm font-medium text-foreground">
              {getResultDocumentName(result)}
            </span>
            <Badge variant="outline">doc {getResultDocumentId(result)}</Badge>
            {result.chunkId && <Badge variant="outline">chunk {result.chunkId}</Badge>}
          </div>
          {result.sectionPath && (
            <p className="mb-2 text-xs text-muted-foreground">{result.sectionPath}</p>
          )}
          <p className="whitespace-pre-wrap text-sm leading-6 text-foreground">
            {getResultText(result) || '该命中未返回可展示片段'}
          </p>
          <dl className="mt-3 grid gap-2 text-xs text-muted-foreground sm:grid-cols-3">
            <div className="rounded-md border border-border px-2 py-1">
              <dt>score</dt>
              <dd className="font-mono text-foreground">{formatNumber(result.score)}</dd>
            </div>
            <div className="rounded-md border border-border px-2 py-1">
              <dt>vectorScore</dt>
              <dd className="font-mono text-foreground">{formatNumber(result.vectorScore)}</dd>
            </div>
            <div className="rounded-md border border-border px-2 py-1">
              <dt>rerankScore</dt>
              <dd className="font-mono text-foreground">{formatNumber(result.rerankScore)}</dd>
            </div>
          </dl>
        </article>
      ))}
    </div>
  )
}

export function QARetrievalTestPage() {
  const [form, setForm] = useState<RetrievalFormState>(initialForm)
  const [latestRun, setLatestRun] = useState<QARetrievalTestRun | null>(null)
  const [latestRunId, setLatestRunId] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const requestSeq = useRef(0)
  const createMutation = useCreateQARetrievalTestRunMutation()
  const runQuery = useQARetrievalTestRunQuery(latestRunId, Boolean(latestRunId))

  const visibleRun = runQuery.data ?? latestRun
  const runError = runQuery.isError ? getErrorMessage(runQuery.error) : null
  const latencyMs = visibleRun ? getLatencyMs(visibleRun) : undefined
  const results = useMemo(() => visibleRun?.results ?? [], [visibleRun])

  const submit = async () => {
    const seq = requestSeq.current + 1
    requestSeq.current = seq
    setFormError(null)
    setLatestRun(null)
    setLatestRunId(null)
    createMutation.reset()

    try {
      const payload = buildPayload(form)
      const created = await createMutation.mutateAsync(payload)
      if (requestSeq.current !== seq) return
      setLatestRun(created)
      setLatestRunId(created.id)
    } catch (error) {
      if (requestSeq.current !== seq) return
      setFormError(getErrorMessage(error))
    }
  }

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-2xl font-semibold text-foreground">QA 检索测试</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            输入 query 和覆盖参数，创建一次 QA 检索体验测试并查看有序命中。
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => void runQuery.refetch()}
          disabled={!latestRunId || runQuery.isFetching}
        >
          <RefreshCw aria-hidden="true" className="size-4" />
          刷新结果
        </Button>
      </div>

      <section className="space-y-4 rounded-lg border border-border bg-card p-5">
        <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">Query</span>
            <Textarea
              maxLength={5000}
              value={form.question}
              onChange={(event) => setForm({ ...form, question: event.target.value })}
              className="min-h-28"
              placeholder="输入要验证召回效果的问题"
            />
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">知识库 ID</span>
            <Textarea
              maxLength={2000}
              value={form.knowledgeBaseIds}
              onChange={(event) => setForm({ ...form, knowledgeBaseIds: event.target.value })}
              className="min-h-28 font-mono text-xs"
              placeholder="每行或逗号分隔；为空则使用当前 QA 配置"
            />
          </label>
        </div>

        <div className="grid gap-3 md:grid-cols-5">
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">Top K</span>
            <Input
              type="number"
              min={1}
              max={100}
              value={form.topK}
              onChange={(event) => setForm({ ...form, topK: event.target.value })}
            />
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">Score 阈值</span>
            <Input
              type="number"
              min={0}
              max={1}
              step={0.01}
              value={form.scoreThreshold}
              onChange={(event) => setForm({ ...form, scoreThreshold: event.target.value })}
            />
          </label>
          <label className="flex items-center gap-2 rounded-lg border border-border p-3 text-sm">
            <input
              type="checkbox"
              className="size-4"
              checked={form.enableRerank}
              onChange={(event) => setForm({ ...form, enableRerank: event.target.checked })}
            />
            <span className="font-medium text-foreground">启用 rerank</span>
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">Rerank 阈值</span>
            <Input
              type="number"
              min={0}
              max={1}
              step={0.01}
              value={form.rerankThreshold}
              onChange={(event) => setForm({ ...form, rerankThreshold: event.target.value })}
            />
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="font-medium text-foreground">Rerank Top N</span>
            <Input
              type="number"
              min={1}
              max={100}
              value={form.rerankTopN}
              onChange={(event) => setForm({ ...form, rerankTopN: event.target.value })}
            />
          </label>
        </div>

        {formError && <StateMessage type="error" message={formError} />}
        {createMutation.isError && !formError && (
          <StateMessage type="error" message={getErrorMessage(createMutation.error)} />
        )}

        <div className="flex justify-end">
          <Button
            type="button"
            onClick={() => void submit()}
            disabled={createMutation.isPending || form.question.trim().length === 0}
          >
            {createMutation.isPending ? (
              <Loader2 aria-hidden="true" className="size-4 animate-spin" />
            ) : (
              <Search aria-hidden="true" className="size-4" />
            )}
            发起测试
          </Button>
        </div>
      </section>

      <section className="space-y-4 rounded-lg border border-border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 className="text-lg font-semibold text-foreground">测试结果</h4>
            <p className="mt-1 text-sm text-muted-foreground">只展示最近一次发起的测试结果。</p>
          </div>
          {visibleRun && (
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              <Badge variant={visibleRun.status === 'failed' ? 'destructive' : 'outline'}>
                {visibleRun.status}
              </Badge>
              <Badge variant="outline">ID {visibleRun.id}</Badge>
              <Badge variant="outline">创建 {formatDateTime(visibleRun.createdAt)}</Badge>
              <Badge variant="outline">
                延迟 {latencyMs === undefined ? '-' : `${latencyMs}ms`}
              </Badge>
            </div>
          )}
        </div>

        {runError && <StateMessage type="error" message={`刷新测试结果失败：${runError}`} />}
        {runQuery.isFetching && visibleRun?.status === 'running' && (
          <StateMessage type="empty" message="测试运行中，正在刷新状态。" />
        )}
        {!visibleRun && !createMutation.isPending && !formError && (
          <StateMessage type="empty" message="尚未发起检索测试。" />
        )}
        {visibleRun?.status === 'failed' && (
          <StateMessage type="error" message="检索测试失败，输入已保留，可调整参数后重试。" />
        )}
        {visibleRun?.status === 'completed' && results.length === 0 && (
          <StateMessage type="empty" message="测试完成但没有命中结果。" />
        )}
        {visibleRun?.status === 'completed' && results.length > 0 && (
          <ResultsList results={results} />
        )}
      </section>
    </div>
  )
}
