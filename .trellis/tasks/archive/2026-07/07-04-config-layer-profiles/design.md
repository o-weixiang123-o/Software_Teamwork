# Design

## Architecture

Make `config/` the committed configuration authority and introduce a small
repository-level loader:

```text
config/base.yaml
config/{profile}.yaml
        |
        v
config/ctl       +  .env.local / process env
        |
        +-- .local/config/<profile>.env       # Docker Compose dotenv
        +-- .local/config/<profile>.env.sh    # shell-safe exports
        |
        v
scripts/local/dev-up.sh, run-backend.sh, runtime helpers
        |
        v
existing Go/Python services continue reading env vars
```

The important boundary is that services do not need to parse YAML directly in
this slice. They keep their current `internal/config` loaders, but local and
scripted startup gets those environment variables from `config/ctl`.

## Profile Format

Profiles are intentionally simple and flat at the render boundary:

```yaml
version: 1
profile: dev
description: Local development profile.
extends: base
env:
  GATEWAY_HTTP_ADDR:
    value: ":8080"
    description: Gateway host-run bind address.
  INTERNAL_SERVICE_TOKEN:
    fromEnv: INTERNAL_SERVICE_TOKEN
    required: true
    sensitive: true
    description: Shared local service token.
```

Field semantics:

- `value`: committed non-sensitive value. The loader exports it as-is unless an
  override is provided by a secret file or process env.
- `fromEnv`: read a value from `.env.local` or the process environment. If the
  entry is `required`, loader validation fails when it is missing.
- `required`: missing value is an error for render.
- `sensitive`: committed `value` is forbidden in staging and production. It is
  allowed in dev only for explicitly documented local/demo placeholders.
- `description`: documentation and schema clarity.

`base.yaml` may contain shared values and shared secret references. Environment
profiles override entries by key. The loader merges maps recursively enough for
the `env` map; in practice env entries are replaced by profile override.

## Loader

Implement `config/ctl` as a small Go module using `gopkg.in/yaml.v3`.

Commands:

```bash
config/ctl/configctl verify
config/ctl/configctl render --profile dev --secret-file .env.local --format dotenv --out .local/config/dev.env
config/ctl/configctl render --profile dev --secret-file .env.local --format shell --out .local/config/dev.env.sh
```

During development scripts may run it via:

```bash
(cd config/ctl && go run . render ...)
```

Render precedence:

```text
config/base.yaml value/fromEnv
< config/{profile}.yaml value/fromEnv
< dotenv secret files in order
< process environment
< script-specific flags such as --china
```

Process environment wins over `.env.local` so CI/CD and production injectors can
override local files without editing Git-tracked profiles.

## Secret Policy

The verifier rejects committed profile values that look like real secrets:

- key names containing `PASSWORD`, `TOKEN`, `SECRET`, `API_KEY`, `PRIVATE_KEY`,
  `CREDENTIAL`, `DSN`, or `DATABASE_URL` with inline values in staging or
  production;
- URL values with embedded username/password in staging or production;
- PEM/private-key/certificate material;
- common provider key prefixes or long high-entropy bearer-like values.

Staging and production profiles must use `fromEnv` / secret references for
sensitive settings. Dev may use obvious local placeholders only when they are
documented as demo values, but real personal provider keys still belong in
`.env.local`.

## Local Script Integration

Update local entrypoint scripts to call a shared helper:

```bash
scripts/config/load-profile.sh
```

The helper:

1. resolves `CONFIG_PROFILE` (default `dev`);
2. resolves `CONFIG_SECRET_FILE` (default `.env.local`);
3. calls `config/ctl` to render `.local/config/<profile>.env` and
   `.local/config/<profile>.env.sh`;
4. sources `.local/config/<profile>.env.sh` for host-run shell processes;
5. exposes `CONFIG_COMPOSE_ENV_FILE` for Docker Compose `--env-file`.

Scripts that currently hard-code `deploy/.env` should switch to
`$CONFIG_COMPOSE_ENV_FILE` after loading the profile.

`--china` remains a script runtime override and therefore still wins over
committed config. Scripts must not rewrite profile files or `.env.local`.

## Documentation Changes

- `config/README.md` is the authoritative explanation of profiles, precedence,
  secret ownership, and examples.
- Root `.env.example` becomes the canonical local secret template.
- `deploy/.env.example` is removed or reduced to a pointer that tells users to
  use `config/` + `.env.local`.
- `deploy/README.md`, local integration runbook, backend quality spec, and
  local seed verifier docs are updated to reference the new flow.

## Compatibility

This is not a gradual architecture change, but it preserves service runtime
contracts by keeping env vars as the service boundary. The breaking change is at
the repository startup layer: developers should use `.env.local` and profile
loader output instead of editing `deploy/.env`.

## Rollback

Rollback is straightforward:

- restore scripts to source `deploy/.env`;
- restore `deploy/.env.example` as the local default template;
- keep or remove `config/` depending on review outcome.

No database schema or service API contract changes are involved.
