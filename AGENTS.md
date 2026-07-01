# AGENTS.md

This file provides guidance for AI coding agents working in the
`infra-controller` repository.

## Project Overview

**NCX Infra Controller (NICo)** is an API-based microservice written in Rust
that provides site-local, zero-trust, bare-metal lifecycle management with
DPU-enforced isolation. It automates the complexity of the bare-metal lifecycle
to fast-track building next-generation AI Cloud offerings.

> **Status:** Experimental/Preview. APIs, configurations, and features may
> change without notice between releases.

### Key Responsibilities

- Hardware inventory management and orchestration
- Redfish-based hardware management
- Hardware testing and firmware updates
- IP address allocation and DNS services
- Power control (on/off/reset)
- Provisioning, wiping, and node-release orchestration
- Machine trust enforcement during tenant switching

## Repository Structure

```
infra-controller/
├── crates/              # Rust crate implementations. To discover all crates
│                        # and their purpose, run `ls crates/` or see the
│                        # [workspace] members list in `Cargo.toml` — each
│                        # crate's own `Cargo.toml` has a `description` field.
│                        # Note: the directory name does NOT always equal the
│                        # crate name (e.g. crates/api/ → crate nico-api).
│                        # Use `grep '^name =' crates/<dir>/Cargo.toml | head -1`
│                        # to get the actual crate name before running
│                        # `cargo test -p <name>` or similar.
├── book/                # mdBook documentation
├── deploy/              # Kubernetes deployment configs and Kustomization overlays
├── dev/                 # Local dev tools (Dockerfiles, test configs, certs)
├── helm/                # Helm chart for Kubernetes deployment
├── bluefield/           # BlueField DPU-specific components
├── pxe/                 # PXE boot artifact generation
├── lints/               # Custom Clippy lints (carbide-lints crate)
├── include/             # Shared Makefile fragments
├── .github/             # GitHub Actions workflows and templates
├── Cargo.toml           # Workspace dependency management
├── Makefile.toml        # Primary build/task automation
├── Makefile-build.toml  # Build-specific tasks
└── Makefile-package.toml # Packaging tasks
```

## Technology Stack

- **Language:** Rust (edition 2024, toolchain pinned in `rust-toolchain.toml`)
- **Async runtime:** Tokio
- **gRPC framework:** Tonic (with TLS via Rustls/aws_lc_rs)
- **HTTP framework:** Axum (pinned; see `Cargo.toml` for compatibility rationale)
- **Database:** SQLx (compile-time checked queries)
- **Observability:** OpenTelemetry, Tracing (structured logfmt logging)
- **Build tool:** `cargo-make` (TOML task runner)
- **API definitions:** Protocol Buffers (protobuf)

## Build, Test, and Lint Commands

All task automation uses `cargo-make`. Install it with:

```bash
cargo install cargo-make
```

### Building

```bash
# Standard debug build (all workspace crates)
cargo build

# Release build
cargo build --release

# Full CI build + test (mirrors what CI runs)
cargo make build-and-test-release-container-services

# Build the admin CLI locally
cargo make build-cli
```

### Testing

```bash
# Run all tests
cargo test

# Build prerequisites first, then test (recommended for integration tests)
cargo make correctly-execute-tests
```

