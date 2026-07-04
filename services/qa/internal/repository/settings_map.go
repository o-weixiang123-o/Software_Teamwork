package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/repository/sqlc"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

func mcpServerFromListRow(row sqlc.ListMCPServersRow) (service.MCPServerRecord, error) {
	return mcpServerFromFields(
		row.ID, row.Alias, row.DisplayName, row.Transport, row.Command, row.ArgsJson,
		row.EndpointUrl, row.TokenEncrypted, row.TokenLast4, row.TokenHeader,
		row.ToolTimeoutSeconds, row.Enabled, row.SortOrder, row.ToolCount,
		row.LastConnectedAt, row.LastError, row.CreatedByUserID, row.CreatedAt, row.UpdatedAt,
	)
}

func mcpServerFromGetRow(row sqlc.GetMCPServerRow) (service.MCPServerRecord, error) {
	return mcpServerFromFields(
		row.ID, row.Alias, row.DisplayName, row.Transport, row.Command, row.ArgsJson,
		row.EndpointUrl, row.TokenEncrypted, row.TokenLast4, row.TokenHeader,
		row.ToolTimeoutSeconds, row.Enabled, row.SortOrder, row.ToolCount,
		row.LastConnectedAt, row.LastError, row.CreatedByUserID, row.CreatedAt, row.UpdatedAt,
	)
}

func mcpServerFromFields(
	id, alias, displayName, transport, command string,
	argsJSON []byte,
	endpointURL string,
	tokenEncrypted []byte,
	tokenLast4, tokenHeader string,
	toolTimeoutSeconds int32,
	enabled bool,
	sortOrder, toolCount int32,
	lastConnectedAt pgtype.Timestamptz,
	lastError, createdByUserID string,
	createdAt, updatedAt pgtype.Timestamptz,
) (service.MCPServerRecord, error) {
	var args []string
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return service.MCPServerRecord{}, fmt.Errorf("decode MCP server arguments: %w", err)
	}
	server := service.MCPServerRecord{
		ID:                 id,
		Alias:              alias,
		DisplayName:        displayName,
		Transport:          transport,
		Command:            command,
		Args:               args,
		EndpointURL:        endpointURL,
		TokenEncrypted:     tokenEncrypted,
		TokenLast4:         tokenLast4,
		TokenHeader:        tokenHeader,
		ToolTimeoutSeconds: int(toolTimeoutSeconds),
		Enabled:            enabled,
		SortOrder:          int(sortOrder),
		ToolCount:          int(toolCount),
		LastError:          lastError,
		CreatedByUserID:    createdByUserID,
	}
	if lastConnectedAt.Valid {
		value := lastConnectedAt.Time
		server.LastConnectedAt = &value
	}
	if createdAt.Valid {
		server.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		server.UpdatedAt = updatedAt.Time
	}
	return server, nil
}

func storedLLMFromRow(row sqlc.GetActiveLLMConfigRow) (service.StoredLLMConfig, error) {
	temperature, err := numericToFloat64(row.Temperature)
	if err != nil {
		return service.StoredLLMConfig{}, fmt.Errorf("decode LLM temperature: %w", err)
	}
	return service.StoredLLMConfig{
		ID:              row.ID,
		Provider:        row.Provider,
		ProfileID:       row.ProfileID,
		APIEndpoint:     row.ApiEndpoint,
		APIKeyEncrypted: row.ApiKeyEncrypted,
		APIKeyLast4:     row.ApiKeyLast4,
		TokenHeader:     row.TokenHeader,
		Model:           row.ModelName,
		TimeoutSeconds:  int(row.TimeoutSeconds),
		Temperature:     temperature,
		MaxTokens:       int(row.MaxTokens),
	}, nil
}

