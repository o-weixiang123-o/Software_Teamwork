-- Local integration contract: scripts/local/dev-up.sh applies this seed after
-- service migrations so the demo admin and cross-service fixtures exist.
\connect auth_system

INSERT INTO auth_users (
    id,
    username,
    display_name,
    email,
    status,
    created_at,
    updated_at
)
VALUES (
    'usr_local_admin',
    'admin',
    'Local Demo Administrator',
    'admin@example.invalid',
    'active',
    now(),
    now()
)
ON CONFLICT (username) WHERE deleted_at IS NULL DO UPDATE
SET display_name = EXCLUDED.display_name,
    email = EXCLUDED.email,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO auth_credentials (
    id,
    user_id,
    credential_type,
    password_hash,
    password_hash_alg,
    password_hash_params_version,
    password_hash_params_json,
    password_changed_at,
    created_at,
    updated_at
)
VALUES (
    'cred_local_admin_password',
    'usr_local_admin',
    'password',
    '$argon2id$v=19$m=65536,t=3,p=2$bG9jYWwtZGVtby1zYWx0IQ$tESTl/LqUlaDlE8hP4+CNLG5go/+X2xvYXBdqk+4eOI',
    'argon2id',
    'argon2id-v1',
    '{"memoryKiB":65536,"iterations":3,"parallelism":2,"saltBytes":16,"keyBytes":32}'::jsonb,
    now(),
    now(),
    now()
)
ON CONFLICT (user_id, credential_type) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    password_hash_alg = EXCLUDED.password_hash_alg,
    password_hash_params_version = EXCLUDED.password_hash_params_version,
    password_hash_params_json = EXCLUDED.password_hash_params_json,
    password_changed_at = now(),
    updated_at = now();

INSERT INTO user_roles (
    id,
    user_id,
    role_id,
    assigned_by,
    assigned_at,
    created_at
)
SELECT
    'urole_local_admin_admin',
    'usr_local_admin',
    r.id,
    'local-seed',
    now(),
    now()
FROM auth_roles r
WHERE r.code = 'admin'
ON CONFLICT (user_id, role_id) DO NOTHING;

INSERT INTO auth_users (
    id,
    username,
    display_name,
    email,
    status,
    created_at,
    updated_at
)
VALUES (
    'usr_local_super_admin',
    'superadmin',
    'Local Demo Super Administrator',
    'superadmin@example.invalid',
    'active',
    now(),
    now()
)
ON CONFLICT (username) WHERE deleted_at IS NULL DO UPDATE
SET display_name = EXCLUDED.display_name,
    email = EXCLUDED.email,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO auth_credentials (
    id,
    user_id,
    credential_type,
    password_hash,
    password_hash_alg,
    password_hash_params_version,
    password_hash_params_json,
    password_changed_at,
    created_at,
    updated_at
)
VALUES (
    'cred_local_super_admin_password',
    'usr_local_super_admin',
    'password',
    '$argon2id$v=19$m=65536,t=3,p=2$bG9jYWwtZGVtby1zYWx0IQ$tESTl/LqUlaDlE8hP4+CNLG5go/+X2xvYXBdqk+4eOI',
    'argon2id',
    'argon2id-v1',
    '{"memoryKiB":65536,"iterations":3,"parallelism":2,"saltBytes":16,"keyBytes":32}'::jsonb,
    now(),
    now(),
    now()
)
ON CONFLICT (user_id, credential_type) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    password_hash_alg = EXCLUDED.password_hash_alg,
    password_hash_params_version = EXCLUDED.password_hash_params_version,
    password_hash_params_json = EXCLUDED.password_hash_params_json,
    password_changed_at = now(),
    updated_at = now();

INSERT INTO user_roles (
    id,
    user_id,
    role_id,
    assigned_by,
    assigned_at,
    created_at
)
SELECT
    'urole_local_super_admin_super_admin',
    'usr_local_super_admin',
    r.id,
    'local-seed',
    now(),
    now()
FROM auth_roles r
WHERE r.code = 'super_admin'
ON CONFLICT (user_id, role_id) DO NOTHING;

\connect knowledge_system

