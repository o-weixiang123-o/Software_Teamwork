package vendorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const vendorCodeSuccess = 0

type Client struct {
	baseURL      string
	serviceToken string
	http         *http.Client
}

func New(baseURL string, timeout time.Duration, serviceToken string) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		serviceToken: strings.TrimSpace(serviceToken),
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

type APIError struct {
	Code       int
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("vendor api error code=%d", e.Code)
}

func (e *APIError) MatchesHTTPStatus(status int) bool {
	if e == nil {
		return false
	}
	if e.HTTPStatus == status {
		return true
	}
	return e.HTTPStatus == 0 && e.Code == status
}

type envelope struct {
	Code          int             `json:"code"`
	Message       string          `json:"message"`
	Data          json.RawMessage `json:"data"`
	TotalDatasets int64           `json:"total_datasets"`
}

type documentListData struct {
	Total int64                    `json:"total"`
	Docs  []map[string]interface{} `json:"docs"`
}

type chunkListData struct {
	Total  int64                    `json:"total"`
	Chunks []map[string]interface{} `json:"chunks"`
	Doc    map[string]interface{}   `json:"doc"`
}

type RetrievalData struct {
	Total   int64                    `json:"total"`
	Chunks  []map[string]interface{} `json:"chunks"`
	DocAggs []map[string]interface{} `json:"doc_aggs"`
}

func (c *Client) ListDatasets(ctx context.Context, userID string, page, pageSize int) ([]map[string]interface{}, int64, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		query.Set("page_size", strconv.Itoa(pageSize))
	}
	var payload envelope
	if err := c.getJSON(ctx, userID, "/api/v1/datasets?"+query.Encode(), &payload); err != nil {
		return nil, 0, err
	}
	items, err := decodeDatasetItems(payload.Data)
	if err != nil {
		return nil, 0, err
	}
	total := payload.TotalDatasets
	if total == 0 {
		total = int64(len(items))
	}
	return items, total, nil
}

func (c *Client) CreateDataset(ctx context.Context, userID string, body []byte) (map[string]interface{}, error) {
	var payload envelope
	if err := c.doJSON(ctx, userID, http.MethodPost, createDatasetPath(body), body, &payload); err != nil {
		return nil, err
	}
	return decodeObject(payload.Data)
}

func createDatasetPath(body []byte) string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return "/api/v1/datasets"
	}
	raw, ok := payload["parser_config_credentials"]
	if !ok {
		return "/api/v1/datasets"
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "/api/v1/datasets"
	}
	return "/api/v1/internal/datasets"
}

func (c *Client) GetDataset(ctx context.Context, userID, datasetID string) (map[string]interface{}, error) {
	var payload envelope
	path := "/api/v1/datasets/" + url.PathEscape(datasetID)
	if err := c.getJSON(ctx, userID, path, &payload); err != nil {
		return nil, err
	}
	return decodeObject(payload.Data)
}

func (c *Client) UpdateDataset(ctx context.Context, userID, datasetID string, body []byte) (map[string]interface{}, error) {
	var payload envelope
	path := "/api/v1/datasets/" + url.PathEscape(datasetID)
	if err := c.doJSON(ctx, userID, http.MethodPut, path, body, &payload); err != nil {
		return nil, err
	}
	return decodeObject(payload.Data)
}

func (c *Client) DeleteDataset(ctx context.Context, userID, datasetID string) error {
	body, err := json.Marshal(map[string]any{"ids": []string{datasetID}})
	if err != nil {
		return err
	}
	var payload envelope
	return c.doJSON(ctx, userID, http.MethodDelete, "/api/v1/datasets", body, &payload)
}

func (c *Client) ListDocuments(ctx context.Context, userID, datasetID string, page, pageSize int) ([]map[string]interface{}, int64, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		query.Set("page_size", strconv.Itoa(pageSize))
	}
	path := fmt.Sprintf("/api/v1/datasets/%s/documents?%s", url.PathEscape(datasetID), query.Encode())
	var payload envelope
	if err := c.getJSON(ctx, userID, path, &payload); err != nil {
		return nil, 0, err
	}
	var listed documentListData
	if len(payload.Data) == 0 {
		return nil, 0, nil
	}
	if err := json.Unmarshal(payload.Data, &listed); err != nil {
		return nil, 0, err
	}
	return listed.Docs, listed.Total, nil
}

