-- Local integration contract: keep QA's default knowledge-base list empty.
--
-- Knowledge runtime is the retrieval source of truth. An empty
-- defaultKnowledgeBaseIds list lets QA/Knowledge MCP search all indexed
-- knowledge bases. Older local volumes may still contain the retired
-- kb_local_demo binding, so this seed removes that legacy default instead of
-- inserting it.
\connect qa_system

DELETE FROM qa_config_knowledge_bases
WHERE external_kb_id = 'kb_local_demo';