When writing tests, prefer the **table-driven** style — see the [Testing section in `STYLE_GUIDE.md`](STYLE_GUIDE.md#testing).
Enumerating a function's input variants as grouped `carbide-test-support` scenarios (`scenarios!` / `value_scenarios!`)
or explicit cases (`check_cases` / `check_values`) is the easiest way to reach thorough coverage of parsers, validators,
conversions, and the like.

### Linting and Formatting

```bash
# Run all pre-commit checks (what CI runs)
cargo make pre-commit-verify-workspace

# Individual checks:
cargo make clippy              # Clippy linter (warnings = errors)
cargo make carbide-lints       # Custom lints (requires nightly setup)
cargo make check-format-nightly # Check rustfmt formatting
cargo make check-workspace-deps # Validate dependency declarations in Cargo.toml
cargo make check-licenses      # Validate no restricted licenses introduced
cargo make check-bans          # Check for banned dependencies

# Optional maintenance check (not part of required CI or pre-commit):
cargo make check-isolated-package-builds # Check each package with default features

# Auto-fix formatting:
cargo fmt --all
cargo make format-nightly      # Also sort imports
```

> **Note:** The nightly toolchain is used only for `check-format-nightly` and
> `carbide-lints`. The stable toolchain pinned in `rust-toolchain.toml` is used
> for everything else.

### Top-level Makefile entrypoints

A top-level [`Makefile`](Makefile) at the repo root provides a thin
discoverable entrypoint for selected Core workflows and the `rest-api/` Go
services. It delegates to cargo-make or `rest-api/Makefile`.

```bash
make help                # default goal: list available targets
make core/check-isolated-package-builds # optional independent default-feature builds
make rest-build          # build rest-api Go binaries
make rest-test           # run rest-api unit tests
make rest-lint           # lint rest-api
make rest-fmt            # go fmt check on rest-api
make rest-helm-lint      # helm lint rest charts
make rest-docker-build-local
make rest-kind-reset     # spin up the local kind dev cluster (~10 min)
make rest-api/<target>   # pass any target through to rest-api/Makefile
```

## Coding Conventions

Follow the shared [Engineering Guidelines](CONTRIBUTING.md#engineering-guidelines)
for scope control, reuse-before-new-code, evidence-backed assumptions, and
verification expectations.

See [`STYLE_GUIDE.md`](STYLE_GUIDE.md) for detailed Rust coding conventions.
Make sure to review it to ensure changes meet the expected style of the codebase.

### Avoid stringly-typed values

When a value has a known, finite set of possibilities, model it with an enum (or
a struct of enums) and derive its string form via `Display`/`FromStr` — do not
pass it around as a bare `String` or `&str` literal. Stringly-typed values are
easy to misspell (`NICO-` vs `NICOO-`), silently break log filters and alerts,
and can't be exhaustively checked by the compiler. See
[`ErrorCode`](crates/api-model/src/errors.rs) for the pattern: typed
`ErrorSystem`/`ErrorSubsystem` parts plus a `code`, rendered to the wire string
in one place. Reserve raw strings for genuinely open-ended values.

## Further Reading

- [`README.md`](README.md) — Project overview and getting started
- [`STYLE_GUIDE.md`](STYLE_GUIDE.md) — Detailed Rust coding conventions
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Contribution workflow and DCO process
- [`book/src/README.md`](book/src/README.md) — Architecture and operational guides

## Cursor Cloud specific instructions

Scope note: the Cloud env setup targets the **Rust Core** (the `carbide-api`
product: build, lint, test, and run). The Go `rest-api/` subtree also
builds and its unit tests pass without Docker (see the last section). System
deps, `cargo-make`, and Go 1.26.4 are installed by the startup update script;
the Rust toolchain resolves automatically from `rust-toolchain.toml` (1.96.0).

### Postgres is a build-time dependency (sqlx)
`sqlx::query!` macros connect to a database **at compile time**, so a reachable
Postgres is required to build/lint/test — not just to run. Postgres is installed
but **not auto-started** (no systemd in the VM). Before building, start it and
make credentials match `.envrc`:

```bash
sudo pg_ctlcluster 16 main start
sudo -u postgres psql -c "ALTER USER postgres WITH PASSWORD 'admin';"
export DATABASE_URL="postgresql://postgres:admin@localhost" REPO_ROOT=/workspace
```

`direnv` is not auto-hooked in non-interactive shells, so export `DATABASE_URL`
and `REPO_ROOT` yourself (or run `direnv allow`). An empty DB is enough to
compile; `#[sqlx::test]` tests create their own ephemeral databases.

Standard build/lint/test commands are unchanged — see the sections above
(`cargo build -p carbide-api`, `cargo make clippy`, `cargo test -p <crate>`).
Full-workspace `cargo make clippy` passes here.

### Non-obvious: clang needs `libstdc++-14-dev`
The default `c++` is clang, which selects the **GCC 14** toolchain. Only
installing `libstdc++-13-dev` makes clang fail to find `cstddef` when compiling
the `carbide-dhcp` Kea C++ shim (`crates/dhcp/build.rs`). The update script
installs `libstdc++-14-dev` to fix this; keep it if you touch the deps.

### Running `carbide-api` natively (no Docker/Vault in the VM)
The canonical native runner is `dev/mac-local-dev/run-carbide-api.sh` with
`dev/mac-local-dev/carbide-api-config.toml` (has the required `[auth.trust]`
block; run from repo root so relative cert paths resolve). That script starts
Vault + Postgres via **Docker**, which is **not installed** here, and the Vault
binary/image cannot be fetched (HashiCorp domains are **egress-blocked**;
GitHub is reachable). Workaround that works in this VM:

1. Native Postgres (above) + run migrations: `./target/debug/carbide-api migrate --datastore="postgres://postgres:admin@localhost/postgres"`.
2. A tiny HTTPS **Vault stub** using the dev cert `dev/certs/localhost/localhost.{crt,key}` (signed by `dev/certs/localhost/ca.crt`, SAN `localhost`), returning 404 for KV reads and 200 for writes — enough for the server to boot.
3. Env: `VAULT_ADDR=https://localhost:8200 VAULT_TOKEN=dummy VAULT_CACERT=dev/certs/localhost/ca.crt VAULT_KV_MOUNT_LOCATION=secrets VAULT_PKI_MOUNT_LOCATION=certs VAULT_PKI_ROLE_NAME=role DISABLE_TLS_ENFORCEMENT=true UNSUPPORTED_CERTIFICATE_PROVIDER=true CARBIDE_WEB_AUTH_TYPE=none`, then `carbide-api run --config-path dev/mac-local-dev/carbide-api-config.toml`.

The gRPC API listens on `:1079` (TLS — use `grpcurl -insecure localhost:1079 list`), the admin web UI is at `https://localhost:1079/admin`, metrics on `:1080`. `grpcurl` is available; `vault` is not. Because the stub returns 404 for KV, the background **site-explorer** logs `MissingCredentials machines/bmc/site/root` — this is expected and non-fatal (real BMC creds would be pre-seeded into Vault). For a real end-to-end hardware flow you need a genuine Vault + mock BMC (machine-a-tron), which require Docker/network access this VM lacks.

### Go `rest-api/` subtree (build + unit tests work; no Docker)
Uses Go **1.26.4** (the update script installs it to `/usr/local/go`; the base
image's 1.22 is too old). `go` module downloads work (proxy reachable).

- **Build all binaries**: `cd rest-api && make build` (produces `rest-api/build/binaries/{api,workflow,sitemgr,site-agent,migrations,credsmgr,nicocli,nico-mcp}`).
- **Tests**: the `make test-*` targets call `ensure-postgres`, which starts a
  **Docker** container on port `30432` — Docker is not available here. Provide
  that Postgres yourself and run the module's `go test` directly (skip the
  `make` wrapper):

  ```bash
  # one-time: a throwaway Postgres on :30432 (trust auth) matching what tests expect
  /usr/lib/postgresql/16/bin/initdb -D /tmp/pg-resttest -U postgres --auth-host=trust --auth-local=trust
  printf "port=30432\nlisten_addresses='localhost'\nunix_socket_directories='/tmp/pg-resttest'\n" >> /tmp/pg-resttest/postgresql.conf
  /usr/lib/postgresql/16/bin/pg_ctl -D /tmp/pg-resttest -l /tmp/pg-resttest/logfile start
  psql -h localhost -p 30432 -U postgres -c "CREATE DATABASE nicotest" -c "\c nicotest" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm"
  # migrations
  (cd rest-api/db/cmd/migrations && go build -o migrations .)
  PGHOST=localhost PGPORT=30432 PGDATABASE=nicotest PGUSER=postgres PGPASSWORD=postgres rest-api/db/cmd/migrations/migrations db init_migrate
  # run a module's tests (verified: common, db, api)
  (cd rest-api/api && go test -p 1 ./... -count=1)
  ```

  Tests connect to `localhost:30432` as `postgres/postgres` (see
  `db/pkg/db/tx_test.go`); `test-flow`/`powershelf-manager`/`nvswitch-manager`
  need `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME` exported (see the Makefile).
- **Still need Docker (not covered here)**: `ipam` tests use `testcontainers-go`;
  `site-manager`/`site-agent` need `CGO_ENABLED=1 -race` and the mock gRPC
  servers (`make core-mock-server-start` / `flow-mock-server-start`); full E2E
  (`make kind-reset`) needs Kind + Temporal + Keycloak.