INSERT INTO knowledge_bases (
    id,
    name,
    description,
    doc_type,
    chunk_strategy,
    retrieval_strategy,
    created_by,
    created_at,
    updated_at
)
VALUES (
    'kb_local_demo',
    'Local Demo Knowledge Base',
    'Seed knowledge base for local integration smoke tests.',
    'GENERAL',
    '{"chunkSize":800,"overlap":120}'::jsonb,
    '{"topK":5,"scoreThreshold":0.2}'::jsonb,
    'usr_local_admin',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    doc_type = EXCLUDED.doc_type,
    chunk_strategy = EXCLUDED.chunk_strategy,
    retrieval_strategy = EXCLUDED.retrieval_strategy,
    updated_at = now();

INSERT INTO knowledge_documents (
    id,
    knowledge_base_id,
    file_ref,
    name,
    content_type,
    size_bytes,
    status,
    error_code,
    error_message,
    tags,
    parser_backend,
    current_job_id,
    created_by,
    created_at,
    updated_at
)
VALUES (
    'doc_local_demo_seed',
    'kb_local_demo',
    null,
    'local-demo-grid-inspection.md',
    'text/markdown',
    640,
    'ready',
    null,
    null,
    '["local-demo","seed","grid-inspection"]'::jsonb,
    'local_seed',
    null,
    'usr_local_admin',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET knowledge_base_id = EXCLUDED.knowledge_base_id,
    file_ref = null,
    name = EXCLUDED.name,
    content_type = EXCLUDED.content_type,
    size_bytes = EXCLUDED.size_bytes,
    status = EXCLUDED.status,
    error_code = null,
    error_message = null,
    tags = EXCLUDED.tags,
    parser_backend = EXCLUDED.parser_backend,
    current_job_id = null,
    created_by = EXCLUDED.created_by,
    updated_at = now(),
    deleted_at = null;

INSERT INTO document_chunks (
    id,
    knowledge_base_id,
    document_id,
    chunk_index,
    section_path,
    content,
    token_count,
    chunk_type,
    embedding_provider,
    embedding_model,
    embedding_dimension,
    metadata,
    created_at
)
VALUES (
    'chunk_local_demo_seed_001',
    'kb_local_demo',
    'doc_local_demo_seed',
    0,
    'local-demo/overview',
    'Local demo grid inspection note: verify transformer load, relay status, and coal inventory handoff before the summer peak window.',
    24,
    'text',
    'local_hashing',
    'local_hashing',
    384,
    '{"source":"local_seed","privateContent":false}'::jsonb,
    now()
)
ON CONFLICT (id) DO UPDATE
SET knowledge_base_id = EXCLUDED.knowledge_base_id,
    document_id = EXCLUDED.document_id,
    chunk_index = EXCLUDED.chunk_index,
    section_path = EXCLUDED.section_path,
    content = EXCLUDED.content,
    token_count = EXCLUDED.token_count,
    chunk_type = EXCLUDED.chunk_type,
    embedding_provider = EXCLUDED.embedding_provider,
    embedding_model = EXCLUDED.embedding_model,
    embedding_dimension = EXCLUDED.embedding_dimension,
    metadata = EXCLUDED.metadata;

\connect document_system

INSERT INTO report_types (code, name, description, enabled, updated_at)
VALUES
    ('summer_peak_inspection', '迎峰度夏检查报告', '本地演示：迎峰度夏供电保障与风险检查报告类型。', true, now()),
    ('coal_inventory_audit', '煤库存审计报告', '本地演示：煤场库存账实、煤质计量与保供风险审计报告类型。', true, now())
ON CONFLICT (code) DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled,
    updated_at = now();

UPDATE report_types
SET default_template_id = CASE code
        WHEN 'summer_peak_inspection' THEN '11111111-1111-4111-8111-111111111101'::uuid
        WHEN 'coal_inventory_audit' THEN '11111111-1111-4111-8111-111111111102'::uuid
        ELSE default_template_id
    END,
    updated_at = now()
WHERE code IN ('summer_peak_inspection', 'coal_inventory_audit')
  AND (
      default_template_id IS NULL
      OR default_template_id IN (
          '11111111-1111-4111-8111-111111111101'::uuid,
          '11111111-1111-4111-8111-111111111102'::uuid
      )
  );

INSERT INTO report_materials (
    id,
    material_name,
    material_type,
    category,
    file_ref,
    filename,
    file_size,
    description,
    tags_json,
    enabled,
    created_by,
    created_at,
    updated_at
)
VALUES (
    '22222222-2222-4222-8222-222222222201',
    '本地演示检查记录',
    'text',
    'local-demo',
    null,
    'local-demo-inspection-notes.md',
    0,
    '用于本地联调的安全占位素材，不包含真实文件引用或生产内容。',
    '["本地演示","种子数据","无文件引用"]'::jsonb,
    true,
    'usr_local_admin',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET material_name = EXCLUDED.material_name,
    material_type = EXCLUDED.material_type,
    category = EXCLUDED.category,
    file_ref = null,
    filename = EXCLUDED.filename,
    file_size = EXCLUDED.file_size,
    description = EXCLUDED.description,
    tags_json = EXCLUDED.tags_json,
    enabled = EXCLUDED.enabled,
    created_by = EXCLUDED.created_by,
    updated_at = now(),
    deleted_at = null;

INSERT INTO report_template_materials (
    id,
    template_id,
    material_id,
    usage_type,
    created_at
)
VALUES (
    '22222222-2222-4222-8222-222222222202',
    '11111111-1111-4111-8111-111111111101',
    '22222222-2222-4222-8222-222222222201',
    'reference',
    now()
)
ON CONFLICT (template_id, material_id, usage_type) DO UPDATE
SET id = EXCLUDED.id;

INSERT INTO reports (
    id,
    report_name,
    report_type,
    template_id,
    topic,
    specialty,
    plant_or_business_object,
    report_year,
    status,
    extra_context_json,
    creator_id,
    creator_name,
    source,
    latest_job_id,
    latest_report_file_id,
    generated_at,
    exported_at,
    created_at,
    updated_at,
    deleted_at
)
VALUES (
    '22222222-2222-4222-8222-222222222301',
    '本地演示迎峰度夏检查报告',
    'summer_peak_inspection',
    '11111111-1111-4111-8111-111111111101',
    '本地演示迎峰度夏保供检查',
    '电网运行',
    '本地演示电厂',
    2026,
    'generated',
    '{"seed":"local-demo","privateContent":false,"usesRealProvider":false}'::jsonb,
    'usr_local_admin',
    '本地演示管理员',
    'local_seed',
    null,
    null,
    now(),
    null,
    now(),
    now(),
    null
)
ON CONFLICT (id) DO UPDATE
SET report_name = EXCLUDED.report_name,
    report_type = EXCLUDED.report_type,
    template_id = EXCLUDED.template_id,
    topic = EXCLUDED.topic,
    specialty = EXCLUDED.specialty,
    plant_or_business_object = EXCLUDED.plant_or_business_object,
    report_year = EXCLUDED.report_year,
    status = EXCLUDED.status,
    extra_context_json = EXCLUDED.extra_context_json,
    creator_id = EXCLUDED.creator_id,
    creator_name = EXCLUDED.creator_name,
    source = EXCLUDED.source,
    latest_job_id = null,
    latest_report_file_id = null,
    generated_at = COALESCE(reports.generated_at, now()),
    exported_at = null,
    updated_at = now(),
    deleted_at = null;

INSERT INTO report_outlines (
    id,
    report_id,
    outline_json,
    version,
    source,
    source_job_id,
    is_current,
    manual_edited,
    created_at,
    updated_at
)
VALUES (
    '22222222-2222-4222-8222-222222222401',
    '22222222-2222-4222-8222-222222222301',
    '[
        {"id":"local-overview","title":"Inspection Overview","level":1,"sectionPath":"1"},
        {"id":"local-findings","title":"Key Findings","level":1,"sectionPath":"2"}
    ]'::jsonb,
    1,
    'manual',
    null,
    true,
    false,
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE
SET report_id = EXCLUDED.report_id,
    outline_json = EXCLUDED.outline_json,
    version = EXCLUDED.version,
    source = EXCLUDED.source,
    source_job_id = null,
    is_current = true,
    manual_edited = EXCLUDED.manual_edited,
    updated_at = now();

INSERT INTO report_sections (
    id,
    report_id,
    outline_id,
    parent_id,
    outline_node_id,
    section_path,
    title,
    level,
    sort_order,
    numbering,
    section_type,
    content,
    tables_json,
    images_json,
    generation_status,
    content_source,
    manual_edited,
    version,
    last_job_id,
    generated_at,
    created_at,
    updated_at
)
VALUES
    (
        '22222222-2222-4222-8222-222222222501',
        '22222222-2222-4222-8222-222222222301',
        '22222222-2222-4222-8222-222222222401',
        null,
        'local-overview',
        '1',
        'Inspection Overview',
        1,
        1,
        '1',
        'text',
        'Local demo overview: confirm equipment load, staffing plan, and emergency material readiness before peak demand.',
        '[]'::jsonb,
        '[]'::jsonb,
        'succeeded',
        'manual',
        true,
        1,
        null,
        now(),
        now(),
        now()
    ),
    (
        '22222222-2222-4222-8222-222222222502',
        '22222222-2222-4222-8222-222222222301',
        '22222222-2222-4222-8222-222222222401',
        null,
        'local-findings',
        '2',
        'Key Findings',
        1,
        2,
        '2',
        'text',
        'Local demo finding: keep transformer inspection notes and coal inventory handoff visible for report UI checks.',
        '[]'::jsonb,
        '[]'::jsonb,
        'succeeded',
        'manual',
        true,
        1,
        null,
        now(),
        now(),
        now()
    )
ON CONFLICT (id) DO UPDATE
SET report_id = EXCLUDED.report_id,
    outline_id = EXCLUDED.outline_id,
    parent_id = null,
    outline_node_id = EXCLUDED.outline_node_id,
    section_path = EXCLUDED.section_path,
    title = EXCLUDED.title,
    level = EXCLUDED.level,
    sort_order = EXCLUDED.sort_order,
    numbering = EXCLUDED.numbering,
    section_type = EXCLUDED.section_type,
    content = EXCLUDED.content,
    tables_json = EXCLUDED.tables_json,
    images_json = EXCLUDED.images_json,
    generation_status = EXCLUDED.generation_status,
    content_source = EXCLUDED.content_source,
    manual_edited = EXCLUDED.manual_edited,
    version = EXCLUDED.version,
    last_job_id = null,
    generated_at = COALESCE(report_sections.generated_at, now()),
    updated_at = now();

INSERT INTO report_section_versions (
    id,
    report_id,
    section_id,
    version,
    source,
    content,
    tables_json,
    job_id,
    requirements,
    created_by,
    created_at
)
VALUES
    (
        '22222222-2222-4222-8222-222222222601',
        '22222222-2222-4222-8222-222222222301',
        '22222222-2222-4222-8222-222222222501',
        1,
        'manual',
        'Local demo overview: confirm equipment load, staffing plan, and emergency material readiness before peak demand.',
        '[]'::jsonb,
        null,
        'Seeded local demo section snapshot.',
        'usr_local_admin',
        now()
    ),
    (
        '22222222-2222-4222-8222-222222222602',
        '22222222-2222-4222-8222-222222222301',
        '22222222-2222-4222-8222-222222222502',
        1,
        'manual',
        'Local demo finding: keep transformer inspection notes and coal inventory handoff visible for report UI checks.',
        '[]'::jsonb,
        null,
        'Seeded local demo section snapshot.',
        'usr_local_admin',
        now()
    )
ON CONFLICT (id) DO UPDATE
SET report_id = EXCLUDED.report_id,
    section_id = EXCLUDED.section_id,
    version = EXCLUDED.version,
    source = EXCLUDED.source,
    content = EXCLUDED.content,
    tables_json = EXCLUDED.tables_json,
    job_id = null,
    requirements = EXCLUDED.requirements,
    created_by = EXCLUDED.created_by;

INSERT INTO report_events (
    id,
    report_id,
    job_id,
    event_type,
    message,
    payload_json,
    created_at
)
VALUES (
    '22222222-2222-4222-8222-222222222701',
    '22222222-2222-4222-8222-222222222301',
    null,
    'report.seeded_local',
    'Local demo report seed refreshed.',
    '{"source":"local_seed"}'::jsonb,
    now()
)
ON CONFLICT (id) DO UPDATE
SET report_id = EXCLUDED.report_id,
    job_id = null,
    event_type = EXCLUDED.event_type,
    message = EXCLUDED.message,
    payload_json = EXCLUDED.payload_json;

INSERT INTO report_operation_logs (
    id,
    operator_id,
    operator_name,
    operation_type,
    target_type,
    target_id,
    request_id,
    request_source,
    tool_name,
    parameter_summary_json,
    operation_result,
    error_message,
    metadata_json,
    created_at
)
VALUES (
    '22222222-2222-4222-8222-222222222801',
    'usr_local_admin',
    'Local Demo Administrator',
    'local_seed_refresh',
    'report',
    '22222222-2222-4222-8222-222222222301',
    'req_local_seed_document_demo',
    'local_seed',
    null,
    '{"seed":"001-local-demo-seed.sql"}'::jsonb,
    'succeeded',
    null,
    '{"privateContent":false}'::jsonb,
    now()
)
ON CONFLICT (id) DO UPDATE
SET operator_id = EXCLUDED.operator_id,
    operator_name = EXCLUDED.operator_name,
    operation_type = EXCLUDED.operation_type,
    target_type = EXCLUDED.target_type,
    target_id = EXCLUDED.target_id,
    request_id = EXCLUDED.request_id,
    request_source = EXCLUDED.request_source,
    tool_name = null,
    parameter_summary_json = EXCLUDED.parameter_summary_json,
    operation_result = EXCLUDED.operation_result,
    error_message = null,
    metadata_json = EXCLUDED.metadata_json;

\connect qa_system

INSERT INTO conversations (
    id,
    external_user_id,
    title,
    status,
    created_at,
    updated_at,
    last_message_at,
    deleted_at
)
VALUES (
    '33333333-3333-4333-8333-333333333301',
    'usr_local_admin',
    'Local Demo QA Session',
    'active',
    now(),
    now(),
    now(),
    null
)
ON CONFLICT (id) DO UPDATE
SET external_user_id = EXCLUDED.external_user_id,
    title = EXCLUDED.title,
    status = EXCLUDED.status,
    updated_at = now(),
    last_message_at = now(),
    deleted_at = null;

INSERT INTO messages (
    id,
    conversation_id,
    role,
    sequence_no,
    intent,
    status,
    model_name,
    error_code,
    error_message,
    created_at,
    completed_at
)
VALUES
    (
        '33333333-3333-4333-8333-333333333401',
        '33333333-3333-4333-8333-333333333301',
        'user',
        1,
        'knowledge_qa',
        'completed',
        null,
        null,
        null,
        now(),
        now()
    ),
    (
        '33333333-3333-4333-8333-333333333402',
        '33333333-3333-4333-8333-333333333301',
        'assistant',
        2,
        'knowledge_qa',
        'completed',
        'local-placeholder-chat',
        null,
        null,
        now(),
        now()
    )
ON CONFLICT (id) DO UPDATE
SET conversation_id = EXCLUDED.conversation_id,
    role = EXCLUDED.role,
    sequence_no = EXCLUDED.sequence_no,
    intent = EXCLUDED.intent,
    status = EXCLUDED.status,
    model_name = EXCLUDED.model_name,
    error_code = null,
    error_message = null,
    completed_at = now();

INSERT INTO message_content_blocks (
    id,
    message_id,
    block_order,
    block_type,
    content,
    status,
    provider_block_id,
    provider_metadata,
    created_at,
    updated_at
)
VALUES
    (
        '33333333-3333-4333-8333-333333333501',
        '33333333-3333-4333-8333-333333333401',
        0,
        'text',
        'Summarize the local demo readiness notes.',
        'completed',
        null,
        '{"source":"local_seed"}'::jsonb,
        now(),
        now()
    ),
    (
        '33333333-3333-4333-8333-333333333502',
        '33333333-3333-4333-8333-333333333402',
        0,
        'text',
        'The local demo seed contains a ready knowledge document, a generated report sample, and admin runtime config permissions for local inspection.',
        'completed',
        null,
        '{"source":"local_seed","usesRealProvider":false}'::jsonb,
        now(),
        now()
    )
ON CONFLICT (id) DO UPDATE
SET message_id = EXCLUDED.message_id,
    block_order = EXCLUDED.block_order,
    block_type = EXCLUDED.block_type,
    content = EXCLUDED.content,
    status = EXCLUDED.status,
    provider_block_id = null,
    provider_metadata = EXCLUDED.provider_metadata,
    updated_at = now();
