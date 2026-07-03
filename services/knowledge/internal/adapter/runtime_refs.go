package adapter

import (
	"context"
	"errors"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/vendorclient"
)

type runtimeKnowledgeBaseRef struct {
	ID       string
	TenantID string
}

type runtimeDocumentRef struct {
	ID              string
	KnowledgeBaseID string
	TenantID        string
}

func (s *Server) resolveKnowledgeBaseRuntimeRef(ctx context.Context, knowledgeBaseID, fallbackTenantID string) (runtimeKnowledgeBaseRef, error) {
	kbID := strings.TrimSpace(knowledgeBaseID)
	if kbID == "" {
		return runtimeKnowledgeBaseRef{}, service.ValidationError("request validation failed", map[string]string{"knowledgeBaseId": "is required"})
	}
	ref := runtimeKnowledgeBaseRef{
		ID:       kbID,
		TenantID: strings.TrimSpace(fallbackTenantID),
	}
	if s.knowledgeBases == nil {
		return ref, nil
	}
	items, err := s.knowledgeBases.ListRuntimeKnowledgeBases(ctx, []string{kbID})
	if err != nil {
		return runtimeKnowledgeBaseRef{}, service.DependencyError("knowledge base catalog unavailable", err)
	}
	if len(items) == 0 {
		return runtimeKnowledgeBaseRef{}, service.NotFoundError("knowledge base not found")
	}
	if tenantID := strings.TrimSpace(items[0].TenantID); tenantID != "" {
		ref.TenantID = tenantID
	}
	return ref, nil
}

func (s *Server) resolveDocumentRuntimeRef(ctx context.Context, documentID, knowledgeBaseID, fallbackTenantID string) (runtimeDocumentRef, error) {
	docID := strings.TrimSpace(documentID)
	if docID == "" {
		return runtimeDocumentRef{}, service.ValidationError("request validation failed", map[string]string{"documentId": "is required"})
	}
	if kbID := strings.TrimSpace(knowledgeBaseID); kbID != "" {
		kbRef, err := s.resolveKnowledgeBaseRuntimeRef(ctx, kbID, fallbackTenantID)
		if err != nil {
			return runtimeDocumentRef{}, err
		}
		return runtimeDocumentRef{
			ID:              docID,
			KnowledgeBaseID: kbRef.ID,
			TenantID:        kbRef.TenantID,
		}, nil
	}
	if s.runtimeDocs == nil {
		return runtimeDocumentRef{}, service.ValidationError("request validation failed", map[string]string{"knowledgeBaseId": "is required"})
	}
	doc, err := s.runtimeDocs.GetRuntimeDocument(ctx, docID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			return runtimeDocumentRef{}, service.NotFoundError("document not found")
		}
		return runtimeDocumentRef{}, service.DependencyError("document catalog unavailable", err)
	}
	if strings.TrimSpace(doc.KnowledgeBaseID) == "" || strings.TrimSpace(doc.TenantID) == "" {
		return runtimeDocumentRef{}, service.NotFoundError("document not found")
	}
	return runtimeDocumentRef{
		ID:              strings.TrimSpace(doc.ID),
		KnowledgeBaseID: strings.TrimSpace(doc.KnowledgeBaseID),
		TenantID:        strings.TrimSpace(doc.TenantID),
	}, nil
}

func (s *Server) listRuntimeDocuments(ctx context.Context) ([]service.RuntimeDocument, error) {
	if s.runtimeDocs == nil {
		return nil, service.ValidationError("request validation failed", map[string]string{"documentId": "is required"})
	}
	docs, err := s.runtimeDocs.ListRuntimeDocuments(ctx, nil)
	if err != nil {
		return nil, service.DependencyError("document catalog unavailable", err)
	}
	return docs, nil
}

func isVendorNotFound(err error) bool {
	var apiErr *vendorclient.APIError
	if errors.As(err, &apiErr) {
		if apiErr.MatchesHTTPStatus(404) || apiErr.Code == 404 {
			return true
		}
		return strings.Contains(strings.ToLower(apiErr.Message), "not found")
	}
	appErr, ok := service.Classify(err)
	return ok && appErr.Code == service.CodeNotFound
}
