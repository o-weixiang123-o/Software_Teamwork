import tempfile
import textwrap
import unittest
from pathlib import Path

from scripts.check_docker_policy import verify_docker_policy


VALID_GO_DOCKERFILE = textwrap.dedent(
    """
    ARG IMAGE_REGISTRY_PREFIX=
    ARG GO_VERSION=1.25
    ARG ALPINE_VERSION=3.22

    FROM ${IMAGE_REGISTRY_PREFIX}golang:${GO_VERSION}-alpine AS build
    ARG GOPROXY=https://proxy.golang.org,direct
    ARG GOSUMDB=sum.golang.org
    ARG ALPINE_MIRROR=
    ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}
    COPY go.mod go.sum ./
    RUN --mount=type=cache,target=/go/pkg/mod go mod download
    COPY . .
    RUN --mount=type=cache,target=/go/pkg/mod \\
        --mount=type=cache,target=/root/.cache/go-build \\
        CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/auth ./cmd/server

    FROM ${IMAGE_REGISTRY_PREFIX}alpine:${ALPINE_VERSION}
    ARG ALPINE_MIRROR=
    RUN --mount=type=cache,target=/var/cache/apk apk add --update-cache --cache-dir /var/cache/apk ca-certificates
    COPY --from=build /out/auth /usr/local/bin/auth
    ENTRYPOINT ["auth"]
    """
)


VALID_COMPOSE = textwrap.dedent(
    """
    services:
      postgres:
        image: ${POSTGRES_IMAGE:-postgres:16-alpine}
      redis:
        image: ${REDIS_IMAGE:-redis:7-alpine}
      qdrant:
        image: ${QDRANT_IMAGE:-qdrant/qdrant:v1.18.2}
      minio:
        image: ${MINIO_IMAGE:-minio/minio:RELEASE.2025-09-07T16-13-09Z}
      minio-init:
        image: ${MINIO_MC_IMAGE:-minio/mc:RELEASE.2025-08-13T08-35-41Z}
      elasticsearch:
        profiles: ["knowledge-runtime"]
        image: ${KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE:-docker.elastic.co/elasticsearch/elasticsearch:8.15.3}
    """
)


VALID_ENV = textwrap.dedent(
    """
    UV_DEFAULT_INDEX=https://pypi.org/simple
    GOPROXY=https://proxy.golang.org,direct
    GOSUMDB=sum.golang.org
    # POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:16-alpine
    # REDIS_IMAGE=docker.m.daocloud.io/library/redis:7-alpine
    # QDRANT_IMAGE=docker.m.daocloud.io/qdrant/qdrant:v1.18.2
    # MINIO_IMAGE=docker.m.daocloud.io/minio/minio:RELEASE.2025-09-07T16-13-09Z
    # MINIO_MC_IMAGE=docker.m.daocloud.io/minio/mc:RELEASE.2025-08-13T08-35-41Z
    KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE=docker.elastic.co/elasticsearch/elasticsearch:8.15.3
    """
)

