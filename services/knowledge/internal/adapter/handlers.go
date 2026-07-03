package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

func (s *Server) handleListKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	items, total, err := s.vendor.ListDatasets(r.Context(), s.runtimeScopeID(), page.Page, page.PageSize)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writePageJSON(w, http.StatusOK, knowledgeBasesFromVendor(items), service.Page{
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    total,
	}, reqCtx.RequestID)
}

func (s *Server) handleCreateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	var body createKnowledgeBaseRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"name": "is required"}))
		return
	}
	parserConfig, err := s.resolveCreateParserConfig(r.Context(), body)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	payload, err := buildCreateDatasetBody(body, parserConfig, createDatasetOptions{
		VendorEmbeddingID: s.cfg.VendorEmbeddingID,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	created, err := s.vendor.CreateDataset(r.Context(), s.runtimeScopeID(), payload)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeJSON(w, http.StatusCreated, knowledgeBaseFromVendor(created), reqCtx.RequestID)
}

func (s *Server) resolveCreateParserConfig(ctx context.Context, body createKnowledgeBaseRequest) (map[string]any, error) {
	if body.ChunkStrategy != nil || s.parserConfigs == nil {
		return nil, nil
	}
	snapshot, err := s.parserConfigs.ResolveParserConfig(ctx, "")
	if err != nil {
		return nil, err
	}
	return ragflowParserConfigFromSnapshot(snapshot), nil
}

func (s *Server) handleGetKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	ref, err := s.resolveKnowledgeBaseRuntimeRef(r.Context(), r.PathValue("knowledgeBaseId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	dataset, err := s.vendor.GetDataset(r.Context(), s.runtimeScopeID(), ref.ID)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeJSON(w, http.StatusOK, knowledgeBaseFromVendor(dataset), reqCtx.RequestID)
}

func (s *Server) handleUpdateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	var body updateKnowledgeBaseRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	payload, err := buildUpdateDatasetBody(body)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	updated, err := s.vendor.UpdateDataset(r.Context(), s.runtimeScopeID(), r.PathValue("knowledgeBaseId"), payload)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeJSON(w, http.StatusOK, knowledgeBaseFromVendor(updated), reqCtx.RequestID)
}

func (s *Server) handleDeleteKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	if err := s.vendor.DeleteDataset(r.Context(), s.runtimeScopeID(), r.PathValue("knowledgeBaseId")); err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	ref, err := s.resolveKnowledgeBaseRuntimeRef(r.Context(), r.PathValue("knowledgeBaseId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	items, total, err := s.vendor.ListDocuments(r.Context(), s.runtimeScopeID(), ref.ID, page.Page, page.PageSize)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writePageJSON(w, http.StatusOK, documentsFromVendor(items), service.Page{
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    total,
	}, reqCtx.RequestID)
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	file, header, ok := parseDocumentUpload(w, r, s.maxUploadBytes)
	if !ok {
		return
	}
	defer file.Close()

	contentType := ""
	if header != nil {
		contentType = strings.TrimSpace(header.Header.Get("Content-Type"))
	}
	uploaded, err := s.vendor.UploadDocument(r.Context(), s.runtimeScopeID(), r.PathValue("knowledgeBaseId"), header.Filename, contentType, file)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	kbID := r.PathValue("knowledgeBaseId")
	docID := stringField(uploaded, "id")
	if s.cfg.AutoStartIngestion && docID != "" {
		if err := s.vendor.StartDocumentParse(r.Context(), s.runtimeScopeID(), kbID, []string{docID}); err != nil {
			if delErr := s.vendor.DeleteDocument(r.Context(), s.runtimeScopeID(), kbID, docID); delErr != nil {
				s.logger.WarnContext(r.Context(), "upload parse failed and document cleanup failed",
					"service", "knowledge-adapter",
					"request_id", reqCtx.RequestID,
					"document_id", docID,
					"parse_error", err,
					"delete_error", delErr,
				)
			}
			writeAppError(w, r, mapVendorError(err))
			return
		}
		uploaded["run"] = "RUNNING"
		s.startRuntimeWorkerForIngestionAsync(reqCtx.RequestID, docID)
	}
	writeJSON(w, http.StatusCreated, documentFromVendor(uploaded), reqCtx.RequestID)
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	ref, err := s.resolveDocumentRuntimeRef(r.Context(), r.PathValue("documentId"), r.URL.Query().Get("knowledgeBaseId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	doc, err := s.vendor.GetDatasetDocument(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeJSON(w, http.StatusOK, documentFromVendor(doc), reqCtx.RequestID)
}

func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	var body updateDocumentRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Tags == nil {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"body": "must include at least one supported field"}))
		return
	}
	kbID, err := requiredDocumentKnowledgeBaseID(r)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	_, err = s.vendor.GetDatasetDocument(r.Context(), s.runtimeScopeID(), kbID, r.PathValue("documentId"))
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	payload, err := buildUpdateDocumentBody(*body.Tags)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	updated, err := s.vendor.UpdateDocument(r.Context(), s.runtimeScopeID(), kbID, r.PathValue("documentId"), payload)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	updated = documentVendorWithTags(updated, *body.Tags)
	writeJSON(w, http.StatusOK, documentFromVendor(updated), reqCtx.RequestID)
}