func retrievalSettingsFromRow(row sqlc.GetActiveQAConfigRow) (service.RetrievalSettings, error) {
	threshold, err := numericToFloat64(row.SimilarityThreshold)
	if err != nil {
		return service.RetrievalSettings{}, fmt.Errorf("decode QA similarity threshold: %w", err)
	}
	settings := service.RetrievalSettings{
		TopK:            int(row.TopK),
		ScoreThreshold:  threshold,
		EnableRerank:    row.UseRerank,
		RerankThreshold: row.RerankThreshold,
		RerankTopN:      int(row.RerankTopN),
	}
	return settings.WithScoreThresholdConfigured(), nil
}

func insertQAConfigVersionParams(settings service.RetrievalSettings, version int32, userID string) (sqlc.InsertQAConfigVersionParams, error) {
	similarity, err := floatToNumeric(settings.ScoreThreshold)
	if err != nil {
		return sqlc.InsertQAConfigVersionParams{}, err
	}
	rerankThreshold, err := floatToNumeric(settings.RerankThreshold)
	if err != nil {
		return sqlc.InsertQAConfigVersionParams{}, err
	}
	return sqlc.InsertQAConfigVersionParams{
		VersionNo:           version,
		TopK:                int32(settings.TopK),
		SimilarityThreshold: similarity,
		UseRerank:           settings.EnableRerank,
		RerankThreshold:     rerankThreshold,
		RerankTopN:          pgtype.Int4{Int32: int32(settings.RerankTopN), Valid: true},
		CreatedByUserID:     userID,
	}, nil
}

func insertMCPServerParams(server service.MCPServerRecord) (sqlc.InsertMCPServerParams, error) {
	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		return sqlc.InsertMCPServerParams{}, fmt.Errorf("encode MCP server arguments: %w", err)
	}
	return sqlc.InsertMCPServerParams{
		Alias:              server.Alias,
		DisplayName:        server.DisplayName,
		Transport:          server.Transport,
		Command:            nullableInterface(server.Command),
		ArgsJson:           argsJSON,
		EndpointUrl:        nullableInterface(server.EndpointURL),
		TokenEncrypted:     server.TokenEncrypted,
		TokenLast4:         nullableInterface(server.TokenLast4),
		TokenHeader:        server.TokenHeader,
		ToolTimeoutSeconds: int32(server.ToolTimeoutSeconds),
		Enabled:            server.Enabled,
		SortOrder:          int32(server.SortOrder),
		CreatedByUserID:    server.CreatedByUserID,
	}, nil
}

func updateMCPServerParams(server service.MCPServerRecord) (sqlc.UpdateMCPServerParams, error) {
	id, err := uuidFromString(server.ID)
	if err != nil {
		return sqlc.UpdateMCPServerParams{}, err
	}
	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		return sqlc.UpdateMCPServerParams{}, fmt.Errorf("encode MCP server arguments: %w", err)
	}
	return sqlc.UpdateMCPServerParams{
		DisplayName:        server.DisplayName,
		Transport:          server.Transport,
		Command:            nullableInterface(server.Command),
		ArgsJson:           argsJSON,
		EndpointUrl:        nullableInterface(server.EndpointURL),
		TokenEncrypted:     server.TokenEncrypted,
		TokenLast4:         nullableInterface(server.TokenLast4),
		TokenHeader:        server.TokenHeader,
		ToolTimeoutSeconds: int32(server.ToolTimeoutSeconds),
		Enabled:            server.Enabled,
		SortOrder:          int32(server.SortOrder),
		ID:                 id,
	}, nil
}

func uuidFromString(id string) (pgtype.UUID, error) {
	var value pgtype.UUID
	if err := value.Scan(id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid %q: %w", id, err)
	}
	return value, nil
}

func timestamptzFromTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func numericToFloat64(value pgtype.Numeric) (float64, error) {
	f, err := value.Float64Value()
	if err != nil {
		return 0, err
	}
	if !f.Valid {
		return 0, nil
	}
	return f.Float64, nil
}

func floatToNumeric(value float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	if err := numeric.Scan(fmt.Sprintf("%g", value)); err != nil {
		return pgtype.Numeric{}, err
	}
	return numeric, nil
}

func nullableInterface(value string) any {
	if value == "" {
		return nil
	}
	return value
}
