import os
import shutil
import stat
import subprocess
import textwrap
import tempfile
import unittest
from pathlib import Path
from typing import Optional


class LocalDevUpScriptTests(unittest.TestCase):
    def test_minio_init_success_continues_to_migrations_and_seed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root)

            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("[ok] initializing MinIO buckets succeeded", result.stdout)
            self.assertIn("[dev-up] migrating auth", result.stdout)
            self.assertIn("infra, migrations, and seed are ready", result.stdout)

            docker_calls = (root / "docker-calls.log").read_text(encoding="utf-8")
            health_wait_call = next(line for line in docker_calls.splitlines() if " up -d --wait " in line)
            self.assertIn(" postgres redis minio elasticsearch", health_wait_call)
            self.assertNotIn("minio-init", health_wait_call)
            self.assertIn(" up --no-deps --exit-code-from minio-init minio-init", docker_calls)

    def test_minio_init_failure_stops_before_migrations_with_log_hint(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root, {"FAKE_MINIO_INIT_EXIT": "7"})

            self.assertNotEqual(0, result.returncode)
            self.assertNotIn("[dev-up] migrating auth", result.stdout)
            self.assertIn(
                "minio-init failed; inspect logs with: docker compose -f deploy/docker-compose.yml --env-file",
                result.stderr,
            )
            self.assertIn(".local/config/dev.env logs minio-init", result.stderr)

    def test_missing_psql_fails_before_docker_work(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))
            (root / "fake-bin" / "psql").unlink()

            result = self.run_dev_up(root, {"PATH": str(root / "fake-bin")})

            self.assertNotEqual(0, result.returncode)
            self.assertIn("missing required local command(s): psql", result.stderr)
            self.assertIn("Install the missing host tool(s), then rerun ./scripts/local/dev-up.sh.", result.stderr)
            self.assertNotIn("Check Docker with:", result.stderr)
            self.assertNotIn("If Go module download failed", result.stderr)
            self.assertIn("[dev-up] checking local tool dependencies", result.stdout)
            self.assertNotIn("[dev-up] pulling infrastructure images", result.stdout)
            self.assertFalse((root / "docker-calls.log").exists())

    def test_missing_go_fails_before_profile_render_docker_work(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))
            (root / "fake-bin" / "go").unlink()

            result = self.run_dev_up(root, {"PATH": str(root / "fake-bin")})

            self.assertNotEqual(0, result.returncode)
            self.assertIn("go is required to render config profiles with config/ctl", result.stderr)
            self.assertIn("install Go 1.25.x or run from an environment with go on PATH", result.stderr)
            self.assertNotIn("[dev-up] pulling infrastructure images", result.stdout)
            self.assertFalse((root / "docker-calls.log").exists())

    def test_china_flag_uses_mirrors_for_current_process_only(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root, args=["--china"])

            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("using mainland China mirrors for this run (--china)", result.stdout)
            env_text = (root / ".env.local").read_text(encoding="utf-8")
            self.assertIn("GOPROXY=https://proxy.golang.org,direct", env_text)
            docker_env = (root / "docker-env.log").read_text(encoding="utf-8")
            self.assertIn("POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:16-alpine", docker_env)
            self.assertIn(
                "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE=docker.m.daocloud.io/docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
                docker_env,
            )
            self.assertIn("GOPROXY=https://goproxy.cn,direct", docker_env)
            uv_calls = (root / "uv-calls.log").read_text(encoding="utf-8")
            self.assertIn("run --no-project", uv_calls)
            self.assertIn("--with nltk>=3.9.4", uv_calls)
            self.assertIn("--with huggingface-hub>=1.3.1", uv_calls)
            self.assertIn("ragflow_deps/download_deps.py --china", uv_calls)

    def test_elasticsearch_starts_with_default_infra(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root)

            self.assertEqual(0, result.returncode, result.stderr)
            docker_calls = (root / "docker-calls.log").read_text(encoding="utf-8")
            self.assertNotIn("--profile knowledge-runtime", docker_calls)
            self.assertIn("pull postgres redis minio minio-init elasticsearch", docker_calls)
            health_wait_call = next(line for line in docker_calls.splitlines() if " up -d --wait " in line)
            self.assertIn(" elasticsearch", health_wait_call)

    def test_ai_gateway_local_seed_overlay_runs_when_enabled(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))
            with (root / ".env.local").open("a", encoding="utf-8") as env_file:
                env_file.write(
                    textwrap.dedent(
                        """\
                        AI_GATEWAY_LOCAL_SEED_ENABLED=true
                        AI_GATEWAY_LOCAL_PROVIDER=siliconflow
                        AI_GATEWAY_LOCAL_PROVIDER_BASE_URL=https://api.siliconflow.cn/v1
                        AI_GATEWAY_LOCAL_PROVIDER_API_KEY=local-provider-key-for-tests
                        AI_GATEWAY_LOCAL_CHAT_MODEL=deepseek-ai/DeepSeek-V3
                        AI_GATEWAY_LOCAL_EMBEDDING_MODEL=BAAI/bge-m3
                        AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS=1024
                        AI_GATEWAY_LOCAL_RERANK_MODEL=BAAI/bge-reranker-v2-m3
                        AI_GATEWAY_LOCAL_RERANK_TOP_N=5
                        AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY=local-demo-credential-key-change-me
                        AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY_REF=local-demo-key-v1
                        """
                    )
                )

            result = self.run_dev_up(root)

            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("[dev-up] applying AI Gateway local env seed overlay", result.stdout)
            self.assertIn("[ok] applying AI Gateway local env seed overlay succeeded", result.stdout)
            go_calls = (root / "go-calls.log").read_text(encoding="utf-8")
            self.assertIn("render_ai_gateway_local_seed.go", go_calls)
            psql_stdin = (root / "psql-stdin.sql").read_text(encoding="utf-8")
            self.assertIn("-- rendered AI Gateway local overlay", psql_stdin)
            self.assertIn("deepseek-ai/DeepSeek-V3", psql_stdin)

    def test_ai_gateway_local_seed_overlay_skips_when_disabled(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root)

            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("AI_GATEWAY_LOCAL_SEED_ENABLED is not true", result.stdout)
            go_calls = (root / "go-calls.log").read_text(encoding="utf-8")
            self.assertNotIn("render_ai_gateway_local_seed.go", go_calls)

    def prepare_runtime(self, root: Path) -> Path:
        script_source = Path.cwd() / "scripts" / "local" / "dev-up.sh"
        script_target = root / "scripts" / "local" / "dev-up.sh"
        script_target.parent.mkdir(parents=True)
        shutil.copy2(script_source, script_target)
        script_target.chmod(script_target.stat().st_mode | stat.S_IXUSR)
        loader_source = Path.cwd() / "scripts" / "config" / "load-profile.sh"
        loader_target = root / "scripts" / "config" / "load-profile.sh"
        loader_target.parent.mkdir(parents=True)
        shutil.copy2(loader_source, loader_target)
        loader_target.chmod(loader_target.stat().st_mode | stat.S_IXUSR)
        (root / "config" / "ctl").mkdir(parents=True)

        (root / "deploy" / "seeds").mkdir(parents=True)
        (root / "deploy" / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        (root / ".env.local").write_text(
            textwrap.dedent(
                """\
                UV_DEFAULT_INDEX=https://pypi.org/simple
                GOPROXY=https://proxy.golang.org,direct
                GOSUMDB=sum.golang.org
                AUTH_DATABASE_URL=postgres://example/auth
                FILE_DATABASE_URL=postgres://example/file
                KNOWLEDGE_DATABASE_URL=postgres://example/knowledge
                QA_DATABASE_URL=postgres://example/qa
                DOCUMENT_DATABASE_URL=postgres://example/document
                AI_GATEWAY_DATABASE_URL=postgres://example/ai_gateway
                POSTGRES_ADMIN_URL=postgres://example/postgres
                """
            ),
            encoding="utf-8",
        )
        for seed in [
            "001-local-demo-seed.sql",
            "002-ai-gateway-model-profiles.sql",
            "003-qa-document-mcp.sql",
            "004-qa-default-knowledge-base.sql",
        ]:
            (root / "deploy" / "seeds" / seed).write_text("-- seed\n", encoding="utf-8")
        for service in ["auth", "file", "knowledge", "qa", "document", "ai-gateway"]:
            (root / "services" / service).mkdir(parents=True)
        (root / "services" / "knowledge-runtime" / "ragflow_deps").mkdir(parents=True)
        (root / "services" / "knowledge-runtime" / "ragflow_deps" / "download_deps.py").write_text(
            "# fake runtime download script\n",
            encoding="utf-8",
        )

        fake_bin = root / "fake-bin"
        fake_bin.mkdir()
        for command in ["bash", "cp", "dirname", "mkdir"]:
            target = shutil.which(command)
            if target is None:
                raise AssertionError(f"{command} is required to run dev-up.sh tests")
            os.symlink(target, fake_bin / command)
        self.write_executable(
            fake_bin / "docker",
            """\
            #!/usr/bin/env bash
            echo "$*" >> "$FAKE_DOCKER_CALLS"
            {
              echo "POSTGRES_IMAGE=${POSTGRES_IMAGE:-}"
              echo "REDIS_IMAGE=${REDIS_IMAGE:-}"
              echo "MINIO_IMAGE=${MINIO_IMAGE:-}"
              echo "MINIO_MC_IMAGE=${MINIO_MC_IMAGE:-}"
              echo "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE=${KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE:-}"
              echo "UV_DEFAULT_INDEX=${UV_DEFAULT_INDEX:-}"
              echo "GOPROXY=${GOPROXY:-}"
              echo "GOSUMDB=${GOSUMDB:-}"
            } >> "$FAKE_DOCKER_ENV"
            case " $* " in
              *" up -d --wait "*)
                if [[ " $* " == *" minio-init "* ]]; then
                  echo "minio-init must not be part of the health wait" >&2
                  exit 80
                fi
                ;;
              *" up --no-deps --exit-code-from minio-init minio-init "*)
                exit "${FAKE_MINIO_INIT_EXIT:-0}"
                ;;
            esac
            exit 0
            """,
        )
        self.write_executable(
            fake_bin / "go",
            """\
            #!/usr/bin/env bash
            rel="${PWD#"$FAKE_ROOT"/}"
            echo "$rel|$*" >> "$FAKE_GO_CALLS"
            if [[ "$rel" == "config/ctl" && "$1" == "run" && "$2" == "." && "$3" == "render" ]]; then
              format="dotenv"
              out=""
              while (($# > 0)); do
                case "$1" in
                  --format)
                    format="$2"
                    shift 2
                    ;;
                  --out)
                    out="$2"
                    shift 2
                    ;;
                  *)
                    shift
                    ;;
                esac
              done
              [[ -n "$out" ]] || exit 64
              mkdir -p "$(dirname "$out")"
              if [[ "$format" == "shell" ]]; then
                while IFS= read -r line || [[ -n "$line" ]]; do
                  [[ -z "$line" || "$line" =~ ^[[:space:]]*# || "$line" != *=* ]] && continue
                  key="${line%%=*}"
                  value="${line#*=}"
                  printf "export %s=%q\\n" "$key" "$value"
                done < "$FAKE_ROOT/.env.local" > "$out"
              else
                cp "$FAKE_ROOT/.env.local" "$out"
              fi
              exit 0
            fi
            if [[ "$1" == "run" && "$2" == *"render_ai_gateway_local_seed.go" ]]; then
              echo "-- rendered AI Gateway local overlay"
              echo "SELECT '${AI_GATEWAY_LOCAL_CHAT_MODEL:-missing-chat-model}';"
            fi
            exit 0
            """,
        )
        self.write_executable(
            fake_bin / "psql",
            """\
            #!/usr/bin/env bash
            if [[ ! -t 0 ]]; then
              cat >> "$FAKE_PSQL_STDIN"
            fi
            exit 0
            """,
        )
        self.write_executable(
            fake_bin / "uv",
            """\
            #!/usr/bin/env bash
            echo "$*" >> "$FAKE_UV_CALLS"
            exit 0
            """,
        )
        return root

    def run_dev_up(
        self,
        root: Path,
        extra_env: Optional[dict[str, str]] = None,
        args: Optional[list[str]] = None,
    ) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update(extra_env or {})
        env["FAKE_DOCKER_CALLS"] = str(root / "docker-calls.log")
        env["FAKE_DOCKER_ENV"] = str(root / "docker-env.log")
        env["FAKE_GO_CALLS"] = str(root / "go-calls.log")
        env["FAKE_ROOT"] = str(root)
        env["FAKE_PSQL_STDIN"] = str(root / "psql-stdin.sql")
        env["FAKE_UV_CALLS"] = str(root / "uv-calls.log")
        if extra_env and "PATH" in extra_env:
            env["PATH"] = extra_env["PATH"]
        else:
            env["PATH"] = f"{root / 'fake-bin'}{os.pathsep}{env['PATH']}"
        return subprocess.run(
            [str(root / "scripts" / "local" / "dev-up.sh"), *(args or [])],
            cwd=root,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )

    def write_executable(self, path: Path, content: str) -> None:
        path.write_text(textwrap.dedent(content), encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR)


if __name__ == "__main__":
    unittest.main()