class DockerPolicyTests(unittest.TestCase):
    def test_valid_policy_files_have_no_issues(self) -> None:
        issues = self.verify(
            files={
                "deploy/docker-compose.yml": VALID_COMPOSE,
                "deploy/.env.example": VALID_ENV,
            }
        )

        self.assertEqual([], issues)

    def test_root_compose_default_must_be_infra_only(self) -> None:
        compose = VALID_COMPOSE.replace(
            "  qdrant:\n",
            "  gateway:\n    image: ${GATEWAY_IMAGE:-registry.example.com/gateway:local}\n  qdrant:\n",
        )

        issues = self.verify(files={"deploy/docker-compose.yml": compose})

        self.assertIssueContains(issues, "local Docker default must only define infrastructure services")
        self.assertIssueContains(issues, "business service `gateway` must run on the host")

    def test_root_compose_default_must_not_build_business_services(self) -> None:
        compose = VALID_COMPOSE.replace(
            "  minio-init:\n    image:",
            "  parser:\n    build:\n      context: ../services/parser\n  minio-init:\n    image:",
        )

        issues = self.verify(files={"deploy/docker-compose.yml": compose})

        self.assertIssueContains(issues, "local Docker default must only define infrastructure services")
        self.assertIssueContains(issues, "business service `parser` must run on the host")
        self.assertIssueContains(issues, "Compose must not use `build:`")

    def test_profile_services_are_reported(self) -> None:
        compose = VALID_COMPOSE.replace(
            "  minio-init:\n",
            '  gateway:\n    profiles: ["debug"]\n    image: ${GATEWAY_IMAGE:-registry.example.com/gateway:local}\n  minio-init:\n',
        )

        issues = self.verify(files={"deploy/docker-compose.yml": compose})

        self.assertIssueContains(issues, "profile service `gateway` is not allowed")

    def test_elasticsearch_profile_must_not_use_build(self) -> None:
        compose = VALID_COMPOSE.replace(
            "  elasticsearch:\n    profiles: [\"knowledge-runtime\"]\n    image:",
            "  elasticsearch:\n    profiles: [\"knowledge-runtime\"]\n    build:\n      context: .\n    image:",
        )

        issues = self.verify(files={"deploy/docker-compose.yml": compose})

        self.assertIssueContains(issues, "Compose must not use `build:`")

    def test_compose_image_defaults_must_stay_pinned_and_overridable(self) -> None:
        compose = VALID_COMPOSE.replace(
            "image: ${POSTGRES_IMAGE:-postgres:16-alpine}",
            "image: postgres:latest",
        )

        issues = self.verify(files={"deploy/docker-compose.yml": compose})

        self.assertIssueContains(issues, "must not use latest")
        self.assertIssueContains(issues, "must be exposed through")

    def test_dockerfile_regressions_are_reported(self) -> None:
        dockerfile = VALID_GO_DOCKERFILE.replace(
            "FROM ${IMAGE_REGISTRY_PREFIX}alpine:${ALPINE_VERSION}",
            "FROM alpine:latest",
        ).replace("ARG GOSUMDB=sum.golang.org", "ARG GOSUMDB=off")

        issues = self.verify(files={"services/auth/Dockerfile": "# syntax=docker/dockerfile:1\n" + dockerfile})

        self.assertIssueContains(issues, "business service Dockerfile")
        self.assertIssueContains(issues, "external Dockerfile frontend")
        self.assertIssueContains(issues, "GOSUMDB=off")
        self.assertIssueContains(issues, "base image `alpine:latest` must use ${IMAGE_REGISTRY_PREFIX}")
        self.assertIssueContains(issues, "must not use latest")
        self.assertIssueContains(issues, "sibling .dockerignore")

    def test_knowledge_runtime_dockerfile_is_reported_without_parser_policy(self) -> None:
        dockerfile = textwrap.dedent(
            """
            ARG IMAGE_REGISTRY_PREFIX=
            FROM ${IMAGE_REGISTRY_PREFIX}ubuntu:24.04 AS base
            RUN uv sync --python 3.13 --frozen
            CMD ["python", "api/ragflow_server.py"]
            """
        )

        issues = self.verify(
            files={
                "services/knowledge-runtime/Dockerfile": dockerfile,
                "services/knowledge-runtime/.dockerignore": ".git\n",
            }
        )

        parser_issues = [issue for issue in issues if "services/parser is retired" in issue]
        self.assertEqual([], parser_issues)
        self.assertIssueContains(issues, "services/knowledge-runtime/Dockerfile")
        self.assertIssueContains(issues, "business service Dockerfile")

    def test_parser_dockerfile_is_reported(self) -> None:
        issues = self.verify(
            files={
                "services/parser/Dockerfile": "FROM python:3.12\nCMD [\"parser-service\"]\n",
                "services/parser/.dockerignore": ".venv/\n",
            }
        )

        self.assertIssueContains(issues, "services/parser/Dockerfile")
        self.assertIssueContains(issues, "business service Dockerfile")
        self.assertIssueContains(issues, "services/parser is retired")

    def test_env_example_regressions_are_reported(self) -> None:
        env = (
            VALID_ENV
            + "\nPOSTGRES_IMAGE=postgres:latest\n"
            + "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE=docker.m.daocloud.io/docker.elastic.co/elasticsearch/elasticsearch:8.15.3\n"
            + "HF_ENDPOINT=https://hf-mirror.com\n"
        )

        issues = self.verify(files={"deploy/.env.example": env})

        self.assertIssueContains(issues, "POSTGRES_IMAGE")
        self.assertIssueContains(issues, "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE")
        self.assertIssueContains(issues, "must not be active by default")
        self.assertIssueContains(issues, "must not use latest")
        self.assertIssueContains(issues, "HF_ENDPOINT=https://hf-mirror.com")

    def test_business_docker_artifacts_are_reported(self) -> None:
        issues = self.verify(
            files={
                "services/auth/Dockerfile": "FROM golang:1.25\n",
                "services/parser/docker-compose.yml": "services: {}\n",
                "deploy/docker-compose.production.yml": "services: {}\n",
                "deploy/compose.preview.yml": "services: {}\n",
            }
        )

        self.assertIssueContains(issues, "services/auth/Dockerfile")
        self.assertIssueContains(issues, "business service Dockerfile")
        self.assertIssueContains(issues, "services/parser/docker-compose.yml")
        self.assertIssueContains(issues, "service-level Compose file")
        self.assertIssueContains(issues, "deploy/docker-compose.production.yml")
        self.assertIssueContains(issues, "deploy/compose.preview.yml")
        self.assertIssueContains(issues, "non-root deploy Compose file")

    def verify(self, *, files: dict[str, str]) -> list[str]:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            all_files = dict(files)
            all_files.setdefault("deploy/docker-compose.yml", VALID_COMPOSE)
            all_files.setdefault("deploy/.env.example", VALID_ENV)
            for relative, content in all_files.items():
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            return verify_docker_policy(root)

    def assertIssueContains(self, issues: list[str], expected: str) -> None:
        self.assertTrue(
            any(expected in issue for issue in issues),
            f"Expected issue containing {expected!r}, got: {issues!r}",
        )


if __name__ == "__main__":
    unittest.main()
