import importlib
import tempfile
import textwrap
import unittest
from pathlib import Path


def load_verifier():
    try:
        return importlib.import_module("scripts.verify_local_seed_contract")
    except ModuleNotFoundError as exc:
        if exc.name == "scripts.verify_local_seed_contract":
            raise AssertionError("scripts.verify_local_seed_contract module is missing") from exc
        raise


class LocalSeedContractTests(unittest.TestCase):
    def test_repository_seed_contract_has_no_issues(self) -> None:
        verifier = load_verifier()

        issues = verifier.verify_local_seed_contract(Path.cwd())

        self.assertEqual([], issues)

    def test_verifier_reports_missing_required_resource_ids(self) -> None:
        verifier = load_verifier()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "deploy" / "seeds").mkdir(parents=True)
            (root / "docs" / "runbooks").mkdir(parents=True)
            (root / "deploy" / ".env.example").write_text(
                "LOCAL_ADMIN_USERNAME=admin\n"
                "LOCAL_ADMIN_PASSWORD=LocalDemoAdmin#12345\n"
                "LOCAL_SUPER_ADMIN_USERNAME=superadmin\n"
                "LOCAL_SUPER_ADMIN_PASSWORD=LocalDemoAdmin#12345\n"
                "UV_DEFAULT_INDEX=https://pypi.org/simple\n"
                "GOPROXY=https://proxy.golang.org,direct\n"
                "GOSUMDB=sum.golang.org\n"
                "# POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:16-alpine\n"
                "# REDIS_IMAGE=docker.m.daocloud.io/library/redis:7-alpine\n"
                "# QDRANT_IMAGE=docker.m.daocloud.io/qdrant/qdrant:v1.18.2\n"
                "# MINIO_IMAGE=docker.m.daocloud.io/minio/minio:RELEASE.2025-09-07T16-13-09Z\n"
                "# MINIO_MC_IMAGE=docker.m.daocloud.io/minio/mc:RELEASE.2025-08-13T08-35-41Z\n"
                "VENDOR_RUNTIME_URL=http://127.0.0.1:9380\n"
                "KNOWLEDGE_AUTO_START_INGESTION=false\n"
                "# DOC_ENGINE=elasticsearch\n",
                encoding="utf-8",
            )
            (root / ".gitignore").write_text("/.local/\n*.pid\n", encoding="utf-8")
            (root / "services" / "parser").mkdir(parents=True)
            (root / "services" / "parser" / "uv.lock").write_text(
                'source = { registry = "https://pypi.tuna.tsinghua.edu.cn/simple" }\n'
                "https://pypi.tuna.tsinghua.edu.cn/packages/example.whl\n",
                encoding="utf-8",
            )
            (root / "deploy" / "seeds" / "001-local-demo-seed.sql").write_text(
                "\\connect auth_system\nINSERT INTO auth_users (id) VALUES ('usr_local_admin') ON CONFLICT (id) DO NOTHING;\n",
                encoding="utf-8",
            )
            (root / "deploy" / "seeds" / "002-ai-gateway-model-profiles.sql").write_text(
                "default-chat\nhttp://localhost:11434/v1\n",
                encoding="utf-8",
            )
            (root / "deploy" / "seeds" / "003-qa-document-mcp.sql").write_text(
                "\\connect qa_system\n"
                "INSERT INTO mcp_servers (alias) VALUES ('document') ON CONFLICT (alias) DO UPDATE;\n",
                encoding="utf-8",
            )
            (root / "deploy" / "seeds" / "004-qa-default-knowledge-base.sql").write_text(
                "\\connect qa_system\n"
                "keep QA's default knowledge-base list empty\n"
                "defaultKnowledgeBaseIds\n"
                "search all indexed\n"
                "DELETE FROM qa_config_knowledge_bases WHERE external_kb_id = 'kb_local_demo';\n",
                encoding="utf-8",
            )
            (root / "deploy" / "README.md").write_text(
                "deploy/.env.example 是唯一默认配置来源\n"
                "cp deploy/.env.example deploy/.env\n"
                "./scripts/local/dev-up.sh\n"
                "./scripts/local/run-backend.sh\n"
                "LOCAL_ADMIN_USERNAME=admin\n"
                "LOCAL_ADMIN_PASSWORD=LocalDemoAdmin#12345\n"
                "LOCAL_SUPER_ADMIN_USERNAME=superadmin\n"
                "LOCAL_SUPER_ADMIN_PASSWORD=LocalDemoAdmin#12345\n"
                "admin / LocalDemoAdmin#12345\n"
                "superadmin / LocalDemoAdmin#12345\n"
                "Go modules 下载默认读取 `deploy/.env`\n"
                "源选择采用新策略\n"
                "旧的大陆优先默认镜像契约已废弃\n"
                "默认使用官方源\n"
                "--china\n"
                "大陆镜像\n"
                "GOPROXY=https://proxy.golang.org,direct\n"
                "GOSUMDB=sum.golang.org\n"
                "cleanup with down -v\n",
                encoding="utf-8",
            )
            (root / "scripts" / "local").mkdir(parents=True)
            (root / "scripts" / "local" / "dev-up.sh").write_text(
                "[dev-up]\n"
                "[ok]\n"
                "[warn]\n"
                "[fail]\n"
                "[hint]\n"
                "completed successfully\n"
                "failed during\n"
                "Check Docker status:\n"
                "checking local tool dependencies\n"
                "missing required local command(s):\n"
                "Install Docker, Go, psql, uv, and curl\n"
                "Install the missing host tool(s)\n"
                "Mainland China network: rerun ./scripts/local/dev-up.sh --china.\n"
                "preparing Knowledge runtime dependencies with China mirrors\n"
                '--with "nltk>=3.9.4"\n'
                '--with "huggingface-hub>=1.3.1"\n'
                "ragflow_deps/download_deps.py --china\n"
                "--skip-knowledge-runtime-deps\n"
                "LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS\n"
                "checking Go module settings\n"
                "--china\n"
                "using selected default for this run\n"
                "docker.m.daocloud.io/library/postgres:16-alpine\n"
                "goose@v3.27.1\n"
                "psql\n"
                "INFRA_SERVICES=(postgres redis qdrant minio)\n"
                "initializing MinIO buckets\n"
                "--exit-code-from minio-init\n"
                "docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs minio-init\n"
                "001-local-demo-seed.sql\n"
                "002-ai-gateway-model-profiles.sql\n"
                "003-qa-document-mcp.sql\n"
                "004-qa-default-knowledge-base.sql\n"
                "--wait\n"
                "--wait-timeout\n"
                "initialize_qdrant_collection\n"
                "QDRANT_URL\n"
                "QDRANT_COLLECTION\n"
                "EMBEDDING_DIMENSION\n"
                "Cosine\n",
                encoding="utf-8",
            )
            (root / "scripts" / "local" / "run-backend.sh").write_text(
                "[backend]\n"
                "[ok]\n"
                "[warn]\n"
                "[fail]\n"
                "[hint]\n"
                "completed successfully\n"
                "failed during\n"
                "Check service logs under .local/logs/\n"
                "setsid\n"
                "go mod download\n"
                "checking Go modules\n"
                "--china\n"
                "using selected default for this run\n"
                "LOCAL_GO_MOD_DOWNLOAD_TIMEOUT_SECONDS\n"
                "go mod download timed out\n"
                "LOCAL_BACKEND_STARTUP_CHECK_SECONDS\n"
                "backend startup failed\n"
                "auth\nfile\nknowledge\n./cmd/adapter\ngo run \"$go_target\"\nai-gateway\nqa\ndocument\ngateway\n",
                encoding="utf-8",
            )
            (root / "scripts" / "local" / "stop-backend.sh").write_text(
                "[stop]\n"
                "[ok]\n"
                "[warn]\n"
                "[fail]\n"
                "[hint]\n"
                "completed successfully\n"
                "failed during\n"
                "nothing to stop\n"
                'kill -0 -- "-$pid"\n'
                'kill -TERM -- "-$pid"\n'
                'kill -KILL -- "-$pid"\n',
                encoding="utf-8",
            )
            (root / "docs" / "runbooks" / "local-integration.md").write_text(
                "local integration local seed\n",
                encoding="utf-8",
            )

            issues = verifier.verify_local_seed_contract(root)

        self.assertIssueContains(issues, "doc_local_demo_seed")
        self.assertIssueContains(issues, "22222222-2222-4222-8222-222222222301")
        self.assertIssueContains(issues, "33333333-3333-4333-8333-333333333301")
        self.assertIssueContains(issues, "AUTH_DATABASE_URL")

    def test_verifier_reports_missing_runtime_env_defaults(self) -> None:
        verifier = load_verifier()

        issues = verifier.validate_docs(
            deploy_readme="唯一默认配置来源\n",
            runbook="",
            env_example="VENDOR_RUNTIME_URL=http://127.0.0.1:9380\n",
            dev_up_script="",
            run_backend_script="",
            run_knowledge_parse_stack_script="",
            stop_backend_script="",
        )

        self.assertIssueContains(issues, "# DOC_ENGINE=elasticsearch")
        self.assertIssueContains(issues, "./cmd/adapter")

    def test_verifier_reports_missing_local_runtime_gitignore(self) -> None:
        verifier = load_verifier()

        issues = verifier.validate_gitignore("*.pid\n*.log\n")

        self.assertIssueContains(issues, ".gitignore")
        self.assertIssueContains(issues, "/.local/")

    def test_verifier_reports_container_only_ai_gateway_seed_url(self) -> None:
        verifier = load_verifier()

        issues = verifier.validate_seed_002(
            """
            default-chat
            default-embedding
            default-rerank
            host.docker.internal
            cred-local-chat
            cred-local-embedding
            cred-local-rerank
            local-demo-key-v1
            ON CONFLICT
            ON CONFLICT
            """
        )

        self.assertIssueContains(issues, "host.docker.internal")
        self.assertIssueContains(issues, "http://localhost:11434/v1")

    def test_verifier_reports_incomplete_document_mcp_seed(self) -> None:
        verifier = load_verifier()

        issues = verifier.validate_seed_003(
            """
            \\connect qa_system
            INSERT INTO mcp_servers (alias, token_encrypted)
            VALUES ('document', NULL)
            ON CONFLICT (alias) DO UPDATE;
            """
        )

        self.assertIssueContains(issues, "http://localhost:8085/mcp")
        self.assertIssueContains(issues, "33333333-3333-4333-8333-333333333601")

    def test_verifier_reports_missing_auth_qa_settings_permissions(self) -> None:
        verifier = load_verifier()

        issues = verifier.validate_auth_migrations(
            """
            INSERT INTO auth_permissions (code) VALUES ('qa:use');
            INSERT INTO role_permissions (id) VALUES ('rperm_admin_qa_use');
            """
        )

        self.assertIssueContains(issues, "qa:settings:read")
        self.assertIssueContains(issues, "qa:settings:write")
        self.assertIssueContains(issues, "rperm_admin_qa_settings_read")
        self.assertIssueContains(issues, "rperm_super_qa_settings_write")

    def assertIssueContains(self, issues: list[str], expected: str) -> None:
        self.assertTrue(
            any(expected in issue for issue in issues),
            f"Expected issue containing {expected!r}, got: {issues!r}",
        )


if __name__ == "__main__":
    unittest.main()
