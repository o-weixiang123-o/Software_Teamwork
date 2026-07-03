-- +goose Up
CREATE TABLE knowledge_bases (
  id text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  doc_type text NOT NULL DEFAULT 'GENERAL',
  chunk_strategy jsonb NOT NULL DEFAULT '{}'::jsonb,
  retrieval_strategy jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE TABLE knowledge_documents (
  id text PRIMARY KEY,
  knowledge_base_id text NOT NULL REFERENCES knowledge_bases(id),
  file_ref text,
  name text NOT NULL,
  content_type text,
  size_bytes bigint,
  status text NOT NULL CHECK (status IN ('uploaded', 'parsing', 'chunking', 'embedding', 'ready', 'failed')),
  error_code text,
  error_message text,
  tags jsonb NOT NULL DEFAULT '[]'::jsonb,
  parser_backend text,
  current_job_id text,
  created_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE TABLE processing_jobs (
  id text PRIMARY KEY,
  knowledge_base_id text NOT NULL REFERENCES knowledge_bases(id),
  document_id text REFERENCES knowledge_documents(id),
  job_type text NOT NULL,
  status text NOT NULL,
  current_stage text,
  progress_percent integer NOT NULL DEFAULT 0 CHECK (progress_percent >= 0 AND progress_percent <= 100),
  message text,
  error_code text,
  error_message text,
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE document_chunks (
  id text PRIMARY KEY,
  knowledge_base_id text NOT NULL REFERENCES knowledge_bases(id),
  document_id text NOT NULL REFERENCES knowledge_documents(id),
  chunk_index integer NOT NULL CHECK (chunk_index >= 0),
  section_path text,
  content text NOT NULL DEFAULT '',
  token_count integer CHECK (token_count IS NULL OR token_count >= 0),
  chunk_type text,
  embedding_provider text,
  embedding_model text,
  embedding_dimension integer CHECK (embedding_dimension IS NULL OR embedding_dimension > 0),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT uniq_document_chunks_document_id_chunk_index UNIQUE (document_id, chunk_index)
);

CREATE INDEX idx_knowledge_bases_created_by_created_at ON knowledge_bases(created_by, created_at DESC);
CREATE INDEX idx_knowledge_bases_deleted_at ON knowledge_bases(deleted_at);
CREATE INDEX idx_knowledge_bases_doc_type ON knowledge_bases(doc_type) WHERE deleted_at IS NULL;

CREATE INDEX idx_knowledge_documents_kb_created_at ON knowledge_documents(knowledge_base_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_knowledge_documents_created_by_created_at ON knowledge_documents(created_by, created_at DESC);
CREATE INDEX idx_knowledge_documents_status ON knowledge_documents(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_knowledge_documents_deleted_at ON knowledge_documents(deleted_at);
CREATE INDEX idx_knowledge_documents_current_job_id ON knowledge_documents(current_job_id) WHERE current_job_id IS NOT NULL;
CREATE INDEX idx_knowledge_documents_file_ref ON knowledge_documents(file_ref) WHERE file_ref IS NOT NULL;

CREATE INDEX idx_processing_jobs_kb_created_at ON processing_jobs(knowledge_base_id, created_at DESC);
CREATE INDEX idx_processing_jobs_document_id ON processing_jobs(document_id) WHERE document_id IS NOT NULL;
CREATE INDEX idx_processing_jobs_status_created_at ON processing_jobs(status, created_at);

CREATE INDEX idx_document_chunks_document_id_chunk_index ON document_chunks(document_id, chunk_index);
CREATE INDEX idx_document_chunks_knowledge_base_id ON document_chunks(knowledge_base_id);

-- +goose Down
DROP TABLE IF EXISTS document_chunks;
DROP TABLE IF EXISTS processing_jobs;
DROP TABLE IF EXISTS knowledge_documents;
DROP TABLE IF EXISTS knowledge_bases;
