import unittest
from pathlib import Path


class KnowledgeRuntimeDependencySplitTests(unittest.TestCase):
    def test_api_only_helper_uses_base_dependency_profile(self) -> None:
        script = Path("scripts/local/run-knowledge-runtime-api.sh").read_text(encoding="utf-8")

        self.assertIn("uv sync --python 3.13 --frozen --no-default-groups", script)
        self.assertIn("uv run --no-sync --no-default-groups", script)
        self.assertNotIn("uv sync --python 3.13 --frozen --group worker", script)
        self.assertNotIn('start_service "knowledge-runtime-worker"', script)

    def test_parse_stack_helper_uses_worker_dependency_profile(self) -> None:
        script = Path("scripts/local/run-knowledge-parse-stack.sh").read_text(encoding="utf-8")

        self.assertIn("uv sync --python 3.13 --frozen --group worker", script)
        self.assertIn('start_service "knowledge-runtime-api"', script)
        self.assertIn('start_service "knowledge-runtime-worker"', script)

    def test_worker_only_helper_uses_worker_dependency_profile(self) -> None:
        script = Path("scripts/local/start-knowledge-runtime-worker.sh").read_text(encoding="utf-8")
        watcher = Path("scripts/local/watch-knowledge-runtime-worker-idle.sh").read_text(encoding="utf-8")

        self.assertIn("uv sync --python 3.13 --frozen --group worker", script)
        self.assertIn("knowledge-runtime-worker", script)
        self.assertIn("waiting for knowledge-runtime-worker heartbeat", script)
        self.assertIn("task_executor_heartbeats", script)
        self.assertIn("KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS", script)
        self.assertIn("watch-knowledge-runtime-worker-idle.sh", script)
        self.assertIn("worker_queue_idle", watcher)
        self.assertIn("stop_worker_group", watcher)
        self.assertIn("valkey.Valkey", watcher)
        self.assertNotIn('start_service "knowledge-runtime-api"', script)
        self.assertNotIn("knowledge-adapter", script)

    def test_runtime_entrypoints_match_dependency_profiles(self) -> None:
        api_script = Path("services/knowledge-runtime/deploy/api/run-local.sh").read_text(encoding="utf-8")
        worker_script = Path("services/knowledge-runtime/deploy/worker/run-local.sh").read_text(encoding="utf-8")

        self.assertIn("uv run --no-sync --no-default-groups", api_script)
        self.assertIn("uv run --no-sync --group worker", worker_script)
        self.assertIn("KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA", worker_script)
        self.assertIn("NLTK_DATA", worker_script)

    def test_download_deps_prefetches_models_to_worker_runtime_path(self) -> None:
        script = Path("services/knowledge-runtime/ragflow_deps/download_deps.py").read_text(encoding="utf-8")

        self.assertIn("model_local_directory", script)
        self.assertIn('"rag" / "res" / "deepdoc"', script)
        self.assertIn("Falling back to direct file downloads", script)
        self.assertIn("HfApi", script)
        self.assertIn('"InfiniFlow/deepdoc"', script)
        self.assertIn('"InfiniFlow/text_concat_xgb_v1.0"', script)


if __name__ == "__main__":
    unittest.main()
