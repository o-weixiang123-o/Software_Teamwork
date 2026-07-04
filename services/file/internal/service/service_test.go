package service_test

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/platform/storage"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/service"
)

func TestCreateFileComputesChecksumAndStoresContent(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore())
	created, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "policy.txt",
		ContentType: "text/plain",
		SizeBytes:   int64(len("content")),
		Content:     strings.NewReader("content"),
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if created.ID != "file_1" {
		t.Fatalf("file id = %q", created.ID)
	}
	if created.Filename != "policy.txt" || created.ContentType != "text/plain" || created.SizeBytes != int64(len("content")) {
		t.Fatalf("file metadata = %+v", created)
	}
	if created.ChecksumSHA256 != "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73" {
		t.Fatalf("checksum = %q", created.ChecksumSHA256)
	}
	if created.StorageObjectKey == "" {
		t.Fatal("storage object key is empty")
	}

	content, err := files.GetFileContent(context.Background(), internalContext(), created.ID)
	if err != nil {
		t.Fatalf("GetFileContent() error = %v", err)
	}
	defer content.Body.Close()
	body, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "content" {
		t.Fatalf("content = %q", string(body))
	}
}

func TestCreateFileRejectsChecksumMismatch(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore())
	_, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:       "policy.pdf",
		ContentType:    "application/pdf",
		SizeBytes:      int64(len("content")),
		ChecksumSHA256: strings.Repeat("0", 64),
		Content:        strings.NewReader("content"),
	})
	if !hasCode(err, service.CodeValidation) {
		t.Fatalf("CreateFile() error = %v, want validation_error", err)
	}
}

func TestCreateFileStreamsThroughObjectStore(t *testing.T) {
	putStarted := false
	content := strings.Repeat("a", 2048)
	store := &streamingProbeStore{putStarted: &putStarted}
	files := newTestService(t, store)

	created, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "large.txt",
		ContentType: "text/plain",
		SizeBytes:   int64(len(content)),
		Content: &streamingGuardReader{
			content:      content,
			maxBeforePut: 512,
			putStarted:   &putStarted,
		},
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if store.written != int64(len(content)) {
		t.Fatalf("store wrote %d bytes, want %d", store.written, len(content))
	}
	if created.SizeBytes != int64(len(content)) || created.ChecksumSHA256 == "" {
		t.Fatalf("file metadata = %+v", created)
	}
}

func TestCreateFileUsesSniffedContentType(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore(), service.WithAllowedContentTypes([]string{"text/plain"}))
	created, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "policy.pdf",
		ContentType: "application/pdf",
		SizeBytes:   int64(len("plain text")),
		Content:     strings.NewReader("plain text"),
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if created.ContentType != "text/plain" {
		t.Fatalf("ContentType = %q", created.ContentType)
	}
}

func TestCreateFileRejectsDisallowedContentTypeBeforeStorage(t *testing.T) {
	store := &streamingProbeStore{}
	files := newTestService(t, store, service.WithAllowedContentTypes([]string{"application/pdf"}))
	_, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "notes.txt",
		ContentType: "text/plain",
		SizeBytes:   int64(len("plain text")),
		Content:     strings.NewReader("plain text"),
	})
	if !hasCode(err, service.CodeValidation) {
		t.Fatalf("CreateFile() error = %v, want validation_error", err)
	}
	if store.putCalled {
		t.Fatal("object store was called for disallowed content type")
	}
}

func TestCreateFileAllowsOctetStreamFallbackWhenConfigured(t *testing.T) {
	content := "\x00\x01\x02\x03"
	files := newTestService(t, storage.NewMemoryStore(), service.WithAllowedContentTypes([]string{"application/octet-stream"}))
	created, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "blob.bin",
		ContentType: "image/png",
		SizeBytes:   int64(len(content)),
		Content:     strings.NewReader(content),
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if created.ContentType != "application/octet-stream" {
		t.Fatalf("ContentType = %q", created.ContentType)
	}
}

func TestDeleteFileHidesMetadataAndContent(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore())
	created := createTestFile(t, files)

	if err := files.DeleteFile(context.Background(), internalContext(), created.ID); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if _, err := files.GetFile(context.Background(), internalContext(), created.ID); !hasCode(err, service.CodeNotFound) {
		t.Fatalf("GetFile() error = %v, want not_found", err)
	}
	if _, err := files.GetFileContent(context.Background(), internalContext(), created.ID); !hasCode(err, service.CodeNotFound) {
		t.Fatalf("GetFileContent() error = %v, want not_found", err)
	}
}

