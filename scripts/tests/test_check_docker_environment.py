import unittest

from scripts.check_docker_environment import (
    clean_subprocess_env,
    missing_localhost_no_proxy_entries,
    redact_proxy_value,
    selected_image_sets,
    summarize_output,
)


class DockerEnvironmentTests(unittest.TestCase):
    def test_clean_subprocess_env_removes_proxy_keys(self) -> None:
        env = clean_subprocess_env(
            {
                "PATH": "/bin",
                "http_proxy": "http://proxy:8080",
                "HTTPS_PROXY": "http://proxy:8080",
                "NO_PROXY": "localhost",
            }
        )

        self.assertEqual({"PATH": "/bin"}, env)

    def test_china_profile_uses_explicit_daocloud_registry(self) -> None:
        image_sets = selected_image_sets("china")

        self.assertEqual(1, len(image_sets))
        images = image_sets[0][1]
        self.assertNotIn("go builder", images)
        self.assertNotIn("parser python", images)
        self.assertEqual("docker.m.daocloud.io/library/postgres:16-alpine", images["postgres"])
        self.assertEqual(
            "docker.m.daocloud.io/docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
            images["elasticsearch"],
        )
        self.assertEqual(
            "docker.m.daocloud.io/infiniflow/ragflow_deps:51ce6aab",
            images["ragflow deps"],
        )

    def test_default_profiles_probe_ragflow_deps_image(self) -> None:
        self.assertEqual(
            "infiniflow/ragflow_deps:51ce6aab",
            selected_image_sets("default")[0][1]["ragflow deps"],
        )
        self.assertEqual(
            "registry-1.docker.io/infiniflow/ragflow_deps:51ce6aab",
            selected_image_sets("dockerhub-direct")[0][1]["ragflow deps"],
        )

    def test_redact_proxy_value_hides_credentials(self) -> None:
        value = redact_proxy_value("http://user:secret@example.com:8080/path")

        self.assertEqual("http://***@example.com:8080", value)

    def test_missing_localhost_no_proxy_entries_warns_for_proxy_without_local_bypass(self) -> None:
        missing = missing_localhost_no_proxy_entries(
            {
                "https_proxy": "http://proxy:8080",
                "NO_PROXY": "example.com",
            }
        )

        self.assertEqual(["localhost", "127.0.0.1", "::1"], missing)

    def test_missing_localhost_no_proxy_entries_accepts_wildcard_or_local_entries(self) -> None:
        self.assertEqual(
            [],
            missing_localhost_no_proxy_entries(
                {
                    "http_proxy": "http://proxy:8080",
                    "NO_PROXY": "localhost,127.0.0.1,::1",
                }
            ),
        )
        self.assertEqual(
            [],
            missing_localhost_no_proxy_entries(
                {
                    "http_proxy": "http://proxy:8080",
                    "no_proxy": "*",
                }
            ),
        )

    def test_summarize_output_uses_last_line(self) -> None:
        value = summarize_output("first\nERROR: failed to resolve manifest\n")

        self.assertEqual("ERROR: failed to resolve manifest", value)


if __name__ == "__main__":
    unittest.main()
