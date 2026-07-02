package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	DocumentMCPToolGenerateReportOutline   = "generate_report_outline"
	DocumentMCPToolRegenerateReportOutline = "regenerate_report_outline"
	DocumentMCPToolGenerateReportText      = "generate_report_text"
	DocumentMCPToolRegenerateReportText    = "regenerate_report_text"
	DocumentMCPToolRegenerateReportSection = "regenerate_report_section"
	DocumentMCPToolGetGenerationStatus     = "get_generation_status"
	DocumentMCPToolGetTemplateSchema       = "get_template_schema"
	DocumentMCPToolExportReportDOCX        = "export_report_docx"
	DocumentMCPToolGetReportResult         = "get_report_result"
	OperationDocumentMCPToolCall           = "document_mcp_tool_call"
	documentMCPRequestSource               = "mcp"
	documentMCPToolResultSucceeded         = "succeeded"
	documentMCPToolResultFailed            = "failed"
	documentMCPToolResultAccepted          = "accepted"
	documentMCPDefaultExportFormat         = ReportFileFormatDOCX
	documentMCPErrorUnsupported            = "unsupported"
)

// MCPDocumentService is the subset of the Document metadata service used by
// Document MCP tools. Keeping it narrow prevents the tool layer from reaching
// through to repositories or storage internals.
type MCPDocumentService interface {
	GetReportTemplateStructure(context.Context, RequestContext, string) (ReportTemplateStructure, error)
}

type MCPJobService interface {
	CreateJob(context.Context, RequestContext, CreateJobInput) (ReportJob, error)
	GetJob(context.Context, RequestContext, string) (ReportJob, error)
}

type MCPReportService interface {
	GetReport(context.Context, RequestContext, string) (Report, error)
}

type MCPReportFileService interface {
	CreateReportFile(context.Context, RequestContext, CreateReportFileInput) (ReportFile, error)
	GetReportFile(context.Context, RequestContext, string) (ReportFile, error)
}

type MCPToolService struct {
	documents   MCPDocumentService
	jobs        MCPJobService
	reports     MCPReportService
	reportFiles MCPReportFileService
	recorder    OperationLogRecorder
	now         func() time.Time
}

type MCPToolServiceConfig struct {
	DocumentService MCPDocumentService
	JobService      MCPJobService
	ReportService   MCPReportService
	ReportFileSvc   MCPReportFileService
	Recorder        OperationLogRecorder
}

