# Gateway Active API Owner Map

This document is the human-readable audit list for gateway active API paths. The machine-readable source of truth remains [`../api/public.openapi.yaml`](../api/public.openapi.yaml); if this document and OpenAPI disagree, update this document from OpenAPI.

## Audit Result

- Active operations: `112`.
- Every `/api/v1/**` active operation has `operationId`, `tags`, `x-owner-service`, security, at least one `2XX` response, and at least one `4XX` response.
- `/healthz` and `/readyz` are operational routes owned by `gateway`; they intentionally do not use bearer auth.
- No stable active path uses action-style segments such as `/login`, `/logout`, `/search`, `/generate`, `/export`, `/retry`, or `/revoke`.
- Frontend clients should generate callable methods only from active OpenAPI `paths`, not from `x-missing-contracts`.
- Active means the public method/path/schema is part of the collaboration contract.
  A small number of owner-service operations may still return the stable
  `not_implemented` error while implementation catches up; check
  `docs/services/gateway/docs/implementation.md` before treating an active path
  as end-to-end smoke-ready.

## Owner Summary

| Owner service | Active operations | Notes |
| --- | ---: | --- |
| `gateway` | 5 | Gateway health/readiness, app version freshness, public routing, admin overview and metrics aggregation. |
| `auth` | 11 | Users, sessions, current-user identity, current-user profile, required password change, admin user management, roles, and permissions. |
| `knowledge` | 18 | Knowledge bases, knowledge documents, chunks, retrieval, and parser runtime config. |
| `ai-gateway` | 5 | Model profile runtime configuration exposed through gateway admin paths. |
| `document` | 44 | Report templates, materials, records, outlines, sections, jobs, files, settings, statistics, and logs. |
| `qa` | 29 | QA sessions, messages, attachments, runs, citations, configuration, retrieval tests, and QA metrics. |

## Missing Contracts

No missing contracts remain. All previously placeholder admin overview and metrics
aggregation endpoints are now active gateway contracts listed below.

## Active Operations

