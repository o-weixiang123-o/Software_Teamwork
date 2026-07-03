import { Link } from '@tanstack/react-router'
import {
  BookOpen,
  ChevronLeft,
  ChevronRight,
  Edit,
  FileText,
  Loader2,
  Plus,
  Search,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'

import { InlineNotice, StateBlock } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  formatGatewayCapabilityError,
  getGatewayCapabilityIssue,
  useCreateKnowledgeBase,
  useDeleteKnowledgeBase,
  useKnowledgeBases,
  useUpdateKnowledgeBase,
} from '@/features/knowledge'
import type { KnowledgeBaseSummary } from '@/lib/types'

// ── Constants ──

const PAGE_SIZE = 10

const DOC_TYPE_OPTIONS = [
  { value: 'manual', label: '规程规范' },
  { value: 'paper', label: '技术报告论文' },
  { value: 'qa', label: '术语条目' },
  { value: 'naive', label: '通用文档' },
] as const

type DocTypeValue = (typeof DOC_TYPE_OPTIONS)[number]['value']

const DOC_TYPE_LABELS: Record<DocTypeValue, string> = DOC_TYPE_OPTIONS.reduce(
  (labels, option) => ({ ...labels, [option.value]: option.label }),
  {} as Record<DocTypeValue, string>,
)

const LEGACY_DOC_TYPE_VALUES: Record<string, DocTypeValue> = {
  GENERAL: 'naive',
  MANUAL: 'manual',
  PAPER: 'paper',
  QA: 'qa',
  规程规范: 'manual',
  技术报告论文: 'paper',
  术语条目: 'qa',
  通用文档: 'naive',
}

const RETRIEVAL_MODE_OPTIONS = [
  { value: 'semantic', label: '语义检索' },
  { value: 'vector_rerank', label: '向量重排序' },
] as const

const RETRIEVAL_MODE_LABELS: Record<string, string> = {
  semantic: '语义检索',
  vector_rerank: '向量重排序',
}

function normalizeDocType(value: string | null | undefined): DocTypeValue {
  const trimmed = value?.trim()
  if (!trimmed) return 'naive'
  if (trimmed in DOC_TYPE_LABELS) return trimmed as DocTypeValue
  return LEGACY_DOC_TYPE_VALUES[trimmed] ?? 'naive'
}

function formatDocType(value: string | null | undefined): string {
  const trimmed = value?.trim()
  if (!trimmed) return '-'
  if (trimmed in DOC_TYPE_LABELS || trimmed in LEGACY_DOC_TYPE_VALUES) {
    return DOC_TYPE_LABELS[normalizeDocType(trimmed)]
  }
  return trimmed
}

// ── Types ──

type FormData = {
  name: string
  description: string
  docType: string
  retrievalMode: string
}

type NotificationState = {
  type: 'success' | 'error'
  text: string
}

// ── Defaults ──

const EMPTY_FORM: FormData = {
  name: '',
  description: '',
  docType: 'naive',
  retrievalMode: 'semantic',
}

// ── Skeleton ──