func (c *Client) UploadDocument(ctx context.Context, userID, datasetID, filename, contentType string, content io.Reader) (map[string]interface{}, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v1/datasets/%s/documents?type=local", url.PathEscape(datasetID))
	req, err := c.newRequest(ctx, userID, http.MethodPost, path, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= http.StatusBadRequest {
		var payload envelope
		if err := json.Unmarshal(raw, &payload); err == nil {
			return nil, &APIError{Code: payload.Code, Message: payload.Message, HTTPStatus: res.StatusCode}
		}
		return nil, &APIError{Message: "vendor upload failed", HTTPStatus: res.StatusCode}
	}

	var payload envelope
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Code != vendorCodeSuccess {
		return nil, &APIError{Code: payload.Code, Message: payload.Message, HTTPStatus: res.StatusCode}
	}
	return decodeUploadDocument(payload.Data)
}

// StartDocumentParse queues deepdoc ingestion for uploaded documents in a dataset.
// Both document_ids (Python API) and documents (Go API) are sent for compatibility.
func (c *Client) StartDocumentParse(ctx context.Context, userID, datasetID string, documentIDs []string) error {
	if len(documentIDs) == 0 {
		return fmt.Errorf("document ids are required")
	}
	body, err := json.Marshal(map[string]any{
		"dataset_id":   datasetID,
		"document_ids": documentIDs,
		"documents":    documentIDs,
	})
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/datasets/%s/documents/parse", url.PathEscape(datasetID))
	var payload envelope
	return c.doJSON(ctx, userID, http.MethodPost, path, body, &payload)
}

func (c *Client) GetDocument(ctx context.Context, userID, documentID string) (map[string]interface{}, error) {
	path := "/api/v1/documents/" + url.PathEscape(documentID)
	req, err := c.newRequest(ctx, userID, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, &APIError{Code: 404, Message: "document not found", HTTPStatus: http.StatusNotFound}
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, &APIError{Message: "vendor get document failed", HTTPStatus: res.StatusCode}
	}

	var wrapped struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Data == nil {
		return nil, &APIError{Code: 404, Message: "document not found", HTTPStatus: http.StatusNotFound}
	}
	return wrapped.Data, nil
}

func (c *Client) GetDatasetDocument(ctx context.Context, userID, datasetID, documentID string) (map[string]interface{}, error) {
	const pageSize = 100
	for page := 1; ; page++ {
		items, total, err := c.ListDocuments(ctx, userID, datasetID, page, pageSize)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if strings.TrimSpace(fmt.Sprint(item["id"])) == documentID {
				return item, nil
			}
		}
		if len(items) == 0 || int64(page*pageSize) >= total {
			break
		}
	}
	return nil, &APIError{Code: 404, Message: "document not found", HTTPStatus: http.StatusNotFound}
}

func (c *Client) UpdateDocument(ctx context.Context, userID, datasetID, documentID string, body []byte) (map[string]interface{}, error) {
	var payload envelope
	path := fmt.Sprintf("/api/v1/datasets/%s/documents/%s", url.PathEscape(datasetID), url.PathEscape(documentID))
	if err := c.doJSON(ctx, userID, http.MethodPatch, path, body, &payload); err != nil {
		return nil, err
	}
	return decodeObject(payload.Data)
}

func (c *Client) DeleteDocument(ctx context.Context, userID, datasetID, documentID string) error {
	path := fmt.Sprintf("/api/v1/datasets/%s/documents", url.PathEscape(datasetID))
	body, err := json.Marshal(map[string][]string{"ids": []string{documentID}})
	if err != nil {
		return err
	}
	var payload envelope
	return c.doJSON(ctx, userID, http.MethodDelete, path, body, &payload)
}

func (c *Client) ListChunks(ctx context.Context, userID, datasetID, documentID string, page, pageSize int) ([]map[string]interface{}, int64, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		query.Set("page_size", strconv.Itoa(pageSize))
	}
	path := fmt.Sprintf("/api/v1/datasets/%s/documents/%s/chunks?%s",
		url.PathEscape(datasetID), url.PathEscape(documentID), query.Encode())
	var payload envelope
	if err := c.getJSON(ctx, userID, path, &payload); err != nil {
		return nil, 0, err
	}
	var listed chunkListData
	if len(payload.Data) == 0 {
		return nil, 0, nil
	}
	if err := json.Unmarshal(payload.Data, &listed); err != nil {
		return nil, 0, err
	}
	return listed.Chunks, listed.Total, nil
}