func NewMCPToolService(cfg MCPToolServiceConfig) *MCPToolService {
	return &MCPToolService{
		documents:   cfg.DocumentService,
		jobs:        cfg.JobService,
		reports:     cfg.ReportService,
		reportFiles: cfg.ReportFileSvc,
		recorder:    cfg.Recorder,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

type MCPToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type MCPToolCallResult struct {
	RequestID      string                    `json:"requestId"`
	ToolName       string                    `json:"toolName"`
	Status         string                    `json:"status"`
	Job            *MCPReportJobSummary      `json:"job,omitempty"`
	Report         *MCPReportSummary         `json:"report,omitempty"`
	ReportFile     *MCPReportFileSummary     `json:"reportFile,omitempty"`
	TemplateSchema *MCPTemplateSchemaSummary `json:"templateSchema,omitempty"`
	Error          *MCPToolError             `json:"error,omitempty"`
	Warnings       []string                  `json:"warnings,omitempty"`
}

func (r MCPToolCallResult) IsFailed() bool {
	return r.Status == documentMCPToolResultFailed
}

func (r MCPToolCallResult) IsAccepted() bool {
	return r.Status == documentMCPToolResultAccepted
}

func (r MCPToolCallResult) IsSucceeded() bool {
	return r.Status == documentMCPToolResultSucceeded
}

type MCPToolError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type MCPReportJobSummary struct {
	ID         string         `json:"id"`
	ReportID   string         `json:"reportId"`
	JobType    string         `json:"jobType"`
	TargetType string         `json:"targetType,omitempty"`
	TargetID   string         `json:"targetId,omitempty"`
	Status     string         `json:"status"`
	Progress   map[string]any `json:"progress,omitempty"`
	ErrorCode  string         `json:"errorCode,omitempty"`
	CreatedAt  string         `json:"createdAt,omitempty"`
}

type MCPReportSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	ReportType         string `json:"reportType,omitempty"`
	TemplateID         string `json:"templateId,omitempty"`
	Status             string `json:"status"`
	LatestJobID        string `json:"latestJobId,omitempty"`
	LatestReportFileID string `json:"latestReportFileId,omitempty"`
	GeneratedAt        string `json:"generatedAt,omitempty"`
	ExportedAt         string `json:"exportedAt,omitempty"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
}

type MCPReportFileSummary struct {
	ID          string `json:"id"`
	ReportID    string `json:"reportId"`
	JobID       string `json:"jobId,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Format      string `json:"format"`
	FileSize    int64  `json:"fileSize,omitempty"`
	Status      string `json:"status"`
	ContentPath string `json:"contentPath,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type MCPTemplateSchemaSummary struct {
	TemplateID    string          `json:"templateId"`
	OutlineSchema json.RawMessage `json:"outlineSchema"`
	StyleConfig   json.RawMessage `json:"styleConfig"`
}

func (s *MCPToolService) ListTools(context.Context) []MCPToolDefinition {
	return []MCPToolDefinition{
		toolDefinition(DocumentMCPToolGenerateReportOutline, "Create an outline generation report job.", jobToolSchema(false)),
		toolDefinition(DocumentMCPToolRegenerateReportOutline, "Create an outline regeneration report job.", jobToolSchema(false)),
		toolDefinition(DocumentMCPToolGenerateReportText, "Create a report content generation job.", jobToolSchema(false)),
		toolDefinition(DocumentMCPToolRegenerateReportText, "Create a report content regeneration job.", jobToolSchema(false)),
		toolDefinition(DocumentMCPToolRegenerateReportSection, "Create a section regeneration job.", jobToolSchema(true)),
		toolDefinition(DocumentMCPToolGetGenerationStatus, "Read a report generation job status.", requiredStringSchema("jobId", "Report job ID.")),
		toolDefinition(DocumentMCPToolGetTemplateSchema, "Read a report template structure schema.", requiredStringSchema("templateId", "Report template ID.")),
		toolDefinition(DocumentMCPToolExportReportDOCX, "Create a basic DOCX report export job.", exportDOCXSchema()),
		toolDefinition(DocumentMCPToolGetReportResult, "Read a safe report result summary.", requiredStringSchema("reportId", "Report ID.")),
	}
}

func (s *MCPToolService) CallTool(ctx context.Context, reqCtx RequestContext, name string, arguments json.RawMessage) MCPToolCallResult {
	reqCtx = normalizeMCPRequestContext(reqCtx)
	result := MCPToolCallResult{
		RequestID: reqCtx.RequestID,
		ToolName:  strings.TrimSpace(name),
	}
	args, err := decodeMCPArguments(arguments)
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"argumentShape": "invalid"})
		return result
	}

	switch result.ToolName {
	case DocumentMCPToolGenerateReportOutline:
		result = s.createGenerationJob(ctx, reqCtx, result.ToolName, JobTypeOutlineGeneration, args, false)
	case DocumentMCPToolRegenerateReportOutline:
		result = s.createGenerationJob(ctx, reqCtx, result.ToolName, JobTypeOutlineRegeneration, args, false)
	case DocumentMCPToolGenerateReportText:
		result = s.createGenerationJob(ctx, reqCtx, result.ToolName, JobTypeContentGeneration, args, false)
	case DocumentMCPToolRegenerateReportText:
		result = s.createGenerationJob(ctx, reqCtx, result.ToolName, JobTypeContentRegeneration, args, false)
	case DocumentMCPToolRegenerateReportSection:
		result = s.createGenerationJob(ctx, reqCtx, result.ToolName, JobTypeSectionRegeneration, args, true)
	case DocumentMCPToolGetGenerationStatus:
		result = s.getGenerationStatus(ctx, reqCtx, args)
	case DocumentMCPToolGetTemplateSchema:
		result = s.getTemplateSchema(ctx, reqCtx, args)
	case DocumentMCPToolExportReportDOCX:
		result = s.exportReportDOCX(ctx, reqCtx, args)
	case DocumentMCPToolGetReportResult:
		result = s.getReportResult(ctx, reqCtx, args)
	default:
		result.Status = documentMCPToolResultFailed
		result.Error = &MCPToolError{Code: documentMCPErrorUnsupported, Message: "document MCP tool is not supported"}
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"toolSupported": false})
	}
	if result.RequestID == "" {
		result.RequestID = reqCtx.RequestID
	}
	if result.ToolName == "" {
		result.ToolName = name
	}
	return result
}

func (s *MCPToolService) createGenerationJob(ctx context.Context, reqCtx RequestContext, toolName string, jobType JobType, args map[string]any, requiresSection bool) MCPToolCallResult {
	result := MCPToolCallResult{RequestID: reqCtx.RequestID, ToolName: toolName}
	if s.jobs == nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(NewError(CodeDependency, "report job service is not configured", nil))
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	reportID := stringArgument(args, "reportId", "report_id")
	if reportID == "" {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"reportId": "is required"}))
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	sectionID := ""
	targetScope := ""
	if requiresSection {
		sectionID = stringArgument(args, "sectionId", "section_id")
		if sectionID == "" {
			result.Status = documentMCPToolResultFailed
			result.Error = toolErrorFromError(ValidationError(map[string]string{"sectionId": "is required"}))
			s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, true))
			return result
		}
		targetScope = "section"
	}
	materialIDs, err := stringSliceArgumentStrict(args, "materialIds", "material_ids", "materialRefs", "material_refs")
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"materialIds": "must be an array of strings"}))
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	options, err := mapArgumentStrict(args, "options")
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"options": "must be a JSON object"}))
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	retrieval, err := mapArgumentStrict(args, "retrieval")
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"retrieval": "must be a JSON object"}))
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	job, err := s.jobs.CreateJob(ctx, reqCtx, CreateJobInput{
		RequestID:    reqCtx.RequestID,
		UserID:       reqCtx.UserID,
		ReportID:     reportID,
		JobType:      jobType,
		TargetScope:  targetScope,
		SectionID:    sectionID,
		Requirements: stringArgument(args, "requirements"),
		MaterialIDs:  materialIDs,
		Options:      options,
		Retrieval:    retrieval,
	})
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
		return result
	}
	result.Status = documentMCPToolResultAccepted
	result.Job = reportJobSummary(job)
	s.recordToolCall(ctx, reqCtx, result, generationParameterSummary(args, jobType, requiresSection))
	return result
}

func (s *MCPToolService) getGenerationStatus(ctx context.Context, reqCtx RequestContext, args map[string]any) MCPToolCallResult {
	result := MCPToolCallResult{RequestID: reqCtx.RequestID, ToolName: DocumentMCPToolGetGenerationStatus}
	if s.jobs == nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(NewError(CodeDependency, "report job service is not configured", nil))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"jobIdProvided": stringArgument(args, "jobId", "job_id") != ""})
		return result
	}
	jobID := stringArgument(args, "jobId", "job_id")
	if jobID == "" {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"jobId": "is required"}))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"jobIdProvided": false})
		return result
	}
	job, err := s.jobs.GetJob(ctx, reqCtx, jobID)
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"jobId": jobID})
		return result
	}
	result.Status = documentMCPToolResultSucceeded
	result.Job = reportJobSummary(job)
	s.recordToolCall(ctx, reqCtx, result, map[string]any{"jobId": job.ID})
	return result
}

func (s *MCPToolService) getTemplateSchema(ctx context.Context, reqCtx RequestContext, args map[string]any) MCPToolCallResult {
	result := MCPToolCallResult{RequestID: reqCtx.RequestID, ToolName: DocumentMCPToolGetTemplateSchema}
	if s.documents == nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(NewError(CodeDependency, "document service is not configured", nil))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"templateIdProvided": stringArgument(args, "templateId", "template_id") != ""})
		return result
	}
	templateID := stringArgument(args, "templateId", "template_id", "reportTemplateId", "report_template_id")
	if templateID == "" {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"templateId": "is required"}))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"templateIdProvided": false})
		return result
	}
	structure, err := s.documents.GetReportTemplateStructure(ctx, reqCtx, templateID)
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"templateId": templateID})
		return result
	}
	result.Status = documentMCPToolResultSucceeded
	result.TemplateSchema = &MCPTemplateSchemaSummary{
		TemplateID:    templateID,
		OutlineSchema: emptyJSONIfNil(structure.OutlineSchema),
		StyleConfig:   emptyJSONIfNil(structure.StyleConfig),
	}
	s.recordToolCall(ctx, reqCtx, result, map[string]any{"templateId": templateID})
	return result
}

func (s *MCPToolService) exportReportDOCX(ctx context.Context, reqCtx RequestContext, args map[string]any) MCPToolCallResult {
	result := MCPToolCallResult{RequestID: reqCtx.RequestID, ToolName: DocumentMCPToolExportReportDOCX}
	if s.reportFiles == nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(NewError(CodeDependency, "report file service is not configured", nil))
		s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
		return result
	}
	reportID := stringArgument(args, "reportId", "report_id")
	if reportID == "" {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"reportId": "is required"}))
		s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
		return result
	}
	format := strings.ToLower(stringArgument(args, "format"))
	if format == "" {
		format = documentMCPDefaultExportFormat
	}
	if format != ReportFileFormatDOCX {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"format": "must be docx"}))
		s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
		return result
	}
	styleOptions, err := rawJSONArgument(args, "exportOptions", "export_options", "styleOptions", "style_options")
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"exportOptions": "must be a JSON object"}))
		s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
		return result
	}
	reportFile, err := s.reportFiles.CreateReportFile(ctx, reqCtx, CreateReportFileInput{
		ReportID:     reportID,
		Format:       ReportFileFormatDOCX,
		TemplateID:   stringArgument(args, "templateId", "template_id"),
		StyleOptions: styleOptions,
	})
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
		return result
	}
	result.Status = documentMCPToolResultAccepted
	result.ReportFile = reportFileSummary(reportFile)
	if result.ReportFile != nil {
		result.Job = &MCPReportJobSummary{ID: result.ReportFile.JobID, ReportID: result.ReportFile.ReportID, JobType: string(JobTypeReportFileCreation), Status: string(reportFile.Status)}
	}
	s.recordToolCall(ctx, reqCtx, result, exportParameterSummary(args))
	return result
}

func (s *MCPToolService) getReportResult(ctx context.Context, reqCtx RequestContext, args map[string]any) MCPToolCallResult {
	result := MCPToolCallResult{RequestID: reqCtx.RequestID, ToolName: DocumentMCPToolGetReportResult}
	if s.reports == nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(NewError(CodeDependency, "report service is not configured", nil))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"reportIdProvided": stringArgument(args, "reportId", "report_id") != ""})
		return result
	}
	reportID := stringArgument(args, "reportId", "report_id")
	if reportID == "" {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(ValidationError(map[string]string{"reportId": "is required"}))
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"reportIdProvided": false})
		return result
	}
	report, err := s.reports.GetReport(ctx, reqCtx, reportID)
	if err != nil {
		result.Status = documentMCPToolResultFailed
		result.Error = toolErrorFromError(err)
		s.recordToolCall(ctx, reqCtx, result, map[string]any{"reportId": reportID})
		return result
	}
	result.Status = documentMCPToolResultSucceeded
	result.Report = reportSummary(report)
	if report.LatestReportFileID != "" && s.reportFiles != nil {
		reportFile, err := s.reportFiles.GetReportFile(ctx, reqCtx, report.LatestReportFileID)
		if err != nil {
			result.Status = documentMCPToolResultFailed
			result.Error = toolErrorFromError(err)
			s.recordToolCall(ctx, reqCtx, result, map[string]any{"reportId": reportID, "latestReportFileId": report.LatestReportFileID})
			return result
		}
		result.ReportFile = reportFileSummary(reportFile)
	}
	s.recordToolCall(ctx, reqCtx, result, map[string]any{
		"reportId":              report.ID,
		"latestReportFileIdSet": report.LatestReportFileID != "",
	})
	return result
}

func (s *MCPToolService) recordToolCall(ctx context.Context, reqCtx RequestContext, result MCPToolCallResult, summary map[string]any) {
	if s.recorder == nil {
		return
	}
	operationResult := OperationResultSucceeded
	if result.Status == documentMCPToolResultFailed || result.Error != nil {
		operationResult = OperationResultFailed
	}
	targetType, targetID := mcpResultTarget(result)
	errorMessage := ""
	if result.Error != nil {
		errorMessage = result.Error.Code
	}
	recordOperationLog(ctx, s.recorder, OperationLog{
		OperatorID:       reqCtx.UserID,
		OperatorName:     reqCtx.UserID,
		OperationType:    OperationDocumentMCPToolCall,
		TargetType:       targetType,
		TargetID:         targetID,
		RequestID:        reqCtx.RequestID,
		RequestSource:    documentMCPRequestSource,
		ToolName:         result.ToolName,
		OperationResult:  operationResult,
		ErrorMessage:     errorMessage,
		ParameterSummary: safeToolSummary(summary),
		Metadata: map[string]any{
			"status": result.Status,
		},
		CreatedAt: s.now(),
	})
}

func mcpResultTarget(result MCPToolCallResult) (string, string) {
	if result.Job != nil && result.Job.ID != "" {
		return "job", result.Job.ID
	}
	if result.ReportFile != nil && result.ReportFile.ID != "" {
		return "report_file", result.ReportFile.ID
	}
	if result.Report != nil && result.Report.ID != "" {
		return "report", result.Report.ID
	}
	if result.TemplateSchema != nil && result.TemplateSchema.TemplateID != "" {
		return "report_template", result.TemplateSchema.TemplateID
	}
	return "mcp_tool", result.ToolName
}

func normalizeMCPRequestContext(reqCtx RequestContext) RequestContext {
	if strings.TrimSpace(reqCtx.RequestID) == "" {
		reqCtx.RequestID = "req_" + newID()
	}
	if strings.TrimSpace(reqCtx.CallerService) == "" {
		reqCtx.CallerService = documentMCPRequestSource
	}
	return reqCtx
}

func decodeMCPArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, ValidationError(map[string]string{"arguments": "must be a JSON object"})
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func stringArgument(args map[string]any, names ...string) string {
	for _, name := range names {
		value, ok := args[name]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func stringSliceArgument(args map[string]any, names ...string) []string {
	values, _ := stringSliceArgumentStrict(args, names...)
	return values
}

func stringSliceArgumentStrict(args map[string]any, names ...string) ([]string, error) {
	for _, name := range names {
		value, ok := args[name]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return compactStrings(typed), nil
		case []any:
			values := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("argument %s must be an array of strings", name)
				}
				if strings.TrimSpace(text) != "" {
					values = append(values, strings.TrimSpace(text))
				}
			}
			return values, nil
		default:
			return nil, fmt.Errorf("argument %s must be an array of strings", name)
		}
	}
	return nil, nil
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func mapArgument(args map[string]any, names ...string) map[string]any {
	value, _ := mapArgumentStrict(args, names...)
	return value
}

func mapArgumentStrict(args map[string]any, names ...string) (map[string]any, error) {
	for _, name := range names {
		value, ok := args[name]
		if !ok || value == nil {
			continue
		}
		if mapped, ok := value.(map[string]any); ok {
			return mapped, nil
		}
		return nil, fmt.Errorf("argument %s must be a JSON object", name)
	}
	return nil, nil
}

func rawJSONArgument(args map[string]any, names ...string) (json.RawMessage, error) {
	for _, name := range names {
		value, ok := args[name]
		if !ok || value == nil {
			continue
		}
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("argument %s must be a JSON object", name)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	return nil, nil
}

func toolErrorFromError(err error) *MCPToolError {
	if err == nil {
		return nil
	}
	if appErr, ok := Classify(err); ok {
		return &MCPToolError{Code: string(appErr.Code), Message: appErr.Message, Fields: appErr.Fields}
	}
	return &MCPToolError{Code: string(CodeInternal), Message: "document MCP tool failed"}
}

func reportJobSummary(job ReportJob) *MCPReportJobSummary {
	return &MCPReportJobSummary{
		ID:         job.ID,
		ReportID:   job.ReportID,
		JobType:    string(job.JobType),
		TargetType: job.TargetType,
		TargetID:   job.TargetID,
		Status:     string(job.Status),
		Progress:   safeToolSummary(job.Progress),
		ErrorCode:  job.ErrorCode,
		CreatedAt:  formatMCPTime(job.CreatedAt),
	}
}

func reportSummary(report Report) *MCPReportSummary {
	return &MCPReportSummary{
		ID:                 report.ID,
		Name:               report.Name,
		ReportType:         report.ReportType,
		TemplateID:         report.TemplateID,
		Status:             string(report.Status),
		LatestJobID:        report.LatestJobID,
		LatestReportFileID: report.LatestReportFileID,
		GeneratedAt:        formatMCPTimePtr(report.GeneratedAt),
		ExportedAt:         formatMCPTimePtr(report.ExportedAt),
		UpdatedAt:          formatMCPTime(report.UpdatedAt),
	}
}

func reportFileSummary(reportFile ReportFile) *MCPReportFileSummary {
	summary := &MCPReportFileSummary{
		ID:        reportFile.ID,
		ReportID:  reportFile.ReportID,
		JobID:     reportFile.JobID,
		Filename:  reportFile.Filename,
		Format:    reportFile.Format,
		FileSize:  reportFile.FileSize,
		Status:    string(reportFile.Status),
		CreatedAt: formatMCPTime(reportFile.CreatedAt),
	}
	if reportFile.ID != "" {
		summary.ContentPath = "/api/v1/report-files/" + reportFile.ID + "/content"
	}
	return summary
}

func formatMCPTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatMCPTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatMCPTime(*value)
}

func emptyJSONIfNil(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func generationParameterSummary(args map[string]any, jobType JobType, section bool) map[string]any {
	requirements := stringArgument(args, "requirements")
	return safeToolSummary(map[string]any{
		"reportId":           stringArgument(args, "reportId", "report_id"),
		"sectionId":          stringArgument(args, "sectionId", "section_id"),
		"jobType":            string(jobType),
		"sectionTarget":      section,
		"requirementsLength": len(strings.TrimSpace(requirements)),
		"materialCount":      len(stringSliceArgument(args, "materialIds", "material_ids", "materialRefs", "material_refs")),
		"optionsProvided":    mapArgument(args, "options") != nil,
		"retrievalProvided":  mapArgument(args, "retrieval") != nil,
	})
}

func exportParameterSummary(args map[string]any) map[string]any {
	return safeToolSummary(map[string]any{
		"reportId":          stringArgument(args, "reportId", "report_id"),
		"templateId":        stringArgument(args, "templateId", "template_id"),
		"format":            firstNonEmpty(stringArgument(args, "format"), ReportFileFormatDOCX),
		"exportOptionsSet":  mapArgument(args, "exportOptions", "export_options", "styleOptions", "style_options") != nil,
		"richDocxRequested": false,
		"basicDocxExporter": true,
	})
}

func safeToolSummary(input map[string]any) map[string]any {
	return sanitizeMap(input)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func toolDefinition(name, description string, inputSchema map[string]any) MCPToolDefinition {
	return MCPToolDefinition{Name: name, Description: description, InputSchema: inputSchema}
}

func jobToolSchema(requireSection bool) map[string]any {
	required := []any{"reportId"}
	properties := map[string]any{
		"reportId":     map[string]any{"type": "string", "description": "Report business ID."},
		"requirements": map[string]any{"type": "string", "description": "Optional generation requirements. Stored only as a length summary in logs."},
		"materialIds": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional material business IDs.",
		},
		"options":   map[string]any{"type": "object", "additionalProperties": true},
		"retrieval": map[string]any{"type": "object", "additionalProperties": true},
	}
	if requireSection {
		required = append(required, "sectionId")
		properties["sectionId"] = map[string]any{"type": "string", "description": "Report section business ID."}
	}
	return objectSchema(required, properties)
}

func requiredStringSchema(name, description string) map[string]any {
	return objectSchema([]any{name}, map[string]any{
		name: map[string]any{"type": "string", "description": description},
	})
}

func exportDOCXSchema() map[string]any {
	return objectSchema([]any{"reportId"}, map[string]any{
		"reportId":      map[string]any{"type": "string", "description": "Report business ID."},
		"templateId":    map[string]any{"type": "string", "description": "Optional report template business ID."},
		"format":        map[string]any{"type": "string", "enum": []any{ReportFileFormatDOCX}, "default": ReportFileFormatDOCX},
		"exportOptions": map[string]any{"type": "object", "additionalProperties": true},
	})
}

func objectSchema(required []any, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}
