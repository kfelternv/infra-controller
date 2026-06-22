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

# Auto-fix formatting:
cargo fmt --all
cargo make format-nightly      # Also sort imports
```

> **Note:** The nightly toolchain is used only for `check-format-nightly` and
> `carbide-lints`. The stable toolchain pinned in `rust-toolchain.toml` is used
> for everything else.

### Top-level Makefile (rest-api entrypoint)

A top-level [`Makefile`](Makefile) at the repo root provides a thin
discoverable entrypoint for the `rest-api/` Go services. It just
delegates to `rest-api/Makefile`.

```bash
make help                # default goal: list rest-* targets
make rest-build          # build rest-api Go binaries
make rest-test           # run rest-api unit tests
make rest-lint           # lint rest-api
make rest-fmt            # go fmt check on rest-api
make rest-helm-lint      # helm lint rest charts
make rest-docker-build-local
make rest-kind-reset     # spin up the local kind dev cluster (~10 min)
make rest-api/<target>   # pass any target through to rest-api/Makefile
```

Core (Rust) tasks are not in this Makefile; use cargo and `cargo make`
directly as documented above.

## Coding Conventions

See [`STYLE_GUIDE.md`](STYLE_GUIDE.md) for detailed Rust coding conventions.
Make sure to review it to ensure changes meet the expected style of the codebase.

## Further Reading

- [`README.md`](README.md) — Project overview and getting started
- [`STYLE_GUIDE.md`](STYLE_GUIDE.md) — Detailed Rust coding conventions
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Contribution workflow and DCO process
- [`book/src/README.md`](book/src/README.md) — Architecture and operational guides

## Cursor Cloud specific instructions

These notes are for cloud agents running in the pre-provisioned Cursor VM
snapshot (toolchain + system deps already installed; the startup update script
runs `cargo fetch` and `go mod download`). They capture non-obvious caveats
specific to this environment — they do **not** replace the standard build/test
commands documented above.

### Environment is egress-restricted: container image pulls do NOT work

Outbound TLS to container-blob CDNs is blocked (Docker Hub blobs on
`*.s3.amazonaws.com`, `mirror.gcr.io`, `ghcr.io`, and `releases.hashicorp.com`
all fail). Consequences:

- `docker pull` of public images fails (the daemon itself runs fine).
- Therefore `rest-api` `make kind-reset`, `make docker-build-local`, and the
  Makefile's docker-based `ensure-postgres` / `postgres-up` (which `make test-*`
  depend on) cannot pull `postgres:14.4-alpine` and will not work as written.
- The root `docker-compose.yml` stack and DevSpace/kind flows are likewise out
  of reach. Use the native services described below instead.

apt and the Go/Cargo module proxies are reachable, which is how the toolchain is
provisioned.

### Compiler default

`cc`/`c++` are switched (via `update-alternatives`) to `gcc`/`g++`. The image
defaults them to `clang`, which cannot find libstdc++ and breaks linking of the
C++ FFI crates (`carbide-dhcp`, `libfuzzer-sys`). Leave them on gcc; plain
`cargo build` then works with no `CC`/`CXX` overrides.

### PostgreSQL (native, not Docker)

A native PostgreSQL 16 cluster replaces the docker test DB. Start it (systemd is
not running in this VM) with:

```bash
sudo pg_ctlcluster 16 main start
```

- Auth is `trust` for local/TCP, superuser `postgres`, listening on `5432`.
- Rust tests use `DATABASE_URL=postgresql://postgres:admin@localhost` (set
  `TESTDB_USER=postgres TESTDB_PASSWORD=admin TESTDB_HOST=localhost` too); the
  `sqlx::test` harness creates throwaway databases under the `postgres`
  superuser.
- Go tests default to `localhost:30432` and `db/pkg/db/tx_test.go` hardcodes
  `30432`. Expose 5432 on 30432 with a forwarder:
  `socat TCP-LISTEN:30432,fork,reuseaddr TCP:127.0.0.1:5432 &`

### Running the Rust test suite

`cargo make correctly-execute-tests` (or `cargo test`) works once Postgres is up
and `DATABASE_URL`/`TESTDB_*` are exported. The `measured_boot` / `measurement`
state-transition tests shell out to the `tpm2` CLI (`tpm2-tools`, installed) — no
physical TPM is needed, but if `tpm2` is missing those ~6 tests fail.

### Running the Go (`rest-api`) tests

Do **not** use the `make test-*` targets (they call the docker-based
`ensure-postgres`). Run `go test` directly against the native DB, e.g.:

```bash
cd rest-api/db && PGPORT=5432 PGUSER=postgres PGPASSWORD=postgres go test -p 1 ./... -count=1
```

Packages that hardcode `30432` (e.g. `db/pkg/db`) need the socat forward above.
`make lint-go` works (`golangci-lint` v2 and `revive` are installed and on PATH;
`go vet ./...` is the quick subset).

### Running `carbide-api` locally without Vault

`carbide-api run` requires a reachable Vault at startup
(`ensure_lockdown_ikm_seeded` reads `machines/nic_lockdown_ikm/...`), and the
real Vault binary/image cannot be installed here. A minimal stand-in lives at
`/opt/nico-dev/mock_vault.py` (returns 404 on KV reads → treated as
"not found", 200 on writes). To bring the API up:

```bash
# 1. migrate the schema (uses DATABASE_URL)
DATABASE_URL=postgresql://postgres:admin@localhost ./target/debug/carbide-api migrate
# 2. fresh local TLS certs (idempotent)
(cd dev/certs/localhost && ./gen-certs.sh)
sudo mkdir -p /opt/carbide/firmware
# 3. mock vault
python3 /opt/nico-dev/mock_vault.py &
# 4. run the API from the repo root (config uses CWD-relative cert paths)
DATABASE_URL=postgresql://postgres:admin@localhost CARBIDE_WEB_AUTH_TYPE=none \
  UNSUPPORTED_CERTIFICATE_PROVIDER=true VAULT_ADDR=http://localhost:8200 \
  VAULT_KV_MOUNT_LOCATION=secret VAULT_PKI_MOUNT_LOCATION=unsupported \
  VAULT_PKI_ROLE_NAME=unsupported VAULT_TOKEN=notforprodtoken \
  VAULT_CACERT=$PWD/dev/certs/localhost/ca.crt \
  ./target/debug/carbide-api run --config-path dev/mac-local-dev/carbide-api-config.toml
```

Then gRPC is on `:1079` (mTLS), metrics on `:1080`, admin web UI on
`https://localhost:1079/admin`. Talk to it with the admin CLI (note the flags
are `--api-url`/`--root-ca-path`/`--client-cert-path`/`--client-key-path`; the
checked-in `dev/mac-local-dev/run-nico-admin-cli.sh` wrapper uses outdated flag
names):

```bash
./target/debug/nico-admin-cli --api-url https://localhost:1079 \
  --root-ca-path dev/certs/localhost/ca.crt \
  --client-cert-path dev/certs/localhost/client.crt \
  --client-key-path dev/certs/localhost/client.key version
```

Credential-backed operations only "succeed" against the mock vault (no real
secret storage); everything not requiring real secrets (version, `machine show`,
config, web UI) works normally.
