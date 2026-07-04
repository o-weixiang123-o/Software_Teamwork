import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, FileText, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getDocument, getKnowledgeBase } from '@/api/knowledge'
import { StateBlock } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { getGatewayCapabilityIssue, useChunks } from '@/features/knowledge'
import type { DocumentStatus, DocumentSummary } from '@/lib/types'

// ── Constants ──

const PAGE_SIZE = 50

const STATUS_LABELS: Record<DocumentStatus, string> = {
  uploaded: '已上传',
  parsing: '解析中',
  chunking: '分块中',
  embedding: '向量化中',
  ready: '就绪',
  failed: '失败',
}

const STATUS_VARIANTS: Record<DocumentStatus, 'default' | 'secondary' | 'destructive' | 'outline'> =
  {
    uploaded: 'secondary',
    parsing: 'default',
    chunking: 'default',
    embedding: 'default',
    ready: 'outline',
    failed: 'destructive',
  }

// ── Helpers ──

function formatDateTime(iso?: string | null): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleDateString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return iso
  }
}

// ── Skeleton ──

function ChunkListSkeleton() {
  return (
    <div className="space-y-4">
      <div className="h-6 w-48 rounded skeleton-shimmer" />
      <div className="rounded-lg border border-border bg-card">
        <div className="border-b border-border px-4 py-3">
          <div className="grid grid-cols-5 gap-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="h-4 rounded skeleton-shimmer" />
            ))}
          </div>
        </div>
        <div className="divide-y divide-border">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="grid grid-cols-5 gap-3 px-4 py-3">
              {Array.from({ length: 5 }).map((_, j) => (
                <div key={j} className="h-4 rounded skeleton-shimmer" />
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

// ── Main component ──

interface KnowledgeChunksPageProps {
  documentId: string
  knowledgeBaseId: string
  onNavigateBack?: () => void
}

export function KnowledgeChunksPage({
  documentId,
  knowledgeBaseId,
  onNavigateBack,
}: KnowledgeChunksPageProps) {
  // ── State ──
  const [page, setPage] = useState(1)
  const [docInfo, setDocInfo] = useState<DocumentSummary | null>(null)
  const [kbName, setKbName] = useState<string>('-')
  const [docLoading, setDocLoading] = useState(true)
  const [docError, setDocError] = useState<string | null>(null)
  const [expandedChunkIds, setExpandedChunkIds] = useState<Set<string>>(() => new Set())

  // ── Query ──

  const { data, isLoading, isError, error, refetch } = useChunks(
    documentId,
    knowledgeBaseId,
    page,
    PAGE_SIZE,
  )

  // ── Fetch document info and KB name ──

  useEffect(() => {
    if (!documentId || !knowledgeBaseId) {
      setDocLoading(false)
      setDocError('缺少文档 ID 或知识库 ID 参数')
      return
    }
    if (!knowledgeBaseId) {
      setDocLoading(false)
      setDocError('缺少知识库 ID 参数')
      return
    }
    let cancelled = false
    setDocLoading(true)
    setDocError(null)

    getDocument(documentId, knowledgeBaseId)
      .then((doc) => {
        if (cancelled) return
        setDocInfo(doc)
        // Fetch KB name
        return getKnowledgeBase(doc.knowledgeBaseId).then((kb) => {
          if (!cancelled) setKbName(kb.name)
        })
      })
      .catch((err) => {
        if (!cancelled) {
          setDocError(getGatewayCapabilityIssue(err, '文档详情').description)
        }
      })
      .finally(() => {
        if (!cancelled) setDocLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [documentId, knowledgeBaseId])

  // ── Derived ──

  const totalPages = data ? Math.max(1, Math.ceil(data.page.total / PAGE_SIZE)) : 1
  const showPagination = totalPages > 1
  const isEmpty = !isLoading && !isError && data && data.items.length === 0
  const chunksIssue = isError ? getGatewayCapabilityIssue(error, '文档分块') : null

  function toggleChunkExpanded(chunkId: string) {
    setExpandedChunkIds((current) => {
      const next = new Set(current)
      if (next.has(chunkId)) {
        next.delete(chunkId)
      } else {
        next.add(chunkId)
      }
      return next
    })
  }

  // ── Render ──

  return (
    <div>
      {/* Header */}
      <div className="mb-6">
        {onNavigateBack && (
          <Button
            variant="ghost"
            size="sm"
            className="mb-3 -ml-2 text-muted-foreground"
            onClick={onNavigateBack}
          >
            <ChevronLeft aria-hidden="true" className="mr-1 size-4" />
            返回文档列表
          </Button>
        )}
        {docLoading && (
          <div className="flex items-center gap-2">
            <Loader2 aria-hidden="true" className="size-4 animate-spin text-muted-foreground" />
            <span className="text-sm text-muted-foreground">加载文档信息...</span>
          </div>
        )}
        {docError && (
          <StateBlock
            className="mb-4"
            description={docError}
            size="compact"
            title="加载文档信息失败"
            variant="error"
          />
        )}
        {docInfo && !docLoading && (
          <div>
            <h3 className="text-2xl font-semibold text-foreground">文档分块 - {docInfo.name}</h3>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <Badge variant={STATUS_VARIANTS[docInfo.status] ?? 'secondary'}>
                {STATUS_LABELS[docInfo.status] ?? docInfo.status}
              </Badge>
              <span>知识库: {kbName}</span>
              <span>|</span>
              <span>共 {docInfo.chunkCount} 个分块</span>
              {docInfo.contentType && (
                <>
                  <span>|</span>
                  <span>{docInfo.contentType}</span>
                </>
              )}
              {docInfo.createdAt && (
                <>
                  <span>|</span>
                  <span>上传于 {formatDateTime(docInfo.createdAt)}</span>
                </>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Loading state */}
      {isLoading && <ChunkListSkeleton />}

      {/* Error state */}
      {isError && !isLoading && (
        <StateBlock
          action={
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <Loader2 aria-hidden="true" className="mr-1.5 size-3.5" />
              重试
            </Button>
          }
          description={chunksIssue?.description ?? '未知错误'}
          size="compact"
          title={chunksIssue?.title ?? '加载分块列表失败'}
          variant={
            chunksIssue?.kind === 'forbidden' ? 'forbidden' : (chunksIssue?.variant ?? 'error')
          }
        />
      )}

      {/* Data area */}
      {!isLoading && !isError && (
        <>
          {/* Empty state */}
          {isEmpty && (
            <div className="rounded-lg border border-dashed border-border p-12 text-center">
              <FileText
                aria-hidden="true"
                className="mx-auto mb-3 size-10 text-muted-foreground/40"
              />
              <p className="text-sm text-muted-foreground">此文档暂无分块数据</p>
            </div>
          )}

          {/* Chunks table */}
          {data && data.items.length > 0 && (
            <>
              <div className="overflow-x-auto rounded-lg border border-border bg-card">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/30">
                      <th className="w-16 px-4 py-2.5 text-right font-medium text-muted-foreground">
                        #
                      </th>
                      <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                        内容预览
                      </th>
                      <th className="hidden w-24 px-4 py-2.5 text-right font-medium text-muted-foreground md:table-cell">
                        Token 数
                      </th>
                      <th className="hidden w-24 px-4 py-2.5 text-left font-medium text-muted-foreground lg:table-cell">
                        嵌入服务
                      </th>
                      <th className="hidden w-20 px-4 py-2.5 text-right font-medium text-muted-foreground lg:table-cell">
                        向量维度
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {data.items.map((chunk) => {
                      const isExpanded = expandedChunkIds.has(chunk.id)
                      const contentId = `chunk-content-${chunk.id}`

                      return (
                        <tr
                          key={chunk.id}
                          className="transition-colors duration-150 hover:bg-muted/30"
                        >
                          <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                            {chunk.chunkIndex}
                          </td>
                          <td className="max-w-[34rem] px-4 py-2.5">
                            <div
                              id={contentId}
                              className={
                                isExpanded
                                  ? 'whitespace-pre-wrap break-words text-foreground'
                                  : 'line-clamp-3 text-foreground'
                              }
                            >
                              {chunk.sectionPath && (
                                <span className="mb-0.5 block text-xs font-medium text-muted-foreground">
                                  {chunk.sectionPath}
                                </span>
                              )}
                              <span className="text-xs leading-relaxed">{chunk.content}</span>
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-2">
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-6 px-1.5 text-xs text-muted-foreground"
                                aria-controls={contentId}
                                aria-expanded={isExpanded}
                                onClick={() => toggleChunkExpanded(chunk.id)}
                              >
                                {isExpanded ? (
                                  <ChevronUp aria-hidden="true" className="mr-1 size-3.5" />
                                ) : (
                                  <ChevronDown aria-hidden="true" className="mr-1 size-3.5" />
                                )}
                                {isExpanded ? '收起' : '展开'}
                              </Button>
                              {chunk.chunkType && (
                                <Badge variant="secondary" className="text-[0.65rem]">
                                  {chunk.chunkType}
                                </Badge>
                              )}
                            </div>
                          </td>
                          <td className="hidden whitespace-nowrap px-4 py-2.5 text-right tabular-nums text-muted-foreground md:table-cell">
                            {chunk.tokenCount}
                          </td>
                          <td className="hidden whitespace-nowrap px-4 py-2.5 text-muted-foreground lg:table-cell">
                            {chunk.embeddingProvider ?? '-'}
                          </td>
                          <td className="hidden whitespace-nowrap px-4 py-2.5 text-right tabular-nums text-muted-foreground lg:table-cell">
                            {chunk.embeddingDimension ?? '-'}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>

              {/* Pagination */}
              {showPagination && (
                <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
                  <span>
                    共 {data.page.total} 条，第 {page} / {totalPages} 页
                  </span>
                  <div className="flex gap-1">
                    <Button
                      variant="outline"
                      size="icon-sm"
                      className="hover:scale-105 active:scale-95 transition-transform"
                      disabled={page <= 1}
                      onClick={() => setPage((p) => p - 1)}
                      aria-label="上一页"
                    >
                      <ChevronLeft aria-hidden="true" className="size-3.5" />
                    </Button>
                    <Button
                      variant="outline"
                      size="icon-sm"
                      className="hover:scale-105 active:scale-95 transition-transform"
                      disabled={page >= totalPages}
                      onClick={() => setPage((p) => p + 1)}
                      aria-label="下一页"
                    >
                      <ChevronRight aria-hidden="true" className="size-3.5" />
                    </Button>
                  </div>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  )
}
