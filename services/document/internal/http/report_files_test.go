package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/service"
)

type fakeReportFileService struct {
	files   []service.ReportFile
	content service.FileContent
	err     error
}

func (f *fakeReportFileService) ListReportFiles(context.Context, service.RequestContext, service.ReportFileListFilter) (service.ReportFileListResult, error) {
	return service.ReportFileListResult{
		Items: f.files,
		Page:  service.PageMeta{Page: 1, PageSize: 20, Total: len(f.files)},
	}, nil
}

func (f *fakeReportFileService) CreateReportFile(_ context.Context, rctx service.RequestContext, input service.CreateReportFileInput) (service.ReportFile, error) {
	return service.ReportFile{
		ID:        "rf-1",
		ReportID:  input.ReportID,
		JobID:     "job-1",
		Filename:  "report.docx",
		Format:    input.Format,
		Status:    service.ReportFileStatusPending,
		CreatedBy: rctx.UserID,
		CreatedAt: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (f *fakeReportFileService) GetReportFile(context.Context, service.RequestContext, string) (service.ReportFile, error) {
	if len(f.files) == 0 {
		return service.ReportFile{}, service.NewError(service.CodeNotFound, "report file not found", nil)
	}
	return f.files[0], nil
}

func (f *fakeReportFileService) ReadReportFileContent(context.Context, service.RequestContext, string) (service.FileContent, error) {
	if f.err != nil {
		return service.FileContent{}, f.err
	}
	return f.content, nil
}

func TestCreateReportFileReturnsAcceptedSafeDTO(t *testing.T) {
	server := NewServer(Config{ReportFileSvc: &fakeReportFileService{}})
	req := httptest.NewRequest(http.MethodPost, "/report-files", strings.NewReader(`{"reportId":"report-1","format":"docx"}`))
	req.Header.Set("X-User-Id", "user-1")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "fileRef") || strings.Contains(body, "file_ref") || strings.Contains(body, "file-internal") {
		t.Fatalf("response leaked file internals: %s", body)
	}
	var envelope struct {
		Data struct {
			ID          string `json:"id"`
			ContentPath string `json:"contentPath"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.ID != "rf-1" || envelope.Data.ContentPath != "/api/v1/report-files/rf-1/content" {
		t.Fatalf("unexpected response data: %+v", envelope.Data)
	}
}

func TestGetReportFileReturnsSafeDTO(t *testing.T) {
	now := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	server := NewServer(Config{ReportFileSvc: &fakeReportFileService{
		files: []service.ReportFile{{
			ID:        "rf-1",
			ReportID:  "report-1",
			JobID:     "job-1",
			FileRef:   "file_internal_report",
			Filename:  "report.docx",
			Format:    service.ReportFileFormatDOCX,
			FileSize:  128,
			Status:    service.ReportFileStatusSucceeded,
			CreatedBy: "user-1",
			CreatedAt: now,
		}},
	}})
	req := httptest.NewRequest(http.MethodGet, "/report-files/rf-1", nil)
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Request-Id", "req-file")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertNoDocumentInternals(t, rec.Body.String())
	var envelope struct {
		Data struct {
			ID          string `json:"id"`
			ContentPath string `json:"contentPath"`
			FileSize    int64  `json:"fileSize"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.ID != "rf-1" || envelope.Data.FileSize != 128 || envelope.RequestID != "req-file" {
		t.Fatalf("unexpected response: %+v", envelope)
	}
}

func TestGetReportFileContentStreamsBinary(t *testing.T) {
	server := NewServer(Config{ReportFileSvc: &fakeReportFileService{
		content: service.FileContent{
			Filename:    "report.docx",
			ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			SizeBytes:   10,
			Content:     io.NopCloser(strings.NewReader("docx-bytes")),
		},
	}})
	req := httptest.NewRequest(http.MethodGet, "/report-files/rf-1/content", nil)
	req.Header.Set("X-User-Id", "user-1")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "docx-bytes" {
		t.Fatalf("body = %q", got)
	}
	if strings.Contains(rec.Body.String(), `"data"`) {
		t.Fatalf("binary content was wrapped as JSON: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "report.docx") {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestGetReportFileContentFailureUsesErrorEnvelope(t *testing.T) {
	repo := &contractReportFileRepository{
		report: service.Report{ID: "report-1", CreatorID: "user-1"},
		file: service.ReportFile{
			ID:        "rf-1",
			ReportID:  "report-1",
			FileRef:   "file_internal_report",
			Filename:  "report.docx",
			Format:    service.ReportFileFormatDOCX,
			Status:    service.ReportFileStatusSucceeded,
			CreatedBy: "user-1",
			CreatedAt: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		},
	}
	files := &leakingReportFileContentClient{
		err: service.NewError(
			service.CodeDependency,
			"minio bucket raw objectKey provider.internal elasticsearch prompt=secret",
			nil,
		),
	}
	server := NewServer(Config{
		ReportFileSvc: service.NewReportFileService(repo, files, nil, nil),
	})
	req := httptest.NewRequest(http.MethodGet, "/report-files/rf-1/content", nil)
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Request-Id", "req-file-error")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON error envelope", got)
	}
	assertNoDocumentInternals(t, rec.Body.String())
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Code != "dependency_error" || envelope.Error.RequestID != "req-file-error" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

type contractReportFileRepository struct {
	report service.Report
	file   service.ReportFile
}

func (f *contractReportFileRepository) GetReportByID(context.Context, string) (service.Report, error) {
	return f.report, nil
}

func (f *contractReportFileRepository) ListReportSections(context.Context, string) ([]service.ReportSection, error) {
	return nil, errors.New("unexpected ListReportSections call")
}

func (f *contractReportFileRepository) ListReportOutlines(context.Context, string) ([]service.ReportOutline, error) {
	return nil, errors.New("unexpected ListReportOutlines call")
}

func (f *contractReportFileRepository) FindReportJobByID(context.Context, string) (service.ReportJob, error) {
	return service.ReportJob{}, errors.New("unexpected FindReportJobByID call")
}

func (f *contractReportFileRepository) CreateReportJob(context.Context, service.ReportJob) (service.ReportJob, error) {
	return service.ReportJob{}, errors.New("unexpected CreateReportJob call")
}

func (f *contractReportFileRepository) UpdateReportJobStatus(context.Context, string, service.JobStatus, string, string, *time.Time, *time.Time) (service.ReportJob, error) {
	return service.ReportJob{}, errors.New("unexpected UpdateReportJobStatus call")
}

func (f *contractReportFileRepository) UpdateJobAsynqTaskID(context.Context, string, string) error {
	return errors.New("unexpected UpdateJobAsynqTaskID call")
}

func (f *contractReportFileRepository) CreateReportJobAttempt(context.Context, service.ReportJobAttempt) (service.ReportJobAttempt, error) {
	return service.ReportJobAttempt{}, errors.New("unexpected CreateReportJobAttempt call")
}

func (f *contractReportFileRepository) UpdateAttemptAsynqTaskID(context.Context, string, string) error {
	return errors.New("unexpected UpdateAttemptAsynqTaskID call")
}

func (f *contractReportFileRepository) SetAttemptFailed(context.Context, string, string, string) error {
	return errors.New("unexpected SetAttemptFailed call")
}

func (f *contractReportFileRepository) CreateReportFile(context.Context, service.ReportFile) (service.ReportFile, error) {
	return service.ReportFile{}, errors.New("unexpected CreateReportFile call")
}

func (f *contractReportFileRepository) ListReportFiles(context.Context, service.ReportFileListFilter) ([]service.ReportFile, int, error) {
	return nil, 0, errors.New("unexpected ListReportFiles call")
}

func (f *contractReportFileRepository) GetReportFileByID(context.Context, string) (service.ReportFile, error) {
	return f.file, nil
}

func (f *contractReportFileRepository) GetReportFileByJobID(context.Context, string) (service.ReportFile, error) {
	return service.ReportFile{}, errors.New("unexpected GetReportFileByJobID call")
}

func (f *contractReportFileRepository) UpdateReportFile(context.Context, service.ReportFile) (service.ReportFile, error) {
	return service.ReportFile{}, errors.New("unexpected UpdateReportFile call")
}

type leakingReportFileContentClient struct {
	err error
}

func (f *leakingReportFileContentClient) CreateFile(context.Context, service.RequestContext, service.UploadedFile) (service.FileObject, error) {
	return service.FileObject{}, errors.New("unexpected CreateFile call")
}

func (f *leakingReportFileContentClient) ReadFileContent(context.Context, service.RequestContext, string) (service.FileContent, error) {
	return service.FileContent{}, f.err
}
