/**
 * React Query hooks for document CRUD, chunks, content, and knowledge search.
 *
 * Server state managed by TanStack Query with client-side caching and optimisations.
 */

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import type { KnowledgeQueryRequest, UpdateDocumentRequest } from '@/api/knowledge'
import {
  deleteDocument,
  getDocument,
  getDocumentContent,
  listChunks,
  listDocuments,
  runKnowledgeQuery,
  updateDocument,
  uploadDocument,
} from '@/api/knowledge'
import type { DocumentStatus } from '@/lib/types'

// ── Query keys ──

export const documentKeys = {
  all: ['documents'] as const,
  lists: () => [...documentKeys.all, 'list'] as const,
  list: (knowledgeBaseId: string, page: number, pageSize: number, status?: string) =>
    [...documentKeys.lists(), { knowledgeBaseId, page, pageSize, status }] as const,
  details: () => [...documentKeys.all, 'detail'] as const,
  detail: (id: string, knowledgeBaseId: string) =>
    [...documentKeys.details(), { id, knowledgeBaseId }] as const,
  chunks: (documentId: string, knowledgeBaseId: string) =>
    [...documentKeys.all, 'chunks', { documentId, knowledgeBaseId }] as const,
  chunkPage: (documentId: string, knowledgeBaseId: string, page: number, pageSize: number) =>
    [...documentKeys.chunks(documentId, knowledgeBaseId), { page, pageSize }] as const,
  content: (documentId: string, knowledgeBaseId: string) =>
    [...documentKeys.all, 'content', { documentId, knowledgeBaseId }] as const,
  search: ['knowledge-search'] as const,
}

// ── Queries ──

/** Paginated document list for a knowledge base. */
export function useDocuments(
  knowledgeBaseId: string,
  page = 1,
  pageSize = 20,
  status?: DocumentStatus,
) {
  return useQuery({
    queryKey: documentKeys.list(knowledgeBaseId, page, pageSize, status),
    queryFn: () => listDocuments(knowledgeBaseId, { page, pageSize, status }),
    placeholderData: (prev) => prev,
    enabled: Boolean(knowledgeBaseId),
  })
}

/** Single document detail. */
export function useDocument(id: string, knowledgeBaseId: string) {
  return useQuery({
    queryKey: documentKeys.detail(id, knowledgeBaseId),
    queryFn: () => getDocument(id, knowledgeBaseId),
    enabled: id.length > 0 && knowledgeBaseId.length > 0,
  })
}

/** Paginated document chunks. */
export function useChunks(documentId: string, knowledgeBaseId: string, page = 1, pageSize = 50) {
  return useQuery({
    queryKey: documentKeys.chunkPage(documentId, knowledgeBaseId, page, pageSize),
    queryFn: () => listChunks(documentId, knowledgeBaseId, { page, pageSize }),
    placeholderData: (prev) => prev,
    enabled: Boolean(documentId) && Boolean(knowledgeBaseId),
  })
}

/** Document raw content as Blob (for download). */
export function useDocumentContent(documentId: string, knowledgeBaseId: string) {
  return useQuery({
    queryKey: documentKeys.content(documentId, knowledgeBaseId),
    queryFn: () => getDocumentContent(documentId, knowledgeBaseId),
    enabled: Boolean(documentId) && Boolean(knowledgeBaseId),
    staleTime: Infinity,
  })
}

// ── Mutations ──

/** Upload a document to a knowledge base. */
export function useUploadDocument() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({
      knowledgeBaseId,
      file,
      tags,
    }: {
      knowledgeBaseId: string
      file: File
      tags?: string[]
    }) => uploadDocument(knowledgeBaseId, file, tags),
    onSuccess: (_data, _variables) => {
      void queryClient.invalidateQueries({
        queryKey: documentKeys.lists(),
      })
      void queryClient.invalidateQueries({
        queryKey: ['knowledge-bases'],
      })
    },
  })
}

/** Update document metadata (tags). */
export function useUpdateDocument() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({
      id,
      knowledgeBaseId,
      ...params
    }: { id: string; knowledgeBaseId: string } & UpdateDocumentRequest) =>
      updateDocument(id, knowledgeBaseId, params),
    onSuccess: (_data, _variables) => {
      void queryClient.invalidateQueries({
        queryKey: documentKeys.lists(),
      })
      void queryClient.invalidateQueries({
        queryKey: documentKeys.detail(_variables.id, _variables.knowledgeBaseId),
      })
    },
  })
}

/** Delete a document. */
export function useDeleteDocument() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, knowledgeBaseId }: { id: string; knowledgeBaseId: string }) =>
      deleteDocument(id, knowledgeBaseId),
    onSuccess: (_data, variables) => {
      void queryClient.invalidateQueries({
        queryKey: documentKeys.lists(),
      })
      queryClient.removeQueries({
        queryKey: documentKeys.detail(variables.id, variables.knowledgeBaseId),
      })
      void queryClient.invalidateQueries({
        queryKey: ['knowledge-bases'],
      })
    },
  })
}

/** Run a knowledge retrieval query (search). */
export function useKnowledgeSearch() {
  return useMutation({
    mutationFn: (params: KnowledgeQueryRequest) => runKnowledgeQuery(params),
  })
}
