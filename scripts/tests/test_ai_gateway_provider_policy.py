import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from scripts.check_ai_gateway_provider_policy import verify_ai_gateway_provider_policy


class AIGatewayProviderPolicyTests(unittest.TestCase):
    def test_ai_gateway_client_endpoint_is_allowed(self) -> None:
        issues = self.verify(
            files={
                "services/qa/internal/modelendpoint/endpoint.go": 'const path = "/internal/v1/chat/completions"\n',
                "services/document/internal/platform/aigateway/chat_client.go": 'endpoint := base + "/internal/v1/chat/completions"\n',
            }
        )

        self.assertEqual([], issues)

    def test_direct_provider_sdk_outside_ai_gateway_is_reported(self) -> None:
        issues = self.verify(
            files={
                "services/qa/internal/platform/modelclient/escape.py": textwrap.dedent(
                    """
                    from openai import OpenAI

                    client = OpenAI(api_key="token", base_url="https://api.openai.com/v1")
                    """
                )
            }
        )

        self.assertIssueContains(issues, "OpenAI Python SDK import")
        self.assertIssueContains(issues, "direct OpenAI provider base URL")

    def test_knowledge_runtime_fallback_is_allowlisted(self) -> None:
        issues = self.verify(
            files={
                "services/knowledge-runtime/rag/llm/chat_model.py": textwrap.dedent(
                    """
                    from openai import OpenAI
                    client = OpenAI(api_key=key, base_url="https://api.openai.com/v1")
                    """
                )
            }
        )

        self.assertEqual([], issues)

    def test_ai_gateway_seed_renderer_is_allowlisted(self) -> None:
        issues = self.verify(
            files={
                "scripts/local/render_ai_gateway_local_seed.go": 'baseURL := "https://api.siliconflow.cn/v1"\n',
            }
        )

        self.assertEqual([], issues)

    def verify(self, files: dict[str, str]) -> list[str]:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for relative, content in files.items():
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            return verify_ai_gateway_provider_policy(root)

    def assertIssueContains(self, issues: list[str], expected: str) -> None:
        if not any(expected in issue for issue in issues):
            self.fail(f"expected issue containing {expected!r}, got {issues!r}")


if __name__ == "__main__":
    unittest.main()
