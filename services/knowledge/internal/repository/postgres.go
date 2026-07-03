package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) ListRuntimeKnowledgeBases(ctx context.Context, ids []string) ([]service.RuntimeKnowledgeBase, error) {
	args := []any{"1"}
	filter := ""
	if len(ids) > 0 {
		filter = " AND id = ANY($2::text[])"
		args = append(args, ids)
	}
	rows, err := r.pool.Query(ctx, `SELECT id, COALESCE(embd_id,''), COALESCE(chunk_num,0) FROM knowledgebase WHERE status=$1`+filter+` ORDER BY update_time DESC, id`, args...)
	if err != nil {
		return nil, wrapPostgresError("list runtime knowledge bases", err)
	}
	defer rows.Close()

	items := []service.RuntimeKnowledgeBase{}
	for rows.Next() {
		var item service.RuntimeKnowledgeBase
		if err := rows.Scan(&item.ID, &item.EmbeddingID, &item.ChunkCount); err != nil {
			return nil, wrapPostgresError("scan runtime knowledge base", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPostgresError("list runtime knowledge bases", err)
	}
	return items, nil
}

func (r *PostgresRepository) GetRuntimeDocument(ctx context.Context, id string) (service.RuntimeDocument, error) {
	const query = `
		SELECT d.id, d.kb_id, COALESCE(d.chunk_num,0)
		FROM document d
		JOIN knowledgebase k ON k.id=d.kb_id
		WHERE d.id=$1
			AND COALESCE(d.status,'1')='1'
			AND k.status='1'
	`
	var item service.RuntimeDocument
	if err := r.pool.QueryRow(ctx, query, id).Scan(&item.ID, &item.KnowledgeBaseID, &item.ChunkCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.RuntimeDocument{}, service.ErrNotFound
		}
		return service.RuntimeDocument{}, wrapPostgresError("get runtime document", err)
	}
	return item, nil
}

func (r *PostgresRepository) ListRuntimeDocuments(ctx context.Context, ids []string) ([]service.RuntimeDocument, error) {
	args := []any{}
	filter := ""
	if len(ids) > 0 {
		filter = " AND d.id = ANY($1::text[])"
		args = append(args, ids)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.kb_id, COALESCE(d.chunk_num,0)
		FROM document d
		JOIN knowledgebase k ON k.id=d.kb_id
		WHERE COALESCE(d.status,'1')='1'
			AND k.status='1'
			AND COALESCE(d.chunk_num,0)>0
		`+filter+`
		ORDER BY d.update_time DESC, d.id
	`, args...)
	if err != nil {
		return nil, wrapPostgresError("list runtime documents", err)
	}
	defer rows.Close()

	items := []service.RuntimeDocument{}
	for rows.Next() {
		var item service.RuntimeDocument
		if err := rows.Scan(&item.ID, &item.KnowledgeBaseID, &item.ChunkCount); err != nil {
			return nil, wrapPostgresError("scan runtime document", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPostgresError("list runtime documents", err)
	}
	return items, nil
}

const parserConfigColumns = `id, name, backend, enabled, is_default, concurrency, supported_content_types, endpoint_url, default_parameters, created_at, updated_at, deleted_at`

func (r *PostgresRepository) ListParserConfigs(ctx context.Context, enabled *bool) ([]service.ParserConfig, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+parserConfigColumns+` FROM parser_configs WHERE deleted_at IS NULL AND ($1::boolean IS NULL OR enabled = $1) ORDER BY created_at DESC`, enabled)
	if err != nil {
		return nil, wrapPostgresError("list parser configs", err)
	}
	defer rows.Close()
	items := []service.ParserConfig{}
	for rows.Next() {
		config, err := scanParserConfig(rows)
		if err != nil {
			return nil, wrapPostgresError("scan parser config", err)
		}
		items = append(items, config)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPostgresError("list parser configs", err)
	}
	return items, nil
}

func (r *PostgresRepository) GetParserConfig(ctx context.Context, id string) (service.ParserConfig, error) {
	return r.getParserConfig(ctx, r.pool, id, false)
}

type parserConfigQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *PostgresRepository) getParserConfig(ctx context.Context, q parserConfigQuerier, id string, forUpdate bool) (service.ParserConfig, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	config, err := scanParserConfig(q.QueryRow(ctx, `SELECT `+parserConfigColumns+` FROM parser_configs WHERE id=$1 AND deleted_at IS NULL`+suffix, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ParserConfig{}, service.ErrNotFound
	}
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("get parser config", err)
	}
	return config, nil
}

func (r *PostgresRepository) CreateParserConfig(ctx context.Context, config service.ParserConfig, audit service.ParserConfigAudit) (service.ParserConfig, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("begin parser config create", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if config.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE parser_configs SET is_default=false, updated_at=$1 WHERE is_default AND deleted_at IS NULL`, config.UpdatedAt); err != nil {
			return service.ParserConfig{}, wrapPostgresError("clear parser default", err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO parser_configs (id,name,backend,enabled,is_default,concurrency,supported_content_types,endpoint_url,default_parameters,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, config.ID, config.Name, config.Backend, config.Enabled, config.IsDefault, config.Concurrency, config.SupportedContentTypes, config.EndpointURL, config.DefaultParameters, config.CreatedAt, config.UpdatedAt)
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("create parser config", err)
	}
	if err = insertParserAudit(ctx, tx, audit); err != nil {
		return service.ParserConfig{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return service.ParserConfig{}, wrapPostgresError("commit parser config create", err)
	}
	return config, nil
}

func (r *PostgresRepository) UpdateParserConfig(ctx context.Context, config service.ParserConfig, audit service.ParserConfigAudit) (service.ParserConfig, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("begin parser config update", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = r.getParserConfig(ctx, tx, config.ID, true); err != nil {
		return service.ParserConfig{}, err
	}
	if config.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE parser_configs SET is_default=false, updated_at=$1 WHERE id<>$2 AND is_default AND deleted_at IS NULL`, config.UpdatedAt, config.ID); err != nil {
			return service.ParserConfig{}, wrapPostgresError("clear parser default", err)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE parser_configs SET name=$2,backend=$3,enabled=$4,is_default=$5,concurrency=$6,supported_content_types=$7,endpoint_url=$8,default_parameters=$9,updated_at=$10 WHERE id=$1 AND deleted_at IS NULL`, config.ID, config.Name, config.Backend, config.Enabled, config.IsDefault, config.Concurrency, config.SupportedContentTypes, config.EndpointURL, config.DefaultParameters, config.UpdatedAt)
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("update parser config", err)
	}
	if err = insertParserAudit(ctx, tx, audit); err != nil {
		return service.ParserConfig{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return service.ParserConfig{}, wrapPostgresError("commit parser config update", err)
	}
	return config, nil
}

func (r *PostgresRepository) SoftDeleteParserConfig(ctx context.Context, id string, deletedAt time.Time, audit service.ParserConfigAudit) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return wrapPostgresError("begin parser config delete", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	config, err := r.getParserConfig(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if config.IsDefault {
		return service.ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE parser_configs SET enabled=false,deleted_at=$2,updated_at=$2 WHERE id=$1 AND deleted_at IS NULL`, id, deletedAt)
	if err != nil {
		return wrapPostgresError("delete parser config", err)
	}
	if tag.RowsAffected() == 0 {
		return service.ErrNotFound
	}
	if err = insertParserAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) GetEffectiveParserConfig(ctx context.Context, contentType string) (service.ParserConfig, error) {
	const query = `SELECT ` + parserConfigColumns + `
		FROM parser_configs
		WHERE enabled
			AND deleted_at IS NULL
			AND (
				$1=''
				OR cardinality(supported_content_types)=0
				OR $1=ANY(supported_content_types)
				OR split_part($1,'/',1)||'/*'=ANY(supported_content_types)
			)
		ORDER BY
			CASE
				WHEN $1='' THEN 0
				WHEN $1=ANY(supported_content_types) THEN 0
				WHEN split_part($1,'/',1)||'/*'=ANY(supported_content_types) THEN 1
				WHEN cardinality(supported_content_types)=0 THEN 2
				ELSE 3
			END,
			is_default DESC,
			created_at ASC
		LIMIT 1`
	config, err := scanParserConfig(r.pool.QueryRow(ctx, query, contentType))
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ParserConfig{}, service.ErrNotFound
	}
	if err != nil {
		return service.ParserConfig{}, wrapPostgresError("get effective parser config", err)
	}
	return config, nil
}

func insertParserAudit(ctx context.Context, tx pgx.Tx, audit service.ParserConfigAudit) error {
	_, err := tx.Exec(ctx, `INSERT INTO parser_config_audits (id,parser_config_id,actor_user_id,action,summary,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, audit.ID, audit.ParserConfigID, audit.ActorUserID, audit.Action, audit.Summary, audit.CreatedAt)
	if err != nil {
		return wrapPostgresError("insert parser config audit", err)
	}
	return nil
}

type parserConfigScanner interface{ Scan(...any) error }

func scanParserConfig(row parserConfigScanner) (service.ParserConfig, error) {
	var c service.ParserConfig
	var backend string
	var endpoint pgtype.Text
	var deleted pgtype.Timestamptz
	err := row.Scan(&c.ID, &c.Name, &backend, &c.Enabled, &c.IsDefault, &c.Concurrency, &c.SupportedContentTypes, &endpoint, &c.DefaultParameters, &c.CreatedAt, &c.UpdatedAt, &deleted)
	c.Backend = service.ParserBackend(backend)
	c.EndpointURL = textPtr(endpoint)
	c.DeletedAt = timePtr(deleted)
	return c, err
}

func wrapPostgresError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return service.ErrConflict
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time
	return &timestamp
}
