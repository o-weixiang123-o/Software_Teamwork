import os
import subprocess
import unittest
from pathlib import Path
from typing import Optional


class AIGatewayLocalSeedRendererTests(unittest.TestCase):
    renderer = Path("scripts/local/render_ai_gateway_local_seed.go")

    def test_disabled_seed_outputs_nothing(self) -> None:
        result = self.run_renderer({"AI_GATEWAY_LOCAL_SEED_ENABLED": "false"})

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual("", result.stdout)

    def test_enabled_seed_renders_encrypted_profiles_and_qa_config(self) -> None:
        api_key = "local-provider-key-for-tests"
        result = self.run_renderer(self.enabled_env({"AI_GATEWAY_LOCAL_PROVIDER_API_KEY": api_key}))

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("\\connect ai_gateway_system", result.stdout)
        self.assertIn("\\connect qa_system", result.stdout)
        self.assertIn("default-chat", result.stdout)
        self.assertIn("default-embedding", result.stdout)
        self.assertIn("default-rerank", result.stdout)
        self.assertIn("cred-local-chat", result.stdout)
        self.assertIn("provider_credentials", result.stdout)
        self.assertIn("llm_config_versions", result.stdout)
        self.assertIn("deepseek-ai/DeepSeek-V3", result.stdout)
        self.assertIn("BAAI/bge-m3", result.stdout)
        self.assertIn("BAAI/bge-reranker-v2-m3", result.stdout)
        self.assertIn("https://api.siliconflow.cn/v1", result.stdout)
        self.assertIn("decode(", result.stdout)
        self.assertIn("'ests'", result.stdout)
        self.assertNotIn(api_key, result.stdout)

    def test_missing_required_env_fails_with_key_names(self) -> None:
        env = self.enabled_env()
        env.pop("AI_GATEWAY_LOCAL_CHAT_MODEL")

        result = self.run_renderer(env)

        self.assertNotEqual(0, result.returncode)
        self.assertIn("AI_GATEWAY_LOCAL_CHAT_MODEL", result.stderr)
        self.assertNotIn(env["AI_GATEWAY_LOCAL_PROVIDER_API_KEY"], result.stderr)

    def test_invalid_provider_fails(self) -> None:
        result = self.run_renderer(self.enabled_env({"AI_GATEWAY_LOCAL_PROVIDER": "unknown"}))

        self.assertNotEqual(0, result.returncode)
        self.assertIn("AI_GATEWAY_LOCAL_PROVIDER must be one of", result.stderr)

    def enabled_env(self, overrides: Optional[dict[str, str]] = None) -> dict[str, str]:
        env = {
            "AI_GATEWAY_LOCAL_SEED_ENABLED": "true",
            "AI_GATEWAY_LOCAL_PROVIDER": "siliconflow",
            "AI_GATEWAY_LOCAL_PROVIDER_BASE_URL": "https://api.siliconflow.cn/v1",
            "AI_GATEWAY_LOCAL_PROVIDER_API_KEY": "local-provider-key-for-tests",
            "AI_GATEWAY_LOCAL_CHAT_MODEL": "deepseek-ai/DeepSeek-V3",
            "AI_GATEWAY_LOCAL_EMBEDDING_MODEL": "BAAI/bge-m3",
            "AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS": "1024",
            "AI_GATEWAY_LOCAL_RERANK_MODEL": "BAAI/bge-reranker-v2-m3",
            "AI_GATEWAY_LOCAL_RERANK_TOP_N": "5",
            "AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY": "local-demo-credential-key-change-me",
            "AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY_REF": "local-demo-key-v1",
        }
        env.update(overrides or {})
        return env

    def run_renderer(self, extra_env: dict[str, str]) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        for key in list(env):
            if key.startswith("AI_GATEWAY_LOCAL_") or key.startswith("AI_GATEWAY_CREDENTIAL_"):
                env.pop(key)
        env.update(extra_env)
        return subprocess.run(
            ["go", "run", str(self.renderer)],
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )


if __name__ == "__main__":
    unittest.main()
