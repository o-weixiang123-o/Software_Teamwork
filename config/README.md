# Configuration Profiles

This directory is the repository-level configuration authority.

Committed profiles contain non-sensitive defaults, environment-specific
overrides, and references to secrets. Real secret values belong in untracked
local files such as `.env.local`, CI/CD masked variables, Kubernetes Secrets,
Vault, SOPS, External Secrets, or an equivalent deployment-owned secret system.

## Files

```text
config/
├── schema.yaml
├── base.yaml
├── dev.yaml
├── staging.yaml
├── production.yaml
└── ctl/
```

- `base.yaml`: shared non-sensitive defaults and secret reference names.
- `dev.yaml`: committed local-development overrides.
- `staging.yaml`: committed staging overrides and secret references only.
- `production.yaml`: committed production overrides and secret references only.
- `schema.yaml`: profile contract and secret policy.
- `ctl/`: profile verifier and renderer. This is the executable part of the
  repository configuration layer; startup scripts only call it.

## Precedence

Runtime configuration is resolved in this order:

```text
code defaults
< config/base.yaml
< config/{profile}.yaml
< .env.local or another untracked secret file
< process environment injected by CI/CD or a platform
< explicit script flags such as --china
```

The Go and Python services continue to consume environment variables. Local
scripts use the `config/ctl` resolver to merge profiles and render environment
files under `.local/config/` before starting Docker Compose or host-run
services.

## Local Usage

```bash
cp .env.example .env.local
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

The default profile is `dev`. Override it with:

```bash
CONFIG_PROFILE=staging ./scripts/local/dev-up.sh
```

Use a different untracked secret file with:

```bash
CONFIG_SECRET_FILE=.env.my-machine ./scripts/local/dev-up.sh
```

## Secret Rules

Committed profile files must not contain:

- real API keys, bearer tokens, passwords, or service tokens;
- DSNs or URLs with embedded credentials;
- private keys, certificates, or PEM blocks;
- production provider credentials.

Staging and production profiles must use `fromEnv` references for sensitive
values. Development profiles may reference local/demo placeholders from
`.env.local`, but real personal provider keys still belong only in untracked
files or the process environment.

## Validation

```bash
cd config/ctl && go run . verify
```

To render the dev profile using the template values:

```bash
cd config/ctl
go run . render --profile dev --secret-file ../../.env.example --format dotenv --out ../../.local/config/dev.env
go run . render --profile dev --secret-file ../../.env.example --format shell --out ../../.local/config/dev.env.sh
```