func requiredDocumentKnowledgeBaseID(r *http.Request) (string, error) {
	kbID := strings.TrimSpace(r.URL.Query().Get("knowledgeBaseId"))
	if kbID == "" {
		return "", service.ValidationError("request validation failed", map[string]string{"knowledgeBaseId": "is required"})
	}
	return kbID, nil
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := mutationScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	documentID := r.PathValue("documentId")
	kbID, err := requiredDocumentKnowledgeBaseID(r)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	if err := s.vendor.DeleteDocument(r.Context(), s.runtimeScopeID(), kbID, documentID); err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListDocumentChunks(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	ref, err := s.resolveDocumentRuntimeRef(r.Context(), r.PathValue("documentId"), r.URL.Query().Get("knowledgeBaseId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	if chunkID := strings.TrimSpace(r.URL.Query().Get("id")); chunkID != "" {
		chunk, err := s.vendor.GetChunk(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID, chunkID)
		if err != nil {
			writeAppError(w, r, mapVendorError(err))
			return
		}
		writePageJSON(w, http.StatusOK, []documentChunkSummary{documentChunkFromVendor(chunk, ref.KnowledgeBaseID, ref.ID, 0)}, service.Page{
			Page:     page.Page,
			PageSize: page.PageSize,
			Total:    1,
		}, reqCtx.RequestID)
		return
	}
	_, err = s.vendor.GetDatasetDocument(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	chunks, total, err := s.vendor.ListChunks(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID, page.Page, page.PageSize)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	fallbackStart := (page.Page - 1) * page.PageSize
	writePageJSON(w, http.StatusOK, documentChunksFromVendor(chunks, ref.KnowledgeBaseID, ref.ID, fallbackStart), service.Page{
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    total,
	}, reqCtx.RequestID)
}

func (s *Server) handleGetDocumentContent(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	ref, err := s.resolveDocumentRuntimeRef(r.Context(), r.PathValue("documentId"), r.URL.Query().Get("knowledgeBaseId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	_, err = s.vendor.GetDatasetDocument(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	contentType, body, err := s.vendor.DownloadDocument(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID)
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	w.Header().Set("Content-Type", contentType)
	if len(body) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	chunkID := strings.TrimSpace(r.PathValue("chunkId"))
	if chunkID == "" {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"chunkId": "is required"}))
		return
	}

	documentID := strings.TrimSpace(r.URL.Query().Get("documentId"))
	if documentID != "" {
		ref, err := s.resolveDocumentRuntimeRef(r.Context(), documentID, r.URL.Query().Get("knowledgeBaseId"))
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		chunk, err := s.vendor.GetChunk(r.Context(), s.runtimeScopeID(), ref.KnowledgeBaseID, ref.ID, chunkID)
		if err != nil {
			writeAppError(w, r, mapVendorError(err))
			return
		}
		writeJSON(w, http.StatusOK, documentChunkFromVendor(chunk, ref.KnowledgeBaseID, ref.ID, 0), reqCtx.RequestID)
		return
	}

	docs, err := s.listRuntimeDocuments(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	for _, doc := range docs {
		docID := strings.TrimSpace(doc.ID)
		kbID := strings.TrimSpace(doc.KnowledgeBaseID)
		if docID == "" || kbID == "" {
			continue
		}
		chunk, err := s.vendor.GetChunk(r.Context(), s.runtimeScopeID(), kbID, docID, chunkID)
		if err == nil {
			writeJSON(w, http.StatusOK, documentChunkFromVendor(chunk, kbID, docID, 0), reqCtx.RequestID)
			return
		}
		if isVendorNotFound(err) {
			continue
		}
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeAppError(w, r, service.NotFoundError("chunk not found"))
}

func (s *Server) handleCreateKnowledgeQuery(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if !trustedProjectRetrievalScope(reqCtx, r) {
		if _, err := readScope(reqCtx); err != nil {
			writeAppError(w, r, err)
			return
		}
	}
	var body knowledgeQueryRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	data, err := s.runKnowledgeQuery(r.Context(), s.runtimeScopeID(), body, retrievalBuildOptions{
		VendorRerankID: s.cfg.VendorRerankID,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	topK := body.TopK
	if topK <= 0 {
		topK = 10
	}
	scoreThreshold := 0.0
	if body.ScoreThreshold != nil {
		scoreThreshold = *body.ScoreThreshold
	}
	writeJSON(w, http.StatusCreated, knowledgeQueryFromVendor(newQueryID(), strings.TrimSpace(body.Query), data, topK, scoreThreshold, body.Rerank, body.RerankTopN, knowledgeQueryTraceOptions{
		VendorEmbeddingID: s.cfg.VendorEmbeddingID,
	}), reqCtx.RequestID)
}

func (s *Server) handleKnowledgeStatistics(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		writeJSON(w, http.StatusOK, knowledgeStatisticsSummary{}, requestIDFromContext(r.Context()))
		return
	}
	reqCtx, ok := s.gatewayContext(w, r)
	if !ok {
		return
	}
	if _, err := readScope(reqCtx); err != nil {
		writeAppError(w, r, err)
		return
	}
	stats, err := s.collectKnowledgeStatistics(r.Context(), s.runtimeScopeID())
	if err != nil {
		writeAppError(w, r, mapVendorError(err))
		return
	}
	writeJSON(w, http.StatusOK, stats, reqCtx.RequestID)
}

func (s *Server) collectKnowledgeStatistics(ctx context.Context, runtimeScopeID string) (knowledgeStatisticsSummary, error) {
	const pageSize = 100
	var datasets []map[string]interface{}
	var kbCount int64
	for page := 1; ; page++ {
		items, total, err := s.vendor.ListDatasets(ctx, runtimeScopeID, page, pageSize)
		if err != nil {
			return knowledgeStatisticsSummary{}, err
		}
		datasets = append(datasets, items...)
		if total > kbCount {
			kbCount = total
		}
		if len(items) == 0 || int64(page*pageSize) >= total {
			break
		}
	}
	if kbCount == 0 {
		kbCount = int64(len(datasets))
	}

	var documentCount int64
	for _, dataset := range datasets {
		documentCount += int64Field(dataset, "document_count", "doc_num")
	}

	return knowledgeStatisticsSummary{
		KnowledgeBaseCount: kbCount,
		DocumentCount:      documentCount,
	}, nil
}

func parsePageQuery(r *http.Request) (service.PageInput, error) {
	page := parsePositiveIntParam(r, "page")
	pageSize := parsePositiveIntParam(r, "pageSize")
	return normalizePage(page, pageSize)
}

func parsePositiveIntParam(r *http.Request, name string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return value
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, defaultMaxJSONBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		fieldMessage := "must be a valid JSON object"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			fieldMessage = "exceeds maximum JSON body size"
		}
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"body": fieldMessage}))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"body": "must contain only one JSON object"}))
		return false
	}
	return true
}

func parseDocumentUpload(w http.ResponseWriter, r *http.Request, maxUploadBytes int64) (multipart.File, *multipart.FileHeader, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		fieldMessage := "multipart form is invalid"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			fieldMessage = "exceeds maximum upload size"
		}
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"file": fieldMessage}))
		return nil, nil, false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"file": "is required"}))
		return nil, nil, false
	}
	if header == nil || header.Size == 0 {
		_ = file.Close()
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"file": "must not be empty"}))
		return nil, nil, false
	}
	return file, header, true
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "req_fallback"
	}
	return hex.EncodeToString(buf[:])
}

func (s *Server) logRequestFailure(ctx context.Context, requestID, method, path string, status int, durationMs int64) {
	s.logger.ErrorContext(ctx, "http request failed",
		"service", "knowledge-adapter",
		"request_id", requestID,
		"method", method,
		"path", path,
		"status", status,
		"duration_ms", durationMs,
	)
}
