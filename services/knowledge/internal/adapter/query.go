package adapter

import (
	"context"
	"sort"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/vendorclient"
)

type knowledgeQueryTarget struct {
	ID          string
	EmbeddingID string
}

type retrievalGroup struct {
	embeddingID string
	ids         []string
}

func (s *Server) runKnowledgeQuery(ctx context.Context, runtimeScopeID string, body knowledgeQueryRequest, opts retrievalBuildOptions) (*vendorclient.RetrievalData, error) {
	if strings.TrimSpace(body.Query) == "" {
		return nil, service.ValidationError("request validation failed", map[string]string{"query": "is required"})
	}

	targets, err := s.resolveKnowledgeQueryTargets(ctx, runtimeScopeID, body.KnowledgeBaseIDs)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return &vendorclient.RetrievalData{}, nil
	}

	merged := &vendorclient.RetrievalData{}
	for _, group := range retrievalGroups(targets) {
		groupBody := body
		groupBody.KnowledgeBaseIDs = group.ids
		payload, err := buildRetrievalBody(groupBody, opts)
		if err != nil {
			return nil, err
		}
		data, err := s.vendor.RetrievalSearch(ctx, runtimeScopeID, payload)
		if err != nil {
			return nil, mapVendorError(err)
		}
		mergeRetrievalData(merged, data)
	}
	sortAndLimitRetrievalData(merged, body)
	return merged, nil
}

func (s *Server) resolveKnowledgeQueryTargets(ctx context.Context, runtimeScopeID string, rawIDs []string) ([]knowledgeQueryTarget, error) {
	requestedIDs := normalizeKnowledgeQueryIDs(rawIDs)
	if s.knowledgeBases != nil {
		items, err := s.knowledgeBases.ListRuntimeKnowledgeBases(ctx, requestedIDs)
		if err != nil {
			return nil, service.DependencyError("knowledge base catalog unavailable", err)
		}
		return catalogQueryTargets(requestedIDs, items)
	}
	if len(requestedIDs) > 0 {
		targets := make([]knowledgeQueryTarget, 0, len(requestedIDs))
		for _, id := range requestedIDs {
			targets = append(targets, knowledgeQueryTarget{ID: id})
		}
		return targets, nil
	}
	return s.vendorQueryTargets(ctx, runtimeScopeID)
}

func catalogQueryTargets(requestedIDs []string, items []service.RuntimeKnowledgeBase) ([]knowledgeQueryTarget, error) {
	requested := map[string]struct{}{}
	for _, id := range requestedIDs {
		requested[id] = struct{}{}
	}
	seen := map[string]struct{}{}
	targets := make([]knowledgeQueryTarget, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if len(requestedIDs) == 0 && item.ChunkCount <= 0 {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, knowledgeQueryTarget{
			ID:          id,
			EmbeddingID: strings.TrimSpace(item.EmbeddingID),
		})
	}
	if len(requestedIDs) > 0 {
		for _, id := range requestedIDs {
			if _, ok := seen[id]; !ok {
				return nil, service.NotFoundError("knowledge base not found")
			}
		}
	}
	return targets, nil
}

func (s *Server) vendorQueryTargets(ctx context.Context, runtimeScopeID string) ([]knowledgeQueryTarget, error) {
	const pageSize = 100
	targets := []knowledgeQueryTarget{}
	for page := 1; ; page++ {
		items, total, err := s.vendor.ListDatasets(ctx, runtimeScopeID, page, pageSize)
		if err != nil {
			return nil, mapVendorError(err)
		}
		for _, item := range items {
			id := stringField(item, "id", "kb_id", "dataset_id")
			if id == "" || int64Field(item, "chunk_num", "chunk_count") <= 0 {
				continue
			}
			targets = append(targets, knowledgeQueryTarget{
				ID:          id,
				EmbeddingID: stringField(item, "embd_id", "embedding_id"),
			})
		}
		if len(items) == 0 || int64(page*pageSize) >= total {
			break
		}
	}
	return targets, nil
}

func retrievalGroups(targets []knowledgeQueryTarget) []retrievalGroup {
	groupMap := map[string]*retrievalGroup{}
	for _, target := range targets {
		key := target.EmbeddingID
		group := groupMap[key]
		if group == nil {
			group = &retrievalGroup{embeddingID: target.EmbeddingID}
			groupMap[key] = group
		}
		group.ids = append(group.ids, target.ID)
	}
	groups := make([]retrievalGroup, 0, len(groupMap))
	for _, group := range groupMap {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].embeddingID < groups[j].embeddingID
	})
	return groups
}

func mergeRetrievalData(dst *vendorclient.RetrievalData, src *vendorclient.RetrievalData) {
	if src == nil {
		return
	}
	dst.Chunks = append(dst.Chunks, src.Chunks...)
	dst.DocAggs = append(dst.DocAggs, src.DocAggs...)
	if src.Total > 0 {
		dst.Total += src.Total
	} else {
		dst.Total += int64(len(src.Chunks))
	}
}

func sortAndLimitRetrievalData(data *vendorclient.RetrievalData, req knowledgeQueryRequest) {
	if data == nil || len(data.Chunks) == 0 {
		return
	}
	sort.SliceStable(data.Chunks, func(i, j int) bool {
		left := retrievalChunkScore(data.Chunks[i])
		right := retrievalChunkScore(data.Chunks[j])
		if left == right {
			return stringField(data.Chunks[i], "chunk_id", "id") < stringField(data.Chunks[j], "chunk_id", "id")
		}
		return left > right
	})
	limit := req.TopK
	if limit <= 0 {
		limit = 10
	}
	if req.RerankTopN != nil && *req.RerankTopN > 0 && *req.RerankTopN < limit {
		limit = *req.RerankTopN
	}
	if limit > 0 && len(data.Chunks) > limit {
		data.Chunks = data.Chunks[:limit]
	}
}

func retrievalChunkScore(raw map[string]interface{}) float64 {
	return floatField(raw, "similarity", "score", "vector_similarity")
}

func normalizeKnowledgeQueryIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
