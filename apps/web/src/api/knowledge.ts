import type { components, paths } from '@/api/generated/gateway'

import {
  buildQuery,
  gatewayFileRequest,
  gatewayPageRequest,
  gatewayRequest,
  requestVoid,
} from './client'

type JsonResponseData<
  Path extends keyof paths,
  Method extends keyof paths[Path],
  Status extends number,
> = paths[Path][Method] extends {
  responses: Record<Status, { content: { 'application/json': { data: infer Data } } }>
}
  ? Data
  : never

type JsonRequestBody<
  Path extends keyof paths,
  Method extends keyof paths[Path],
> = paths[Path][Method] extends {
  requestBody: { content: { 'application/json': infer Body } }
}
  ? Body
  : never

type QueryParams<
  Path extends keyof paths,
  Method extends keyof paths[Path],
> = paths[Path][Method] extends { parameters: { query?: infer Query } } ? Query : never

type PaginatedResponseItem<
  Path extends keyof paths,
  Method extends keyof paths[Path],
  Status extends number,
> = paths[Path][Method] extends {
  responses: Record<Status, { content: { 'application/json': { data: (infer Item)[] } } }>
}
  ? Item
  : never

export type KnowledgePage = components['schemas']['PageInfo']

export type ListKnowledgeBasesParams = QueryParams<'/api/v1/knowledge-bases', 'get'>
export type CreateKnowledgeBaseRequest = JsonRequestBody<'/api/v1/knowledge-bases', 'post'>
export type UpdateKnowledgeBaseRequest = JsonRequestBody<
  '/api/v1/knowledge-bases/{knowledgeBaseId}',
  'patch'
>
export type KnowledgeBaseSummary =
  | JsonResponseData<'/api/v1/knowledge-bases', 'post', 201>
  | PaginatedResponseItem<'/api/v1/knowledge-bases', 'get', 200>

export type ListDocumentsParams = QueryParams<
  '/api/v1/knowledge-bases/{knowledgeBaseId}/documents',
  'get'
>
export type DocumentSummary = JsonResponseData<
  '/api/v1/knowledge-bases/{knowledgeBaseId}/documents',
  'post',
  201
> &
  PaginatedResponseItem<'/api/v1/knowledge-bases/{knowledgeBaseId}/documents', 'get', 200>
export type UpdateDocumentRequest = JsonRequestBody<'/api/v1/documents/{documentId}', 'patch'>
export type DocumentChunk = PaginatedResponseItem<
  '/api/v1/documents/{documentId}/chunks',
  'get',
  200
>

export type KnowledgeQueryRequest = JsonRequestBody<'/api/v1/knowledge-queries', 'post'>
export type KnowledgeQuerySummary = JsonResponseData<'/api/v1/knowledge-queries', 'post', 201>

export type KnowledgePageResult<T> = {
  items: T[]
  page: KnowledgePage
}

/** GET /knowledge-bases?page=&pageSize= */
export async function listKnowledgeBases(
  params: ListKnowledgeBasesParams = {},
): Promise<KnowledgePageResult<KnowledgeBaseSummary>> {
  return gatewayPageRequest<KnowledgeBaseSummary>(
    `/knowledge-bases${buildQuery({ page: params.page, pageSize: params.pageSize })}`,
  )
}

/** POST /knowledge-bases */
export function createKnowledgeBase(
  params: CreateKnowledgeBaseRequest,
): Promise<KnowledgeBaseSummary> {
  return gatewayRequest<KnowledgeBaseSummary>('/knowledge-bases', {
    method: 'POST',
    body: params,
  })
}

/** GET /knowledge-bases/{knowledgeBaseId} */
export function getKnowledgeBase(knowledgeBaseId: string): Promise<KnowledgeBaseSummary> {
  return gatewayRequest<KnowledgeBaseSummary>(
    `/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}`,
  )
}

/** PATCH /knowledge-bases/{knowledgeBaseId} */
export function updateKnowledgeBase(
  knowledgeBaseId: string,
  params: UpdateKnowledgeBaseRequest,
): Promise<KnowledgeBaseSummary> {
  return gatewayRequest<KnowledgeBaseSummary>(
    `/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}`,
    {
      method: 'PATCH',
      body: params,
    },
  )
}

/** DELETE /knowledge-bases/{knowledgeBaseId} */
export async function deleteKnowledgeBase(knowledgeBaseId: string): Promise<void> {
  await requestVoid(`/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}`, {
    method: 'DELETE',
  })
}

/** GET /knowledge-bases/{knowledgeBaseId}/documents?page=&pageSize=&status= */
export async function listDocuments(
  knowledgeBaseId: string,
  params: ListDocumentsParams = {},
): Promise<KnowledgePageResult<DocumentSummary>> {
  return gatewayPageRequest<DocumentSummary>(
    `/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/documents${buildQuery({
      page: params.page,
      pageSize: params.pageSize,
      status: params.status,
    })}`,
  )
}

/** POST /knowledge-bases/{knowledgeBaseId}/documents (multipart/form-data) */
export function uploadDocument(
  knowledgeBaseId: string,
  file: File,
  tags?: string[],
): Promise<DocumentSummary> {
  const formData = new FormData()
  formData.append('file', file)
  tags?.forEach((tag) => formData.append('tags', tag))

  return gatewayRequest<DocumentSummary>(
    `/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/documents`,
    { method: 'POST', body: formData },
  )
}

/** GET /documents/{documentId} */
export function getDocument(documentId: string, knowledgeBaseId: string): Promise<DocumentSummary> {
  return gatewayRequest<DocumentSummary>(
    `/documents/${encodeURIComponent(documentId)}${buildQuery({ knowledgeBaseId })}`,
  )
}

/** PATCH /documents/{documentId} */
export function updateDocument(
  documentId: string,
  knowledgeBaseId: string,
  params: UpdateDocumentRequest,
): Promise<DocumentSummary> {
  return gatewayRequest<DocumentSummary>(
    `/documents/${encodeURIComponent(documentId)}${buildQuery({ knowledgeBaseId })}`,
    {
      method: 'PATCH',
      body: params,
    },
  )
}

/** DELETE /documents/{documentId} */
export async function deleteDocument(documentId: string, knowledgeBaseId: string): Promise<void> {
  await requestVoid(
    `/documents/${encodeURIComponent(documentId)}${buildQuery({ knowledgeBaseId })}`,
    { method: 'DELETE' },
  )
}

/** GET /documents/{documentId}/chunks?page=&pageSize= */
export async function listChunks(
  documentId: string,
  knowledgeBaseId: string,
  params: QueryParams<'/api/v1/documents/{documentId}/chunks', 'get'> = {},
): Promise<KnowledgePageResult<DocumentChunk>> {
  return gatewayPageRequest<DocumentChunk>(
    `/documents/${encodeURIComponent(documentId)}/chunks${buildQuery({
      knowledgeBaseId,
      page: params.page,
      pageSize: params.pageSize,
    })}`,
  )
}

/** GET /documents/{documentId}/content */
export function getDocumentContent(documentId: string, knowledgeBaseId: string): Promise<Blob> {
  return gatewayFileRequest(
    `/documents/${encodeURIComponent(documentId)}/content${buildQuery({ knowledgeBaseId })}`,
  )
}

/** POST /knowledge-queries */
export function runKnowledgeQuery(params: KnowledgeQueryRequest): Promise<KnowledgeQuerySummary> {
  return gatewayRequest<KnowledgeQuerySummary>('/knowledge-queries', {
    method: 'POST',
    body: params,
  })
}
