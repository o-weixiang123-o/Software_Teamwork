import { describe, expect, it } from 'vitest'

import { ApiError } from '@/api/client'
import type { QACitation, QAMessageWithArtifacts } from '@/lib/types'

import {
  createSafeToolStep,
  formatQAError,
  formatQAStreamError,
  getCitationAvailabilityText,
  getCitationDelta,
  getSafeReasoningStep,
  getToolEventSummary,
  getToolReportArtifact,
  mergeMessageReportArtifact,
} from './capability'

describe('QA capability helpers', () => {
  it('formats readiness and dependency errors with request id state', () => {
    expect(
      formatQAError(
        new ApiError({
          code: 'not_implemented',
          message: 'route pending',
          requestId: 'req-501',
          status: 501,
        }),
        'RAG 检索',
      ),
    ).toContain('requestId: req-501')

    expect(
      formatQAStreamError({
        code: 'dependency_error',
        message: 'knowledge unavailable',
        status: 502,
      }),
    ).toContain('响应未包含 requestId')
  })

  it('formats forbidden errors as permission denials', () => {
    const formatted = formatQAError(
      new ApiError({
        code: 'forbidden',
        message: 'not allowed',
        requestId: 'req-403',
        status: 403,
      }),
      'QA 会话列表',
    )

    expect(formatted).toContain('权限不足')
    expect(formatted).toContain('requestId: req-403')
    expect(formatted).not.toContain('稍后重试')
  })

  it('does not expose backend raw error messages in user-visible text', () => {
    const formatted = formatQAStreamError({
      code: 'dependency_error',
      message: 'provider raw error includes http://10.0.0.2/minio/private-object',
      requestId: 'req-safe',
      status: 502,
    })

    expect(formatted).toContain('依赖服务暂不可用')
    expect(formatted).toContain('requestId: req-safe')
    expect(formatted).not.toContain('provider raw')
    expect(formatted).not.toContain('10.0.0.2')
    expect(formatted).not.toContain('private-object')
  })

  it('builds tool steps from sanitized summary fields without dumping raw payloads', () => {
    const view = createSafeToolStep('completed', {
      argumentsSummary: {
        bucket: 'qa-prod-files',
        internalPreview: 'http://10.0.0.5/minio/private/object',
        objectKey: 'secret/minio/key',
        prompt: 'full hidden prompt',
        queryCount: 3,
        sourcePath: 'project-a/private/doc.pdf',
      },
      latencyMs: 120,
      rawResult: 'provider raw response',
      resultSummary: { documentUri: 's3://qa-prod-files/private/doc.pdf', hitCount: 2 },
      toolCallId: 'tool-1',
      toolName: 'search_knowledge',
    })

    expect(view.toolCallId).toBe('tool-1')
    expect(view.step).toMatchObject({
      label: 'search_knowledge 完成',
      status: 'done',
      type: 'tool_call',
    })
    expect(view.step.detail).toContain('查询数: 3')
    expect(view.step.detail).not.toContain('queryCount')
    expect(view.step.detail).not.toContain('qa-prod-files')
    expect(view.step.detail).not.toContain('project-a/private')
    expect(view.step.detail).not.toContain('s3://')
    expect(view.step.detail).not.toContain('secret/minio/key')
    expect(view.step.detail).not.toContain('full hidden prompt')
    expect(view.step.detail).not.toContain('http://10.0.0.5')
    expect(view.step.detail).not.toContain('provider raw response')
  })

  it('does not display free-text tool summaries that may leak sensitive details', () => {
    const view = createSafeToolStep('failed', {
      errorCode: 'dependency_error',
      errorMessage: 'provider raw error body includes http://10.0.0.2/internal',
      latencyMs: 30,
      summary: 'prompt: hidden system prompt http://10.0.0.1/minio/bucket/object',
      toolSummary: 'safe-looking but unstructured text from backend',
      toolName: 'search_knowledge',
    })

    expect(view.step.detail).toContain('dependency_error')
    expect(view.step.detail).toContain('30ms')
    expect(view.step.detail).not.toContain('hidden system prompt')
    expect(view.step.detail).not.toContain('10.0.0.1')
    expect(view.step.detail).not.toContain('10.0.0.2')
    expect(view.step.detail).not.toContain('safe-looking but unstructured')
    expect(view.step.detail).not.toContain('provider raw error body')
  })

  it('accepts current backend tool and flat reasoning event fields', () => {
    const toolView = createSafeToolStep('started', {
      argumentsSummary: { queryCount: 2 },
      tool: 'search_knowledge',
      toolCallId: 'tool-2',
    })

    expect(toolView.toolCallId).toBe('tool-2')
    expect(toolView.step).toMatchObject({
      label: expect.stringContaining('search_knowledge'),
      status: 'running',
      type: 'tool_call',
    })
    expect(toolView.step.detail).toContain('查询数: 2')

    expect(
      getSafeReasoningStep({
        detail: 'using retrieved citation snapshots',
        label: '整理引用',
        status: 'done',
        type: 'citation',
      }),
    ).toMatchObject({
      detail: 'using retrieved citation snapshots',
      label: '整理引用',
      status: 'done',
      type: 'citation',
    })
  })

  it('maps only sanitized SSE flat tool summaries for display', () => {
    const event = {
      arguments: { query: 'legacy query', topK: 3 },
      argumentsSummary: { queryCount: 2, topK: 5 },
      result: { hitCount: 1 },
      resultSummary: { hitCount: 4 },
    }

    expect(getToolEventSummary(event, 'argumentsSummary')).toEqual({
      queryCount: 2,
      topK: 5,
    })
    expect(getToolEventSummary(event, 'resultSummary')).toEqual({ hitCount: 4 })
    expect(getToolEventSummary({ arguments: { topK: 3 } }, 'argumentsSummary')).toBeUndefined()
    expect(getToolEventSummary({ result: { hitCount: 3 } }, 'resultSummary')).toBeUndefined()
    expect(
      getToolEventSummary({ resultSummary: 'safe-looking free text' }, 'resultSummary'),
    ).toBeUndefined()
  })

  it('extracts report artifacts from resultSummary and keeps message-level artifacts current', () => {
    const artifact = getToolReportArtifact({
      resultSummary: {
        hitCount: 1,
        reportArtifact: {
          artifactType: 'report_generation',
          downloadPath: '/api/v1/report-files/file-1/content',
          fileStatus: 'succeeded',
          filename: '巡检报告.docx',
          reportId: 'report-1',
          reportName: '巡检报告',
        },
      },
    })

    expect(artifact).toMatchObject({
      artifactType: 'report_generation',
      downloadPath: '/api/v1/report-files/file-1/content',
      reportId: 'report-1',
      reportName: '巡检报告',
    })

    const message: QAMessageWithArtifacts = {
      artifacts: [],
      content: '',
      createdAt: '2026-07-03T00:00:00.000Z',
      id: 'msg-1',
      role: 'assistant',
      sessionId: 'session-1',
      status: 'streaming',
    }

    const firstMerge = mergeMessageReportArtifact(message, artifact)
    expect(firstMerge).toHaveLength(1)

    const updated = mergeMessageReportArtifact(
      { ...message, artifacts: firstMerge },
      artifact ? { ...artifact, fileStatus: 'failed' } : undefined,
    )
    expect(updated).toHaveLength(1)
    expect(updated?.[0]?.fileStatus).toBe('failed')
  })

  it('updates temporary report artifacts when stable report ids arrive later', () => {
    const message: QAMessageWithArtifacts = {
      artifacts: [
        {
          artifactType: 'report_generation',
          jobId: 'job-1',
          jobStatus: 'running',
          reportName: '巡检报告',
        },
      ],
      content: '',
      createdAt: '2026-07-03T00:00:00.000Z',
      id: 'msg-1',
      role: 'assistant',
      sessionId: 'session-1',
      status: 'streaming',
    }

    const merged = mergeMessageReportArtifact(message, {
      artifactType: 'report_generation',
      downloadPath: '/api/v1/report-files/file-1/content',
      fileStatus: 'succeeded',
      jobId: 'job-1',
      jobStatus: 'succeeded',
      reportFileId: 'file-1',
      reportId: 'report-1',
      reportName: '巡检报告',
    })

    expect(merged).toHaveLength(1)
    expect(merged?.[0]).toMatchObject({
      jobId: 'job-1',
      reportFileId: 'file-1',
      reportId: 'report-1',
    })
  })

  it('sanitizes reasoning label and detail before display', () => {
    expect(
      getSafeReasoningStep({
        detail: 'provider raw error includes http://10.0.0.2/internal',
        label: 'system prompt: hidden chain',
        status: 'done',
        type: 'generation',
      }),
    ).toMatchObject({
      detail: undefined,
      label: 'generation',
      status: 'done',
      type: 'generation',
    })
  })

  it('accepts only structured reasoning and citation payloads', () => {
    expect(
      getSafeReasoningStep({
        step: {
          detail: '已生成脱敏摘要',
          label: '检索摘要',
          status: 'done',
          type: 'citation',
        },
      }),
    ).toMatchObject({ detail: '已生成脱敏摘要', status: 'done', type: 'citation' })

    expect(
      getSafeReasoningStep({ step: { status: 'done', type: 'private_chain_of_thought' } }),
    ).toBeUndefined()

    const citation = getCitationDelta({
      citation: {
        id: 'cit-1',
        citationNo: 1,
        docId: 'DOC-001',
        docName: '电力变压器巡检手册.pdf',
        isSourceAvailable: false,
        score: 0.96,
        text: '变压器外壳应保持清洁...',
      },
    })

    expect(citation).toMatchObject({
      citationNo: 1,
      docId: 'DOC-001',
      id: 'cit-1',
      text: '变压器外壳应保持清洁...',
    })
    expect(getCitationDelta({ citation: { messageId: 'msg-1' } })).toBeUndefined()
  })

  it('keeps citation detail readiness explicit', () => {
    const citation: QACitation = {
      id: 'cit-1',
      isSourceAvailable: false,
      messageId: 'msg-1',
    }

    expect(getCitationAvailabilityText(citation)).toContain('仅展示 QA 保存的引用快照')
  })
})