| Method | Path | Owner service | Tag | Operation ID | Auth |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/healthz` | `gateway` | `health` | `getHealthz` | `none` |
| `GET` | `/readyz` | `gateway` | `health` | `getReadyz` | `none` |
| `GET` | `/api/v1/app-version/freshness` | `gateway` | `app-version` | `getAppVersionFreshness` | `none` |
| `POST` | `/api/v1/users` | `auth` | `auth` | `createUser` | `none` |
| `POST` | `/api/v1/sessions` | `auth` | `auth` | `createSession` | `none` |
| `DELETE` | `/api/v1/sessions/current` | `auth` | `auth` | `deleteCurrentSession` | `bearerAuth` |
| `GET` | `/api/v1/users/me` | `auth` | `auth` | `getCurrentUser` | `bearerAuth` |
| `GET` | `/api/v1/users/me/profile` | `auth` | `auth` | `getCurrentUserProfile` | `bearerAuth` |
| `PATCH` | `/api/v1/users/me/profile` | `auth` | `auth` | `updateCurrentUserProfile` | `bearerAuth` |
| `POST` | `/api/v1/users/me/password-changes` | `auth` | `auth` | `createCurrentUserPasswordChange` | `bearerAuth` |
| `GET` | `/api/v1/knowledge-bases` | `knowledge` | `knowledge` | `listKnowledgeBases` | `bearerAuth` |
| `POST` | `/api/v1/knowledge-bases` | `knowledge` | `knowledge` | `createKnowledgeBase` | `bearerAuth` |
| `GET` | `/api/v1/knowledge-bases/{knowledgeBaseId}` | `knowledge` | `knowledge` | `getKnowledgeBase` | `bearerAuth` |
| `PATCH` | `/api/v1/knowledge-bases/{knowledgeBaseId}` | `knowledge` | `knowledge` | `updateKnowledgeBase` | `bearerAuth` |
| `DELETE` | `/api/v1/knowledge-bases/{knowledgeBaseId}` | `knowledge` | `knowledge` | `deleteKnowledgeBase` | `bearerAuth` |
| `GET` | `/api/v1/knowledge-bases/{knowledgeBaseId}/documents` | `knowledge` | `knowledge` | `listKnowledgeBaseDocuments` | `bearerAuth` |
| `POST` | `/api/v1/knowledge-bases/{knowledgeBaseId}/documents` | `knowledge` | `documents` | `uploadKnowledgeBaseDocument` | `bearerAuth` |
| `GET` | `/api/v1/documents/{documentId}` | `knowledge` | `knowledge` | `getDocument` | `bearerAuth` |
| `PATCH` | `/api/v1/documents/{documentId}` | `knowledge` | `documents` | `updateDocument` | `bearerAuth` |
| `DELETE` | `/api/v1/documents/{documentId}` | `knowledge` | `documents` | `deleteDocument` | `bearerAuth` |
| `GET` | `/api/v1/documents/{documentId}/chunks` | `knowledge` | `knowledge` | `listDocumentChunks` | `bearerAuth` |
| `GET` | `/api/v1/documents/{documentId}/content` | `knowledge` | `documents` | `getDocumentContent` | `bearerAuth` |
| `POST` | `/api/v1/knowledge-queries` | `knowledge` | `knowledge` | `createKnowledgeQuery` | `bearerAuth` |
| `GET` | `/api/v1/admin/users` | `auth` | `admin-users` | `listAdminUsers` | `bearerAuth` |
| `POST` | `/api/v1/admin/users` | `auth` | `admin-users` | `createAdminUser` | `bearerAuth` |
| `PATCH` | `/api/v1/admin/users/{userId}` | `auth` | `admin-users` | `updateAdminUser` | `bearerAuth` |
| `POST` | `/api/v1/admin/users/{userId}/password-resets` | `auth` | `admin-users` | `createAdminUserPasswordReset` | `bearerAuth` |
| `GET` | `/api/v1/admin/model-profiles` | `ai-gateway` | `admin-runtime-config` | `listAdminModelProfiles` | `bearerAuth` |
| `POST` | `/api/v1/admin/model-profiles` | `ai-gateway` | `admin-runtime-config` | `createAdminModelProfile` | `bearerAuth` |
| `GET` | `/api/v1/admin/model-profiles/{profileId}` | `ai-gateway` | `admin-runtime-config` | `getAdminModelProfile` | `bearerAuth` |
| `PATCH` | `/api/v1/admin/model-profiles/{profileId}` | `ai-gateway` | `admin-runtime-config` | `updateAdminModelProfile` | `bearerAuth` |
| `DELETE` | `/api/v1/admin/model-profiles/{profileId}` | `ai-gateway` | `admin-runtime-config` | `deleteAdminModelProfile` | `bearerAuth` |
| `GET` | `/api/v1/admin/parser-configs` | `knowledge` | `admin-runtime-config` | `listAdminParserConfigs` | `bearerAuth` |
| `POST` | `/api/v1/admin/parser-configs` | `knowledge` | `admin-runtime-config` | `createAdminParserConfig` | `bearerAuth` |
| `GET` | `/api/v1/admin/parser-configs/{parserConfigId}` | `knowledge` | `admin-runtime-config` | `getAdminParserConfig` | `bearerAuth` |
| `PATCH` | `/api/v1/admin/parser-configs/{parserConfigId}` | `knowledge` | `admin-runtime-config` | `updateAdminParserConfig` | `bearerAuth` |
| `DELETE` | `/api/v1/admin/parser-configs/{parserConfigId}` | `knowledge` | `admin-runtime-config` | `deleteAdminParserConfig` | `bearerAuth` |
| `GET` | `/api/v1/report-types` | `document` | `report-generation` | `listReportTypes` | `bearerAuth` |
| `GET` | `/api/v1/report-templates` | `document` | `report-generation` | `listReportTemplates` | `bearerAuth` |
| `POST` | `/api/v1/report-templates` | `document` | `report-generation` | `createReportTemplate` | `bearerAuth` |
| `GET` | `/api/v1/report-templates/{reportTemplateId}` | `document` | `report-generation` | `getReportTemplate` | `bearerAuth` |
| `PATCH` | `/api/v1/report-templates/{reportTemplateId}` | `document` | `report-generation` | `updateReportTemplate` | `bearerAuth` |
| `DELETE` | `/api/v1/report-templates/{reportTemplateId}` | `document` | `report-generation` | `deleteReportTemplate` | `bearerAuth` |
| `GET` | `/api/v1/report-templates/{reportTemplateId}/structure` | `document` | `report-generation` | `getReportTemplateStructure` | `bearerAuth` |
| `PATCH` | `/api/v1/report-templates/{reportTemplateId}/structure` | `document` | `report-generation` | `updateReportTemplateStructure` | `bearerAuth` |
| `GET` | `/api/v1/report-materials` | `document` | `report-generation` | `listReportMaterials` | `bearerAuth` |
| `POST` | `/api/v1/report-materials` | `document` | `report-generation` | `createReportMaterial` | `bearerAuth` |
| `GET` | `/api/v1/report-materials/{materialId}` | `document` | `report-generation` | `getReportMaterial` | `bearerAuth` |
| `DELETE` | `/api/v1/report-materials/{materialId}` | `document` | `report-generation` | `deleteReportMaterial` | `bearerAuth` |
| `GET` | `/api/v1/reports` | `document` | `report-generation` | `listReports` | `bearerAuth` |
| `POST` | `/api/v1/reports` | `document` | `report-generation` | `createReport` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}` | `document` | `report-generation` | `getReport` | `bearerAuth` |
| `PATCH` | `/api/v1/reports/{reportId}` | `document` | `report-generation` | `updateReport` | `bearerAuth` |
| `DELETE` | `/api/v1/reports/{reportId}` | `document` | `report-generation` | `deleteReport` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/outlines` | `document` | `report-generation` | `listReportOutlines` | `bearerAuth` |
| `POST` | `/api/v1/reports/{reportId}/outlines` | `document` | `report-generation` | `createReportOutline` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/outlines/{outlineId}` | `document` | `report-generation` | `getReportOutline` | `bearerAuth` |
| `PATCH` | `/api/v1/reports/{reportId}/outlines/{outlineId}` | `document` | `report-generation` | `updateReportOutline` | `bearerAuth` |
| `DELETE` | `/api/v1/reports/{reportId}/outlines/{outlineId}/sections/{sectionId}` | `document` | `report-generation` | `deleteReportOutlineSection` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/sections` | `document` | `report-generation` | `listReportSections` | `bearerAuth` |
| `POST` | `/api/v1/reports/{reportId}/sections` | `document` | `report-generation` | `createReportSection` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/sections/{sectionId}` | `document` | `report-generation` | `getReportSection` | `bearerAuth` |
| `PATCH` | `/api/v1/reports/{reportId}/sections/{sectionId}` | `document` | `report-generation` | `updateReportSection` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/sections/{sectionId}/versions` | `document` | `report-generation` | `listReportSectionVersions` | `bearerAuth` |
| `POST` | `/api/v1/reports/{reportId}/sections/{sectionId}/versions` | `document` | `report-generation` | `createReportSectionVersion` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/jobs` | `document` | `report-generation` | `listReportJobs` | `bearerAuth` |
| `POST` | `/api/v1/reports/{reportId}/jobs` | `document` | `report-generation` | `createReportJob` | `bearerAuth` |
| `GET` | `/api/v1/report-jobs/{jobId}` | `document` | `report-generation` | `getReportJob` | `bearerAuth` |
| `PATCH` | `/api/v1/report-jobs/{jobId}` | `document` | `report-generation` | `updateReportJob` | `bearerAuth` |
| `GET` | `/api/v1/report-jobs/{jobId}/attempts` | `document` | `report-generation` | `listReportJobAttempts` | `bearerAuth` |
| `POST` | `/api/v1/report-jobs/{jobId}/attempts` | `document` | `report-generation` | `createReportJobAttempt` | `bearerAuth` |
| `GET` | `/api/v1/reports/{reportId}/events` | `document` | `report-generation` | `listReportEvents` | `bearerAuth` |
| `GET` | `/api/v1/report-files` | `document` | `report-generation` | `listReportFiles` | `bearerAuth` |
| `POST` | `/api/v1/report-files` | `document` | `report-generation` | `createReportFile` | `bearerAuth` |
| `GET` | `/api/v1/report-files/{reportFileId}` | `document` | `report-generation` | `getReportFile` | `bearerAuth` |
| `GET` | `/api/v1/report-files/{reportFileId}/content` | `document` | `report-generation` | `getReportFileContent` | `bearerAuth` |
| `GET` | `/api/v1/report-statistics/overview` | `document` | `report-generation` | `getReportStatisticsOverview` | `bearerAuth` |
| `GET` | `/api/v1/report-statistics/daily` | `document` | `report-generation` | `listDailyReportStatistics` | `bearerAuth` |
| `GET` | `/api/v1/report-operation-logs` | `document` | `report-generation` | `listReportOperationLogs` | `bearerAuth` |
| `GET` | `/api/v1/report-settings` | `document` | `report-generation` | `getReportSettings` | `bearerAuth` |
| `PATCH` | `/api/v1/report-settings` | `document` | `report-generation` | `updateReportSettings` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions` | `qa` | `qa-sessions` | `listQASessions` | `bearerAuth` |
| `POST` | `/api/v1/qa-sessions` | `qa` | `qa-sessions` | `createQASession` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions/{sessionId}` | `qa` | `qa-sessions` | `getQASession` | `bearerAuth` |
| `PATCH` | `/api/v1/qa-sessions/{sessionId}` | `qa` | `qa-sessions` | `updateQASession` | `bearerAuth` |
| `DELETE` | `/api/v1/qa-sessions/{sessionId}` | `qa` | `qa-sessions` | `deleteQASession` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions/{sessionId}/messages` | `qa` | `qa-messages` | `listQAMessages` | `bearerAuth` |
| `POST` | `/api/v1/qa-sessions/{sessionId}/messages` | `qa` | `qa-messages` | `createQAMessage` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions/{sessionId}/attachments` | `qa` | `qa-session-attachments` | `listQASessionAttachments` | `bearerAuth` |
| `POST` | `/api/v1/qa-sessions/{sessionId}/attachments` | `qa` | `qa-session-attachments` | `uploadQASessionAttachment` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions/{sessionId}/attachments/{attachmentId}` | `qa` | `qa-session-attachments` | `getQASessionAttachment` | `bearerAuth` |
| `DELETE` | `/api/v1/qa-sessions/{sessionId}/attachments/{attachmentId}` | `qa` | `qa-session-attachments` | `deleteQASessionAttachment` | `bearerAuth` |
| `GET` | `/api/v1/qa-sessions/{sessionId}/events` | `qa` | `qa-messages` | `listQAStreamEvents` | `bearerAuth` |
| `GET` | `/api/v1/response-runs/{responseRunId}` | `qa` | `qa-agent-runs` | `getQAResponseRun` | `bearerAuth` |
| `PATCH` | `/api/v1/response-runs/{responseRunId}` | `qa` | `qa-agent-runs` | `updateQAResponseRun` | `bearerAuth` |
| `GET` | `/api/v1/response-runs/{responseRunId}/tool-calls` | `qa` | `qa-agent-runs` | `listQAResponseRunToolCalls` | `bearerAuth` |
| `GET` | `/api/v1/messages/{messageId}/citations` | `qa` | `qa-citations` | `listQAMessageCitations` | `bearerAuth` |
| `GET` | `/api/v1/citations/{citationId}` | `qa` | `qa-citations` | `getQACitation` | `bearerAuth` |
| `POST` | `/api/v1/citation-lookups` | `qa` | `qa-citations` | `createQACitationLookup` | `bearerAuth` |
| `GET` | `/api/v1/qa-config-versions/current` | `qa` | `qa-settings` | `getCurrentQAConfigVersion` | `bearerAuth` |
| `POST` | `/api/v1/qa-config-versions` | `qa` | `qa-settings` | `createQAConfigVersion` | `bearerAuth` |
| `GET` | `/api/v1/llm-config-versions/current` | `qa` | `qa-settings` | `getCurrentQALLMConfigVersion` | `bearerAuth` |
| `POST` | `/api/v1/llm-config-versions` | `qa` | `qa-settings` | `createQALLMConfigVersion` | `bearerAuth` |
| `POST` | `/api/v1/llm-connection-tests` | `qa` | `qa-settings` | `createQALLMConnectionTest` | `bearerAuth` |
| `POST` | `/api/v1/retrieval-test-runs` | `qa` | `qa-retrieval-tests` | `createQARetrievalTestRun` | `bearerAuth` |
| `GET` | `/api/v1/retrieval-test-runs/{testRunId}` | `qa` | `qa-retrieval-tests` | `getQARetrievalTestRun` | `bearerAuth` |
| `GET` | `/api/v1/qa-metrics/overview` | `qa` | `qa-metrics` | `getQAMetricsOverview` | `bearerAuth` |
| `GET` | `/api/v1/qa-metrics/trend` | `qa` | `qa-metrics` | `getQAMetricsTrend` | `bearerAuth` |
| `GET` | `/api/v1/qa-metrics/top-queries` | `qa` | `qa-metrics` | `listQATopQueries` | `bearerAuth` |
| `GET` | `/api/v1/qa-metrics/intent-distribution` | `qa` | `qa-metrics` | `listQAIntentDistribution` | `bearerAuth` |
| `GET` | `/api/v1/admin/overview` | `gateway` | `admin` | `getAdminOverview` | `bearerAuth` |
| `GET` | `/api/v1/admin/metrics` | `gateway` | `admin` | `getAdminMetrics` | `bearerAuth` |
