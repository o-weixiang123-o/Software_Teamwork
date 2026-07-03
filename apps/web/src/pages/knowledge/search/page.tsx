import { BookOpen, ChevronDown, ChevronUp, Loader2, Search } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'

import { listKnowledgeBases } from '@/api/knowledge'
import { InlineNotice, StateBlock } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getGatewayCapabilityIssue, useKnowledgeSearch } from '@/features/knowledge'
import type { KnowledgeBaseSummary, KnowledgeQueryResult } from '@/lib/types'
import { cn } from '@/lib/utils'

// ── Constants ──

const TOP_K_MIN = 1
const TOP_K_MAX = 20
const SCORE_THRESHOLD_MIN = 0
const SCORE_THRESHOLD_MAX = 1
const SCORE_THRESHOLD_STEP = 0.01
const TOP_K_DEFAULT = 10
const SCORE_THRESHOLD_DEFAULT = 0

// ── Helpers ──

function scoreColor(score: number): string {
  if (score >= 0.8) return 'text-emerald-600 dark:text-emerald-400'
  if (score >= 0.6) return 'text-amber-600 dark:text-amber-400'
  return 'text-muted-foreground'
}

// ── Main component ──

export function KnowledgeSearchPage() {
  // ── State ──
  const [query, setQuery] = useState('')
  const [availableKbs, setAvailableKbs] = useState<KnowledgeBaseSummary[]>([])
  const [selectedKbIds, setSelectedKbIds] = useState<string[]>([])
  const [kbsLoading, setKbsLoading] = useState(true)
  const [kbsError, setKbsError] = useState<string | null>(null)

  // Search options
  const [useRerank, setUseRerank] = useState(false)
  const [topK, setTopK] = useState(TOP_K_DEFAULT)
  const [scoreThreshold, setScoreThreshold] = useState(SCORE_THRESHOLD_DEFAULT)
  const [tagFilter, setTagFilter] = useState('')

  // Advanced options toggle
  const [showAdvanced, setShowAdvanced] = useState(false)

  // ── Mutation ──

  const searchMutation = useKnowledgeSearch()

  // Result state (we keep results separately since mutation doesn't provide data caching)
  const [resultSummary, setResultSummary] = useState<{
    query: string
    results: KnowledgeQueryResult[]
    trace: {
      embeddingProvider: string
      embeddingModel: string
      embeddingDimension: number
      qdrantCollection: string
      searchTopK: number
      scoreThreshold: number
      hitCount: number
      rerank: boolean
      rerankTopN?: number | null
    }
  } | null>(null)

  // ── Load available knowledge bases ──

  useEffect(() => {
    let cancelled = false
    setKbsLoading(true)
    setKbsError(null)

    // Fetch all KBs within the runtime API page-size cap.
    listKnowledgeBases({ page: 1, pageSize: 100 })
      .then((result) => {
        if (!cancelled) {
          setAvailableKbs(result.items)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setKbsError(getGatewayCapabilityIssue(err, '知识库列表').description)
        }
      })
      .finally(() => {
        if (!cancelled) setKbsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  // ── Handlers ──

  const toggleKb = useCallback((id: string) => {
    setSelectedKbIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }, [])

  const selectAllKbs = useCallback(() => {
    if (selectedKbIds.length === availableKbs.length) {
      setSelectedKbIds([])
    } else {
      setSelectedKbIds(availableKbs.map((kb) => kb.id))
    }
  }, [selectedKbIds.length, availableKbs])

  const handleSearch = useCallback(() => {
    if (!query.trim()) return
    setResultSummary(null)

    const tags = tagFilter
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean)

    searchMutation.mutate(
      {
        query: query.trim(),
        knowledgeBaseIds: selectedKbIds.length > 0 ? selectedKbIds : undefined,
        topK,
        scoreThreshold,
        tags: tags.length > 0 ? tags : undefined,
        rerank: useRerank ? true : false,
        rerankTopN: useRerank ? topK : undefined,
      },
      {
        onSuccess: (data) => {
          setResultSummary({
            query: data.query,
            results: data.results,
            trace: data.trace,
          })
        },
      },
    )
  }, [query, selectedKbIds, topK, scoreThreshold, tagFilter, useRerank, searchMutation])

  const searchIssue = searchMutation.isError
    ? getGatewayCapabilityIssue(searchMutation.error, '知识检索')
    : null

  // Handle Enter key
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') {
        handleSearch()
      }
    },
    [handleSearch],
  )

  // ── Render ──

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-6">
      {/* Header */}
      <div>
        <h3 className="text-2xl font-semibold text-foreground">知识检索</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          在知识库中检索相关内容，支持语义向量检索和向量重排序。
        </p>
      </div>

      <section className="space-y-4 rounded-lg border border-border bg-card p-5">
        {/* Search input */}
        <div>
          <div className="relative">
            <Search
              aria-hidden="true"
              className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              type="text"
              maxLength={500}
              placeholder="输入检索关键词或问题..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleKeyDown}
              className="h-10 pl-9 pr-20"
            />
            <Button
              size="sm"
              className="!absolute right-1 top-1/2 -translate-y-1/2"
              onClick={handleSearch}
              disabled={!query.trim() || searchMutation.isPending}
            >
              {searchMutation.isPending ? (
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              ) : (
                '检索'
              )}
            </Button>
          </div>
        </div>

        {/* Knowledge base selection */}
        <div>
          <p className="mb-1.5 text-xs font-medium text-muted-foreground">知识库范围</p>
          {kbsLoading && (
            <InlineNotice icon={Loader2} iconClassName="animate-spin">
              加载知识库...
            </InlineNotice>
          )}
          {kbsError && <InlineNotice variant="error">加载知识库失败: {kbsError}</InlineNotice>}
          {!kbsLoading && !kbsError && (
            <div className="flex flex-wrap gap-1.5">
              <label className="flex cursor-pointer items-center gap-1.5 rounded-lg border border-border px-2.5 py-1 text-xs transition-colors hover:bg-muted/30">
                <input
                  type="checkbox"
                  className="size-3 accent-primary"
                  checked={availableKbs.length > 0 && selectedKbIds.length === availableKbs.length}
                  onChange={selectAllKbs}
                />
                全选
              </label>
              {availableKbs.map((kb) => (
                <label
                  key={kb.id}
                  className={`flex cursor-pointer items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs transition-colors ${
                    selectedKbIds.includes(kb.id)
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:bg-muted/30'
                  }`}
                >
                  <input
                    type="checkbox"
                    className="size-3 accent-primary"
                    checked={selectedKbIds.includes(kb.id)}
                    onChange={() => toggleKb(kb.id)}
                  />
                  {kb.name}
                  <span className="text-muted-foreground">({kb.documentCount})</span>
                </label>
              ))}
              {availableKbs.length === 0 && (
                <span className="text-xs text-muted-foreground">暂无可用知识库</span>
              )}
            </div>
          )}
        </div>

        {/* Advanced options toggle */}
        <div>
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2 text-xs text-muted-foreground"
            onClick={() => setShowAdvanced((v) => !v)}
          >
            {showAdvanced ? (
              <ChevronUp aria-hidden="true" className="mr-1 size-3.5" />
            ) : (
              <ChevronDown aria-hidden="true" className="mr-1 size-3.5" />
            )}
            高级选项
          </Button>
        </div>

        <div
          inert={!showAdvanced}
          className={cn(
            'overflow-hidden transition-[max-height,opacity] duration-300 ease-out',
            showAdvanced ? 'max-h-[500px] opacity-100' : 'max-h-0 opacity-0',
          )}
        >
          <div className="space-y-3 rounded-lg border border-border bg-background p-4">
            {/* Search mode toggle */}
            <div>
              <label className="mb-1 block text-xs font-medium text-muted-foreground">
                检索模式
              </label>
              <div className="flex gap-2">
                <label
                  className={`flex cursor-pointer items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors ${
                    !useRerank
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:bg-muted/30'
                  }`}
                >
                  <input
                    type="radio"
                    name="search-mode"
                    className="size-3 accent-primary"
                    checked={!useRerank}
                    onChange={() => setUseRerank(false)}
                  />
                  语义向量检索
                </label>
                <label
                  className={`flex cursor-pointer items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors ${
                    useRerank
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:bg-muted/30'
                  }`}
                >
                  <input
                    type="radio"
                    name="search-mode"
                    className="size-3 accent-primary"
                    checked={useRerank}
                    onChange={() => setUseRerank(true)}
                  />
                  向量 + 重排序
                </label>
              </div>
            </div>

            {/* Top K */}
            <div>
              <div className="mb-1 flex items-center justify-between">
                <label htmlFor="search-topk" className="text-xs font-medium text-muted-foreground">
                  返回结果数 (Top K)
                </label>
                <span className="text-xs text-muted-foreground min-w-[2rem] text-right font-mono">
                  {topK}
                </span>
              </div>
              <input
                id="search-topk"
                type="range"
                min={TOP_K_MIN}
                max={TOP_K_MAX}
                value={topK}
                onChange={(e) => setTopK(Number(e.target.value))}
                className="range-slider w-full accent-primary"
              />
              <div className="mt-0.5 flex justify-between text-[0.65rem] text-muted-foreground">
                <span>{TOP_K_MIN}</span>
                <span>{TOP_K_MAX}</span>
              </div>
            </div>

            {/* Score threshold */}
            <div>
              <div className="mb-1 flex items-center justify-between">
                <label htmlFor="search-score" className="text-xs font-medium text-muted-foreground">
                  相似度阈值
                </label>
                <span className="text-xs text-muted-foreground min-w-[2rem] text-right font-mono">
                  {scoreThreshold.toFixed(2)}
                </span>
              </div>
              <input
                id="search-score"
                type="range"
                min={SCORE_THRESHOLD_MIN}
                max={SCORE_THRESHOLD_MAX}
                step={SCORE_THRESHOLD_STEP}
                value={scoreThreshold}
                onChange={(e) => setScoreThreshold(Number(e.target.value))}
                className="range-slider w-full accent-primary"
              />
              <div className="mt-0.5 flex justify-between text-[0.65rem] text-muted-foreground">
                <span>0.00</span>
                <span>1.00</span>
              </div>
            </div>

            {/* Tag filter */}
            <div>
              <label
                htmlFor="search-tags"
                className="mb-1 block text-xs font-medium text-muted-foreground"
              >
                标签筛选（逗号分隔）
              </label>
              <Input
                id="search-tags"
                type="text"
                maxLength={200}
                placeholder="例如: 规程, 安全"
                value={tagFilter}
                onChange={(e) => setTagFilter(e.target.value)}
              />
            </div>
          </div>
        </div>

        {/* Error */}
        {searchIssue && (
          <InlineNotice title={searchIssue.title} variant={searchIssue.variant}>
            {searchIssue.description}
          </InlineNotice>
        )}
      </section>

      {/* Results */}
      {resultSummary && (
        <div className="space-y-4">
          {/* Trace info */}
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>查询: &ldquo;{resultSummary.query}&rdquo;</span>
            <span>|</span>
            <span>命中 {resultSummary.trace.hitCount} 条结果</span>
            <span>|</span>
            <span>嵌入服务: {resultSummary.trace.embeddingProvider}</span>
            <span>|</span>
            <span>模型: {resultSummary.trace.embeddingModel}</span>
            {resultSummary.trace.rerank && (
              <>
                <span>|</span>
                <span>重排序: Top {resultSummary.trace.rerankTopN ?? '-'}</span>
              </>
            )}
          </div>

          {/* Empty results */}
          {resultSummary.results.length === 0 && (
            <StateBlock icon={BookOpen} title="未找到相关内容，请调整检索条件" variant="empty" />
          )}

          {/* Result cards */}
          {resultSummary.results.length > 0 && (
            <div className="space-y-3">
              {resultSummary.results.map((item, index) => (
                <div
                  key={item.chunkId}
                  className="rounded-lg border border-border bg-card p-4 transition-colors hover:border-muted-foreground/20"
                >
                  <div className="mb-2 flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-foreground">
                        {item.documentName}
                      </p>
                      {item.sectionPath && (
                        <p className="truncate text-xs text-muted-foreground">{item.sectionPath}</p>
                      )}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {item.tags && item.tags.length > 0 && (
                        <div className="hidden gap-1 sm:flex">
                          {item.tags.slice(0, 3).map((tag) => (
                            <Badge key={tag} variant="secondary" className="text-[0.65rem]">
                              {tag}
                            </Badge>
                          ))}
                        </div>
                      )}
                      <span
                        className={`whitespace-nowrap text-xs font-mono tabular-nums ${scoreColor(item.score)}`}
                        title="相似度得分"
                      >
                        {item.score.toFixed(4)}
                      </span>
                    </div>
                  </div>

                  <p className="line-clamp-3 text-sm text-muted-foreground">
                    {item.contentPreview}
                  </p>

                  <div className="mt-2 flex items-center gap-3 text-xs text-muted-foreground">
                    <span>#{index + 1}</span>
                    {item.chunkIndex != null && <span>分块 #{item.chunkIndex}</span>}
                    <span className="ml-auto">文档 ID: {item.documentId.slice(0, 8)}...</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Empty initial state (no search yet) */}
      {!resultSummary && !searchMutation.isPending && !searchMutation.isError && (
        <StateBlock
          icon={Search}
          title="输入检索词并选择知识库范围，开始检索相关内容"
          variant="empty"
        />
      )}

      {/* Loading state */}
      {searchMutation.isPending && (
        <StateBlock size="compact" title="检索中..." variant="loading" />
      )}
    </div>
  )
}
