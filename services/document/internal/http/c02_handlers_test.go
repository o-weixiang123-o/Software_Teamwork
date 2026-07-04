package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/service"
)

func TestListReportTemplatesUsesPagedEnvelopeAndHidesFileRef(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	documents := &fakeDocumentService{
		listTemplates: func(ctx context.Context, reqCtx service.RequestContext, filter service.ReportTemplateListFilter) (service.ReportTemplateListResult, error) {
			if reqCtx.UserID != "usr_test" {
				t.Fatalf("UserID = %q, want usr_test", reqCtx.UserID)
			}
			return service.ReportTemplateListResult{
				Items: []service.ReportTemplate{{
					ID:           "00000000-0000-0000-0000-000000000101",
					TemplateName: "inspection",
					ReportType:   "summer_peak_inspection",
					Version:      1,
					FileRef:      "file_internal_123",
					Filename:     "inspection.docx",
					FileSize:     128,
					Enabled:      true,
					CreatedAt:    now,
					UpdatedAt:    now,
				}},
				Page: service.PageMeta{Page: 1, PageSize: 20, Total: 1},
			}, nil
		},
	}
	server := NewServer(Config{DocumentService: documents})

	req := httptest.NewRequest(http.MethodGet, "/report-templates?page=1&pageSize=20", nil)
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Request-Id", "req_templates")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoDocumentInternals(t, body)
	var decoded struct {
		Data []struct {
			ID           string `json:"id"`
			TemplateName string `json:"templateName"`
			Filename     string `json:"filename"`
		} `json:"data"`
		Page      pageResponse `json:"page"`
		RequestID string       `json:"requestId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.RequestID != "req_templates" || decoded.Page.Total != 1 || decoded.Data[0].Filename != "inspection.docx" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
}

func TestCreateReportTemplateRejectsMissingFile(t *testing.T) {
	server := NewServer(Config{DocumentService: &fakeDocumentService{}})
	req := httptest.NewRequest(http.MethodPost, "/report-templates", strings.NewReader("not multipart"))
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "validation_error") {
		t.Fatalf("expected validation_error body, got %s", rec.Body.String())
	}
}

func TestCreateReportMaterialParsesMultipartAndHidesFileRef(t *testing.T) {
	now := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	documents := &fakeDocumentService{
		createMaterial: func(ctx context.Context, reqCtx service.RequestContext, input service.CreateReportMaterialInput) (service.ReportMaterial, error) {
			if input.MaterialName != "coal photo" || input.MaterialType != "image" || len(input.Tags) != 2 {
				t.Fatalf("unexpected material input: %+v", input)
			}
			return service.ReportMaterial{
				ID:           "00000000-0000-0000-0000-000000000201",
				MaterialName: input.MaterialName,
				MaterialType: input.MaterialType,
				FileRef:      "file_internal_456",
				Filename:     input.File.Filename,
				FileSize:     input.File.SizeBytes,
				Tags:         input.Tags,
				Enabled:      true,
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
	}
	body, contentType := multipartBody(t, map[string]string{
		"materialName": "coal photo",
		"materialType": "image",
		"tags":         "audit,coal",
	}, "photo.png", "abc")
	server := NewServer(Config{DocumentService: documents})
	req := httptest.NewRequest(http.MethodPost, "/report-materials", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-User-Id", "usr_test")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	assertNoDocumentInternals(t, rec.Body.String())
}

func TestGetReportTemplateAndMaterialHideStorageInternals(t *testing.T) {
	now := time.Date(2026, 6, 29, 13, 30, 0, 0, time.UTC)
	documents := &fakeDocumentService{
		getTemplate: func(context.Context, service.RequestContext, string) (service.ReportTemplate, error) {
			return service.ReportTemplate{
				ID:           "tpl-1",
				TemplateName: "inspection",
				ReportType:   "summer_peak_inspection",
				Version:      1,
				FileRef:      "file_internal_template",
				Filename:     "inspection.docx",
				FileSize:     128,
				Enabled:      true,
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
		getMaterial: func(context.Context, service.RequestContext, string) (service.ReportMaterial, error) {
			return service.ReportMaterial{
				ID:           "mat-1",
				MaterialName: "audit evidence",
				MaterialType: "docx",
				FileRef:      "file_internal_material",
				Filename:     "evidence.docx",
				FileSize:     64,
				Tags:         []string{"audit"},
				Enabled:      true,
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
	}
	server := NewServer(Config{DocumentService: documents})

	for _, item := range []struct {
		name string
		path string
	}{
		{name: "template", path: "/report-templates/tpl-1"},
		{name: "material", path: "/report-materials/mat-1"},
	} {
		t.Run(item.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, item.path, nil)
			req.Header.Set("X-User-Id", "usr_test")
			rec := httptest.NewRecorder()

			server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			assertNoDocumentInternals(t, rec.Body.String())
		})
	}
}

func assertNoDocumentInternals(t *testing.T, body string) {
	t.Helper()
	for _, marker := range []string{
		"file_internal",
		"file_ref",
		"fileRef",
		"objectKey",
		"object_key",
		"bucket",
		"minio",
		"X-Amz-Signature",
		"provider.internal",
		"elasticsearch",
		"prompt=",
		"sk-",
	} {
		if strings.Contains(body, marker) {
			t.Fatalf("response leaked internal marker %q: %s", marker, body)
		}
	}
}

func multipartBody(t *testing.T, fields map[string]string, filename string, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

type fakeDocumentService struct {
	listTypes       func(context.Context, service.RequestContext) ([]service.ReportType, error)
	listTemplates   func(context.Context, service.RequestContext, service.ReportTemplateListFilter) (service.ReportTemplateListResult, error)
	createTemplate  func(context.Context, service.RequestContext, service.CreateReportTemplateInput) (service.ReportTemplate, error)
	createMaterial  func(context.Context, service.RequestContext, service.CreateReportMaterialInput) (service.ReportMaterial, error)
	listMaterials   func(context.Context, service.RequestContext, service.ReportMaterialListFilter) (service.ReportMaterialListResult, error)
	getTemplate     func(context.Context, service.RequestContext, string) (service.ReportTemplate, error)
	updateTemplate  func(context.Context, service.RequestContext, service.UpdateReportTemplateInput) (service.ReportTemplate, error)
	deleteTemplate  func(context.Context, service.RequestContext, string) error
	getStructure    func(context.Context, service.RequestContext, string) (service.ReportTemplateStructure, error)
	updateStructure func(context.Context, service.RequestContext, service.UpdateReportTemplateStructureInput) (service.ReportTemplateStructure, error)
	getMaterial     func(context.Context, service.RequestContext, string) (service.ReportMaterial, error)
	deleteMaterial  func(context.Context, service.RequestContext, string) error
}

func (f *fakeDocumentService) ListReportTypes(ctx context.Context, reqCtx service.RequestContext) ([]service.ReportType, error) {
	if f.listTypes == nil {
		return nil, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.listTypes(ctx, reqCtx)
}

func (f *fakeDocumentService) ListReportTemplates(ctx context.Context, reqCtx service.RequestContext, filter service.ReportTemplateListFilter) (service.ReportTemplateListResult, error) {
	if f.listTemplates == nil {
		return service.ReportTemplateListResult{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.listTemplates(ctx, reqCtx, filter)
}

func (f *fakeDocumentService) CreateReportTemplate(ctx context.Context, reqCtx service.RequestContext, input service.CreateReportTemplateInput) (service.ReportTemplate, error) {
	if f.createTemplate == nil {
		return service.ReportTemplate{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.createTemplate(ctx, reqCtx, input)
}

func (f *fakeDocumentService) GetReportTemplate(ctx context.Context, reqCtx service.RequestContext, id string) (service.ReportTemplate, error) {
	if f.getTemplate == nil {
		return service.ReportTemplate{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.getTemplate(ctx, reqCtx, id)
}

func (f *fakeDocumentService) UpdateReportTemplate(ctx context.Context, reqCtx service.RequestContext, input service.UpdateReportTemplateInput) (service.ReportTemplate, error) {
	if f.updateTemplate == nil {
		return service.ReportTemplate{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.updateTemplate(ctx, reqCtx, input)
}

func (f *fakeDocumentService) DeleteReportTemplate(ctx context.Context, reqCtx service.RequestContext, id string) error {
	if f.deleteTemplate == nil {
		return service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.deleteTemplate(ctx, reqCtx, id)
}

func (f *fakeDocumentService) GetReportTemplateStructure(ctx context.Context, reqCtx service.RequestContext, id string) (service.ReportTemplateStructure, error) {
	if f.getStructure == nil {
		return service.ReportTemplateStructure{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.getStructure(ctx, reqCtx, id)
}

func (f *fakeDocumentService) UpdateReportTemplateStructure(ctx context.Context, reqCtx service.RequestContext, input service.UpdateReportTemplateStructureInput) (service.ReportTemplateStructure, error) {
	if f.updateStructure == nil {
		return service.ReportTemplateStructure{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.updateStructure(ctx, reqCtx, input)
}

func (f *fakeDocumentService) ListReportMaterials(ctx context.Context, reqCtx service.RequestContext, filter service.ReportMaterialListFilter) (service.ReportMaterialListResult, error) {
	if f.listMaterials == nil {
		return service.ReportMaterialListResult{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.listMaterials(ctx, reqCtx, filter)
}

func (f *fakeDocumentService) CreateReportMaterial(ctx context.Context, reqCtx service.RequestContext, input service.CreateReportMaterialInput) (service.ReportMaterial, error) {
	if f.createMaterial == nil {
		return service.ReportMaterial{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.createMaterial(ctx, reqCtx, input)
}

func (f *fakeDocumentService) GetReportMaterial(ctx context.Context, reqCtx service.RequestContext, id string) (service.ReportMaterial, error) {
	if f.getMaterial == nil {
		return service.ReportMaterial{}, service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.getMaterial(ctx, reqCtx, id)
}

func (f *fakeDocumentService) DeleteReportMaterial(ctx context.Context, reqCtx service.RequestContext, id string) error {
	if f.deleteMaterial == nil {
		return service.NewError(service.CodeInternal, "fake method not configured", nil)
	}
	return f.deleteMaterial(ctx, reqCtx, id)
}
