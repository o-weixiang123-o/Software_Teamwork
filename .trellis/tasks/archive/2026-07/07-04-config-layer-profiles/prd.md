# Repository configuration layer profiles

## Goal

Redesign and implement the repository configuration layer according to mature
open-source and production practices:

- committed files contain non-sensitive defaults, environment profile
  overrides, schema, and secret references;
- uncommitted files or deployment systems provide local and production secret
  values;
- applications and scripts load configuration through a predictable precedence
  chain with validation.

## Background

The current repository relies on `deploy/.env.example` as the primary local
template and `deploy/.env` as the copied untracked runtime file. Go services
mostly read environment variables directly in service-local config packages.
This must be replaced as the architecture authority because it mixes several
concerns:

- local/demo placeholders,
- deployment-time environment variables,
- infrastructure image defaults,
- service URLs,
- secret placeholders,
- optional provider overrides,
- runtime helper settings.

The desired architecture should match common production practice:

```text
code defaults
< committed config/base.yaml
< committed config/{profile}.yaml
< uncommitted .env.local or deployment-injected environment
< explicit CLI flags
```

Committed production/staging profiles must not store real secrets. They should
store non-sensitive values and secret/env references such as
`DATABASE_URL`, `AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY`, Kubernetes Secret names,
Vault paths, or External Secrets keys.

## Requirements

### Profile Layout

- Add a committed repository-level configuration directory:

```text
config/
├── README.md
├── schema.yaml
├── base.yaml
├── dev.yaml
├── staging.yaml
└── production.yaml
```

- `base.yaml` contains shared non-sensitive defaults and profile metadata.
- `dev.yaml` contains committed development overrides and references to local
  env variables, not private developer secrets.
- `staging.yaml` contains non-sensitive staging overrides and explicit secret
  references.
- `production.yaml` contains non-sensitive production overrides and explicit
  secret references only.
- `schema.yaml` documents supported top-level keys, expected value kinds, and
  sensitive-key policy.

### Secret Boundary

- Committed config files must not contain real API keys, passwords, bearer
  tokens, service tokens, DSNs with credentials, private keys, certificates, or
  production provider credentials.
- Local real values belong in untracked local files such as `deploy/.env`,
  `.env.local`, or shell environment.
- Production real values belong in CI/CD masked variables, Kubernetes Secret,
  Vault, cloud secret managers, SOPS-encrypted files, External Secrets, or
  equivalent deployment-owned mechanisms.
- Committed profiles may contain references to secret locations or env var names.

### Runtime Loading

- Implement a repository-level profile loader command that:
  - merges `config/base.yaml` and `config/{profile}.yaml`;
  - reads uncommitted dotenv secret files such as `.env.local`;
  - allows process environment variables to override secret-file values;
  - validates required values before scripts start services;
  - renders a Docker Compose compatible dotenv file for infrastructure;
  - renders shell-safe environment exports for host-run scripts.
- Local scripts must use the loader instead of sourcing `deploy/.env` directly.
- Default local profile is `dev`; scripts can select another profile through a
  flag or `CONFIG_PROFILE`.
- Canonical local secret template is root `.env.example`; developers copy it to
  untracked `.env.local`.
- `deploy/.env.example` is no longer the default configuration source. It may be
  removed or converted to a compatibility pointer, as long as docs and tests no
  longer treat it as authoritative.
- Go service config packages may continue to consume environment variables, but
  those variables must come from the profile loader for local/scripted startup.

### Validation

- Add a repository verifier that checks:
  - required profile files exist;
  - profile files are parseable YAML;
  - each profile declares the expected profile name;
  - staging/production profiles contain secret references rather than inline
    secret values;
  - obvious secret-like inline values are rejected.
- Add tests for the verifier.
- Integrate the verifier into existing local config/seed policy checks or add an
  explicit config-profile test command.

### Documentation

- Update local integration/deploy docs to explain the new source-of-truth
  split:
  - committed profiles for non-sensitive config and references;
  - root `.env.example` as the local secret template;
  - untracked `.env.local` for real local secrets;
  - production secret managers or deployment injection for real production
    values.
- Update Trellis backend quality/config guidance so future tasks follow the new
  profile-first contract instead of adding more ad hoc env-only defaults.

## Acceptance Criteria

- [ ] `config/README.md`, `config/schema.yaml`, `config/base.yaml`,
      `config/dev.yaml`, `config/staging.yaml`, and `config/production.yaml`
      exist.
- [ ] `staging.yaml` and `production.yaml` contain no real secret values and
      use env/secret references for sensitive settings.
- [ ] A verifier rejects missing profiles, invalid YAML, profile-name mismatch,
      and obvious inline secrets in committed profiles.
- [ ] Verifier tests cover success and failure cases.
- [ ] Local scripts render config through the profile loader and no longer source
      `deploy/.env` as the authoritative default.
- [ ] Root `.env.example` documents local/deployment secret keys; `.env.local`
      remains ignored.
- [ ] Docs describe config precedence and secret ownership clearly.
- [ ] Local startup docs tell developers to copy `.env.example` to `.env.local`
      and run scripts with `CONFIG_PROFILE=dev` or the default dev profile.
- [ ] Docker/local policy checks still pass after config docs and verifier
      changes.

## Out Of Scope

- Rewriting every Go service `internal/config` package to parse YAML directly;
  services continue consuming the environment generated by the profile loader.
- Introducing a cluster secret operator or production Kubernetes deployment in
  this slice.
- Changing service ports, database schemas, provider invocation behavior, or
  Knowledge runtime lifecycle behavior.