func TestDeleteFileIsIdempotentAfterPurge(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore())
	created := createTestFile(t, files)

	if err := files.DeleteFile(context.Background(), internalContext(), created.ID); err != nil {
		t.Fatalf("DeleteFile() first error = %v", err)
	}
	if err := files.DeleteFile(context.Background(), internalContext(), created.ID); err != nil {
		t.Fatalf("DeleteFile() second error = %v", err)
	}
}

func TestDeleteFileCanRetryAfterStorageFailure(t *testing.T) {
	store := storage.NewMemoryStore()
	files := newTestService(t, &failOnceDeleteStore{ObjectStore: store})
	created := createTestFile(t, files)

	err := files.DeleteFile(context.Background(), internalContext(), created.ID)
	if !hasCode(err, service.CodeDependency) {
		t.Fatalf("DeleteFile() first error = %v, want dependency_error", err)
	}
	if _, err := files.GetFile(context.Background(), internalContext(), created.ID); !hasCode(err, service.CodeNotFound) {
		t.Fatalf("GetFile() after failed purge = %v, want not_found", err)
	}
	if err := files.DeleteFile(context.Background(), internalContext(), created.ID); err != nil {
		t.Fatalf("DeleteFile() retry error = %v", err)
	}
}

func TestCreateFileRequiresInternalCaller(t *testing.T) {
	files := newTestService(t, storage.NewMemoryStore())
	_, err := files.CreateFile(context.Background(), service.RequestContext{}, service.CreateFileInput{
		FileName:    "policy.pdf",
		ContentType: "application/pdf",
		SizeBytes:   int64(len("content")),
		Content:     strings.NewReader("content"),
	})
	if !hasCode(err, service.CodeUnauthorized) {
		t.Fatalf("CreateFile() error = %v, want unauthorized", err)
	}
}

func newTestService(t *testing.T, store service.ObjectStore, opts ...service.Option) *service.Service {
	t.Helper()
	repo := repository.NewMemoryRepository()
	counter := 0
	options := []service.Option{service.WithIDGenerator(func(prefix string) (string, error) {
		counter++
		return prefix + "_" + strconv.Itoa(counter), nil
	})}
	options = append(options, opts...)
	return service.New(repo, store, options...)
}

func createTestFile(t *testing.T, files *service.Service) service.FileObject {
	t.Helper()
	created, err := files.CreateFile(context.Background(), internalContext(), service.CreateFileInput{
		FileName:    "policy.pdf",
		ContentType: "application/pdf",
		SizeBytes:   int64(len("content")),
		Content:     strings.NewReader("content"),
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	return created
}

func internalContext() service.RequestContext {
	return service.RequestContext{
		RequestID:     "req_test",
		CallerService: "knowledge",
		ServiceToken:  "test-token",
	}
}

func hasCode(err error, code service.Code) bool {
	var appErr *service.AppError
	return errors.As(err, &appErr) && appErr.Code == code
}

type failOnceDeleteStore struct {
	service.ObjectStore
	failed bool
}

func (s *failOnceDeleteStore) Delete(ctx context.Context, key string) error {
	if !s.failed {
		s.failed = true
		return errors.New("storage backend unavailable with object key files/secret")
	}
	return s.ObjectStore.Delete(ctx, key)
}

type streamingProbeStore struct {
	putStarted *bool
	putCalled  bool
	written    int64
}

func (s *streamingProbeStore) Put(ctx context.Context, key string, body io.Reader, contentType string, sizeBytes int64) error {
	s.putCalled = true
	if s.putStarted != nil {
		*s.putStarted = true
	}
	written, err := io.Copy(io.Discard, body)
	if err != nil {
		return err
	}
	if sizeBytes >= 0 && written != sizeBytes {
		return service.ErrConflict
	}
	s.written = written
	return nil
}

func (s *streamingProbeStore) Get(ctx context.Context, key string) (service.StoredObject, error) {
	return service.StoredObject{}, service.ErrNotFound
}

func (s *streamingProbeStore) Delete(ctx context.Context, key string) error {
	return nil
}

type streamingGuardReader struct {
	content      string
	offset       int
	maxBeforePut int
	putStarted   *bool
}

func (r *streamingGuardReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.content) {
		return 0, io.EOF
	}
	limit := len(r.content) - r.offset
	if limit > len(p) {
		limit = len(p)
	}
	if r.putStarted != nil && !*r.putStarted {
		remainingBeforePut := r.maxBeforePut - r.offset
		if remainingBeforePut <= 0 {
			return 0, errors.New("service read beyond sniff prefix before object store put")
		}
		if limit > remainingBeforePut {
			limit = remainingBeforePut
		}
	}
	copy(p, r.content[r.offset:r.offset+limit])
	r.offset += limit
	return limit, nil
}