func (c *Client) GetChunk(ctx context.Context, userID, datasetID, documentID, chunkID string) (map[string]interface{}, error) {
	query := url.Values{}
	query.Set("id", chunkID)
	query.Set("page", "1")
	query.Set("page_size", "1")
	path := fmt.Sprintf("/api/v1/datasets/%s/documents/%s/chunks?%s",
		url.PathEscape(datasetID), url.PathEscape(documentID), query.Encode())
	var payload envelope
	if err := c.getJSON(ctx, userID, path, &payload); err != nil {
		return nil, normalizeChunkNotFoundError(err)
	}
	if len(payload.Data) == 0 {
		return nil, chunkNotFoundError()
	}
	var listed chunkListData
	if err := json.Unmarshal(payload.Data, &listed); err == nil && len(listed.Chunks) > 0 {
		return listed.Chunks[0], nil
	}
	chunk, err := decodeObject(payload.Data)
	if err != nil || len(chunk) == 0 {
		return nil, chunkNotFoundError()
	}
	id := strings.TrimSpace(fmt.Sprint(chunk["id"]))
	if id == "" {
		id = strings.TrimSpace(fmt.Sprint(chunk["chunk_id"]))
	}
	if id != "" && id != chunkID {
		return nil, chunkNotFoundError()
	}
	return chunk, nil
}

func chunkNotFoundError() error {
	return &APIError{Code: http.StatusNotFound, Message: "chunk not found", HTTPStatus: http.StatusNotFound}
}

func normalizeChunkNotFoundError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Message), "chunk not found") {
		return chunkNotFoundError()
	}
	return err
}

func (c *Client) DownloadDocument(ctx context.Context, userID, datasetID, documentID string) (contentType string, body []byte, err error) {
	path := fmt.Sprintf("/api/v1/datasets/%s/documents/%s", url.PathEscape(datasetID), url.PathEscape(documentID))
	req, err := c.newRequest(ctx, userID, http.MethodGet, path, nil)
	if err != nil {
		return "", nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return "", nil, err
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", nil, &APIError{Message: "vendor download failed", HTTPStatus: res.StatusCode}
	}
	contentType = res.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if isJSONContentType(contentType) {
		var payload envelope
		if err := json.Unmarshal(raw, &payload); err == nil && payload.Code != vendorCodeSuccess && (payload.Message != "" || len(payload.Data) > 0) {
			return "", nil, &APIError{Code: payload.Code, Message: payload.Message, HTTPStatus: res.StatusCode}
		}
	}
	return contentType, raw, nil
}

func (c *Client) RetrievalSearch(ctx context.Context, userID string, body []byte) (*RetrievalData, error) {
	var payload envelope
	if err := c.doJSON(ctx, userID, http.MethodPost, "/api/v1/datasets/search", body, &payload); err != nil {
		return nil, err
	}
	var data RetrievalData
	if len(payload.Data) == 0 {
		return &RetrievalData{}, nil
	}
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/system/ping", nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 256))
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != "pong" {
		return fmt.Errorf("vendor ping failed: status=%d body=%q", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) RuntimeStatus(ctx context.Context, userID string) (map[string]interface{}, error) {
	var payload envelope
	if err := c.getJSON(ctx, userID, "/api/v1/system/status", &payload); err != nil {
		return nil, err
	}
	return decodeObject(payload.Data)
}

func (c *Client) getJSON(ctx context.Context, userID, path string, target *envelope) error {
	return c.doJSON(ctx, userID, http.MethodGet, path, nil, target)
}

func (c *Client) doJSON(ctx context.Context, userID, method, path string, body []byte, target *envelope) error {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := c.newRequest(ctx, userID, method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= http.StatusBadRequest && len(raw) == 0 {
		return &APIError{Message: "vendor request failed", HTTPStatus: res.StatusCode}
	}
	if err := json.Unmarshal(raw, target); err != nil {
		if res.StatusCode >= http.StatusBadRequest {
			return &APIError{Message: "vendor request failed", HTTPStatus: res.StatusCode}
		}
		return err
	}
	if res.StatusCode >= http.StatusBadRequest || target.Code != vendorCodeSuccess {
		return &APIError{Code: target.Code, Message: target.Message, HTTPStatus: res.StatusCode}
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, userID, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	userID = strings.TrimSpace(userID)
	req.Header.Set("X-Tenant-Id", userID)
	req.Header.Set("X-User-Id", userID)
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}
	if requestID := requestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	return req, nil
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func decodeDatasetItems(raw json.RawMessage) ([]map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	one, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return []map[string]interface{}{one}, nil
}

func decodeObject(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func decodeUploadDocument(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("vendor upload returned empty data")
	}
	var one map[string]interface{}
	if err := json.Unmarshal(raw, &one); err == nil && one != nil {
		if _, ok := one["id"]; ok {
			return one, nil
		}
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err == nil && len(items) > 0 {
		return items[0], nil
	}
	var wrapped struct {
		Documents []map[string]interface{} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Documents) > 0 {
		return wrapped.Documents[0], nil
	}
	return decodeObject(raw)
}