function KnowledgeManagementSkeleton() {
  return (
    <div className="space-y-4">
      {/* Header skeleton */}
      <div className="flex items-center justify-between">
        <div className="h-7 w-32 rounded skeleton-shimmer" />
        <div className="h-8 w-28 rounded skeleton-shimmer" />
      </div>

      {/* Search skeleton */}
      <div className="flex gap-2">
        <div className="h-8 flex-1 rounded skeleton-shimmer" />
        <div className="h-8 w-36 rounded skeleton-shimmer" />
      </div>

      {/* Table skeleton */}
      <div className="rounded-lg border border-border bg-card">
        <div className="border-b border-border px-4 py-3">
          <div className="grid grid-cols-6 gap-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="h-4 rounded skeleton-shimmer" />
            ))}
          </div>
        </div>
        <div className="divide-y divide-border">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="grid grid-cols-6 gap-4 px-4 py-3">
              {Array.from({ length: 6 }).map((_, j) => (
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

export function KnowledgeManagement() {
  // ── State ──
  const [keyword, setKeyword] = useState('')
  const [docTypeFilter, setDocTypeFilter] = useState('')
  const [page, setPage] = useState(1)

  const [createOpen, setCreateOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [editingKb, setEditingKb] = useState<KnowledgeBaseSummary | null>(null)
  const [deletingKb, setDeletingKb] = useState<KnowledgeBaseSummary | null>(null)

  const [form, setForm] = useState<FormData>(EMPTY_FORM)
  const [notification, setNotification] = useState<NotificationState | null>(null)

  // ── Queries & mutations ──

  const { data, isLoading, isError, error, refetch } = useKnowledgeBases(
    page,
    PAGE_SIZE,
    keyword || undefined,
    docTypeFilter || undefined,
  )

  const createMutation = useCreateKnowledgeBase()
  const updateMutation = useUpdateKnowledgeBase()
  const deleteMutation = useDeleteKnowledgeBase()

  const isMutating =
    createMutation.isPending || updateMutation.isPending || deleteMutation.isPending

  // ── Notification auto-dismiss ──

  useEffect(() => {
    if (!notification) return
    const timer = setTimeout(() => setNotification(null), 4000)
    return () => clearTimeout(timer)
  }, [notification])

  // ── Handlers ──

  const updateField = useCallback((field: keyof FormData, value: string) => {
    setForm((prev) => ({ ...prev, [field]: value }))
  }, [])

  const openCreate = useCallback(() => {
    setForm(EMPTY_FORM)
    setCreateOpen(true)
  }, [])

  const openEdit = useCallback((kb: KnowledgeBaseSummary) => {
    setEditingKb(kb)
    setForm({
      name: kb.name,
      description: kb.description ?? '',
      docType: normalizeDocType(kb.docType),
      retrievalMode: kb.retrievalStrategy?.mode ?? 'semantic',
    })
    setEditOpen(true)
  }, [])

  const openDelete = useCallback((kb: KnowledgeBaseSummary) => {
    setDeletingKb(kb)
    setDeleteOpen(true)
  }, [])

  const handleCreate = useCallback(() => {
    createMutation.mutate(
      {
        name: form.name,
        description: form.description,
        docType: form.docType,
        retrievalStrategy: { mode: form.retrievalMode },
      },
      {
        onSuccess: () => {
          setNotification({ type: 'success', text: '知识库创建成功' })
          setCreateOpen(false)
          setPage(1)
        },
        onError: (err: Error) => {
          setNotification({
            type: 'error',
            text: formatGatewayCapabilityError(err, '知识库创建'),
          })
        },
      },
    )
  }, [form, createMutation])

  const handleEdit = useCallback(() => {
    if (!editingKb) return
    updateMutation.mutate(
      {
        id: editingKb.id,
        name: form.name,
        description: form.description,
        docType: form.docType,
        retrievalStrategy: { mode: form.retrievalMode },
      },
      {
        onSuccess: () => {
          setNotification({ type: 'success', text: '知识库更新成功' })
          setEditOpen(false)
          setEditingKb(null)
        },
        onError: (err: Error) => {
          setNotification({
            type: 'error',
            text: formatGatewayCapabilityError(err, '知识库更新'),
          })
        },
      },
    )
  }, [editingKb, form, updateMutation])

  const handleDelete = useCallback(() => {
    if (!deletingKb) return
    deleteMutation.mutate(deletingKb.id, {
      onSuccess: () => {
        setNotification({ type: 'success', text: '知识库已删除' })
        setDeleteOpen(false)
        setDeletingKb(null)
      },
      onError: (err: Error) => {
        setNotification({
          type: 'error',
          text: formatGatewayCapabilityError(err, '知识库删除'),
        })
      },
    })
  }, [deletingKb, deleteMutation])

  const handleSearch = useCallback((value: string) => {
    setKeyword(value)
    setPage(1)
  }, [])

  const handleDocTypeFilter = useCallback((value: string) => {
    setDocTypeFilter(value)
    setPage(1)
  }, [])

  // ── Derived ──

  const totalPages = data ? Math.max(1, Math.ceil(data.page.total / PAGE_SIZE)) : 1
  const showPagination = totalPages > 1
  const isEmpty = !isLoading && !isError && data && data.items.length === 0
  const knowledgeBaseIssue = isError ? getGatewayCapabilityIssue(error, '知识库列表') : null

  // ── Render ──

  return (
    <div>
      {/* Header */}
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h3 className="text-2xl font-semibold text-foreground">知识库管理</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            管理知识库，支持新建、编辑、删除操作。
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus aria-hidden="true" className="mr-1 size-4" />
          新建知识库
        </Button>
      </div>

      {/* Toast notification */}
      {notification && (
        <InlineNotice className="toast-enter mb-4" variant={notification.type}>
          {notification.text}
        </InlineNotice>
      )}

      {/* Loading state */}
      {isLoading && <KnowledgeManagementSkeleton />}

      {/* Error state */}
      {isError && !isLoading && (
        <StateBlock
          action={
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <Loader2 aria-hidden="true" className="mr-1.5 size-3.5" />
              重试
            </Button>
          }
          description={knowledgeBaseIssue?.description ?? '未知错误'}
          size="compact"
          title={knowledgeBaseIssue?.title ?? '加载知识库失败'}
          variant={
            knowledgeBaseIssue?.kind === 'forbidden'
              ? 'forbidden'
              : (knowledgeBaseIssue?.variant ?? 'error')
          }
        />
      )}

      {/* Data area */}
      {!isLoading && !isError && (
        <>
          {/* Search & filter bar */}
          <div className="mb-4 flex gap-2">
            <div className="search-expand relative flex-1">
              <Search
                aria-hidden="true"
                className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              />
              <Input
                type="text"
                placeholder="搜索知识库名称或描述..."
                value={keyword}
                onChange={(e) => handleSearch(e.target.value)}
                className="pl-8"
              />
            </div>
            <Select
              value={docTypeFilter || undefined}
              onValueChange={(v) => handleDocTypeFilter(String(v))}
            >
              <SelectTrigger className="h-8 w-auto min-w-[120px]">
                <SelectValue placeholder="全部类型" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">全部类型</SelectItem>
                {DOC_TYPE_OPTIONS.map((dt) => (
                  <SelectItem key={dt.value} value={dt.value}>
                    {dt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Filter limitation notice */}
          {(keyword || docTypeFilter) && data?.filteredLocally && (
            <p className="text-xs text-muted-foreground">
              搜索仅过滤当前页，其他页的匹配项不会显示。建议使用准确关键词，或切换到更大页码查看。
            </p>
          )}

          {/* Empty state */}
          {isEmpty && (
            <div className="rounded-lg border border-dashed border-border p-12 text-center">
              <BookOpen
                aria-hidden="true"
                className="mx-auto mb-3 size-10 text-muted-foreground/40"
              />
              <p className="text-sm text-muted-foreground">
                {keyword || docTypeFilter
                  ? '未找到匹配的知识库，请调整搜索条件'
                  : '暂无知识库，点击新建知识库开始'}
              </p>
              {!keyword && !docTypeFilter && (
                <Button variant="outline" size="sm" className="mt-3" onClick={openCreate}>
                  <Plus aria-hidden="true" className="mr-1 size-3.5" />
                  新建知识库
                </Button>
              )}
            </div>
          )}

          {/* Table */}
          {data && data.items.length > 0 && (
            <>
              <div className="overflow-x-auto rounded-lg border border-border bg-card">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/30">
                      <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                        名称
                      </th>
                      <th className="hidden px-4 py-2.5 text-left font-medium text-muted-foreground md:table-cell">
                        描述
                      </th>
                      <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                        类型
                      </th>
                      <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                        文档数
                      </th>
                      <th className="hidden px-4 py-2.5 text-left font-medium text-muted-foreground sm:table-cell">
                        检索策略
                      </th>
                      <th className="hidden px-4 py-2.5 text-left font-medium text-muted-foreground lg:table-cell">
                        创建时间
                      </th>
                      <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">
                        操作
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {data.items.map((kb) => (
                      <tr key={kb.id} className="transition-colors duration-150 hover:bg-muted/30">
                        <td className="max-w-40 truncate px-4 py-2.5 font-medium text-foreground">
                          {kb.name}
                        </td>
                        <td className="hidden max-w-48 truncate px-4 py-2.5 text-muted-foreground md:table-cell">
                          {kb.description || '-'}
                        </td>
                        <td className="px-4 py-2.5">
                          {kb.docType ? (
                            <Badge variant="secondary">{formatDocType(kb.docType)}</Badge>
                          ) : (
                            <span className="text-muted-foreground">-</span>
                          )}
                        </td>
                        <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                          {kb.documentCount}
                        </td>
                        <td className="hidden px-4 py-2.5 text-muted-foreground sm:table-cell">
                          {kb.retrievalStrategy?.mode
                            ? (RETRIEVAL_MODE_LABELS[kb.retrievalStrategy.mode] ??
                              kb.retrievalStrategy.mode)
                            : '-'}
                        </td>
                        <td className="hidden whitespace-nowrap px-4 py-2.5 text-muted-foreground lg:table-cell">
                          {kb.createdAt ? new Date(kb.createdAt).toLocaleDateString('zh-CN') : '-'}
                        </td>
                        <td className="px-4 py-2.5">
                          <div className="flex items-center justify-end gap-1">
                            <Link
                              to="/admin/knowledge/documents"
                              search={{ knowledgeBaseId: kb.id }}
                              className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                              title="文档管理"
                              aria-label={`管理 ${kb.name} 的文档`}
                            >
                              <FileText aria-hidden="true" className="size-3.5" />
                            </Link>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => openEdit(kb)}
                              aria-label={`编辑 ${kb.name}`}
                            >
                              <Edit aria-hidden="true" className="size-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => openDelete(kb)}
                              aria-label={`删除 ${kb.name}`}
                              className="text-destructive hover:text-destructive"
                            >
                              <Trash2 aria-hidden="true" className="size-3.5" />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
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

      {/* ── Create Dialog ── */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建知识库</DialogTitle>
            <DialogDescription>填写知识库的基本信息。</DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            {/* Name */}
            <div>
              <label
                htmlFor="kb-create-name"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                名称 <span className="text-destructive">*</span>
              </label>
              <Input
                id="kb-create-name"
                type="text"
                placeholder="知识库名称"
                value={form.name}
                onChange={(e) => updateField('name', e.target.value)}
              />
            </div>

            {/* Description */}
            <div>
              <label
                htmlFor="kb-create-desc"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                描述
              </label>
              <Input
                id="kb-create-desc"
                type="text"
                placeholder="知识库描述（可选）"
                value={form.description}
                onChange={(e) => updateField('description', e.target.value)}
              />
            </div>

            {/* DocType */}
            <div>
              <label
                htmlFor="kb-create-doctype"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                文档类型
              </label>
              <Select value={form.docType} onValueChange={(v) => updateField('docType', String(v))}>
                <SelectTrigger id="kb-create-doctype" className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DOC_TYPE_OPTIONS.map((dt) => (
                    <SelectItem key={dt.value} value={dt.value}>
                      {dt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Retrieval Strategy */}
            <div>
              <label
                htmlFor="kb-create-retrieval"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                检索策略
              </label>
              <Select
                value={form.retrievalMode}
                onValueChange={(v) => updateField('retrievalMode', String(v))}
              >
                <SelectTrigger id="kb-create-retrieval" className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RETRIEVAL_MODE_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={isMutating}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={!form.name.trim() || isMutating}>
              {createMutation.isPending && (
                <Loader2 aria-hidden="true" className="mr-1.5 size-3.5 animate-spin" />
              )}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Edit Dialog ── */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑知识库</DialogTitle>
            <DialogDescription>修改 "{editingKb?.name}" 的配置信息。</DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            {/* Name */}
            <div>
              <label
                htmlFor="kb-edit-name"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                名称 <span className="text-destructive">*</span>
              </label>
              <Input
                id="kb-edit-name"
                type="text"
                placeholder="知识库名称"
                value={form.name}
                onChange={(e) => updateField('name', e.target.value)}
              />
            </div>

            {/* Description */}
            <div>
              <label
                htmlFor="kb-edit-desc"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                描述
              </label>
              <Input
                id="kb-edit-desc"
                type="text"
                placeholder="知识库描述（可选）"
                value={form.description}
                onChange={(e) => updateField('description', e.target.value)}
              />
            </div>

            {/* DocType */}
            <div>
              <label
                htmlFor="kb-edit-doctype"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                文档类型
              </label>
              <Select value={form.docType} onValueChange={(v) => updateField('docType', String(v))}>
                <SelectTrigger id="kb-edit-doctype" className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DOC_TYPE_OPTIONS.map((dt) => (
                    <SelectItem key={dt.value} value={dt.value}>
                      {dt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Retrieval Strategy */}
            <div>
              <label
                htmlFor="kb-edit-retrieval"
                className="mb-1 block text-sm font-medium text-foreground"
              >
                检索策略
              </label>
              <Select
                value={form.retrievalMode}
                onValueChange={(v) => updateField('retrievalMode', String(v))}
              >
                <SelectTrigger id="kb-edit-retrieval" className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RETRIEVAL_MODE_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setEditOpen(false)
                setEditingKb(null)
              }}
              disabled={isMutating}
            >
              取消
            </Button>
            <Button onClick={handleEdit} disabled={!form.name.trim() || isMutating}>
              {updateMutation.isPending && (
                <Loader2 aria-hidden="true" className="mr-1.5 size-3.5 animate-spin" />
              )}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Delete Confirmation Dialog ── */}
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除知识库 "{deletingKb?.name}"
              吗？此操作不可撤销，知识库中的所有文档也将被删除。
            </DialogDescription>
          </DialogHeader>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setDeleteOpen(false)
                setDeletingKb(null)
              }}
              disabled={isMutating}
            >
              取消
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={isMutating}>
              {deleteMutation.isPending && (
                <Loader2 aria-hidden="true" className="mr-1.5 size-3.5 animate-spin" />
              )}
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
