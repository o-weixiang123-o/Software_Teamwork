-- +goose Up
ALTER TABLE llm_config_versions
    ALTER COLUMN profile_id DROP NOT NULL,
    ADD COLUMN api_endpoint TEXT,
    ADD COLUMN api_key_encrypted BYTEA,
    ADD COLUMN api_key_last4 TEXT,
    ADD COLUMN token_header TEXT NOT NULL DEFAULT 'Authorization';

CREATE TABLE qa_runtime_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO qa_runtime_settings (key, value)
VALUES (
    'system_prompt',
    'You are a QA agent for a power-industry knowledge system.
When the user asks about facts, standards, policies, domain knowledge, uploaded knowledge-base content, or requests citations, call knowledge__search first. Use the user''s question as the query, set topK to 5 unless the user asks otherwise, and leave knowledgeBaseIds empty to search all indexed knowledge bases.
If knowledge__search is unavailable, use search_knowledge as the fallback retrieval tool. Answer knowledge questions only from retrieved tool results; if retrieval finds no relevant content, say that clearly instead of inventing sources.
Use search_session_attachments only for files bound to the current message. Use document__ tools only for report generation tasks.'
)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE mcp_servers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alias TEXT NOT NULL UNIQUE CHECK (alias ~ '^[a-z0-9_]{2,32}$'),
    display_name TEXT NOT NULL DEFAULT '',
    transport TEXT NOT NULL CHECK (transport IN ('stdio', 'streamable_http')),
    command TEXT,
    args_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(args_json) = 'array'),
    endpoint_url TEXT,
    token_encrypted BYTEA,
    token_last4 TEXT,
    token_header TEXT NOT NULL DEFAULT 'Authorization',
    tool_timeout_seconds INTEGER NOT NULL DEFAULT 30 CHECK (tool_timeout_seconds > 0),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    tool_count INTEGER NOT NULL DEFAULT 0 CHECK (tool_count >= 0),
    last_connected_at TIMESTAMPTZ,
    last_error TEXT,
    created_by_user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (transport = 'stdio' AND command IS NOT NULL AND btrim(command) <> '' AND endpoint_url IS NULL)
        OR
        (transport = 'streamable_http' AND endpoint_url IS NOT NULL AND btrim(endpoint_url) <> '' AND command IS NULL)
    )
);

CREATE INDEX idx_mcp_servers_enabled ON mcp_servers(enabled, sort_order);

-- +goose Down
DROP TABLE IF EXISTS mcp_servers;
DROP TABLE IF EXISTS qa_runtime_settings;
ALTER TABLE llm_config_versions
    DROP COLUMN IF EXISTS token_header,
    DROP COLUMN IF EXISTS api_key_last4,
    DROP COLUMN IF EXISTS api_key_encrypted,
    DROP COLUMN IF EXISTS api_endpoint,
    ALTER COLUMN profile_id SET NOT NULL;
