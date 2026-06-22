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

Follow the shared [Engineering Guidelines](CONTRIBUTING.md#engineering-guidelines)
for scope control, reuse-before-new-code, evidence-backed assumptions, and
verification expectations.

See [`STYLE_GUIDE.md`](STYLE_GUIDE.md) for detailed Rust coding conventions.
Make sure to review it to ensure changes meet the expected style of the codebase.

## Further Reading

- [`README.md`](README.md) — Project overview and getting started
- [`STYLE_GUIDE.md`](STYLE_GUIDE.md) — Detailed Rust coding conventions
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Contribution workflow and DCO process
- [`book/src/README.md`](book/src/README.md) — Architecture and operational guides

## Cursor Cloud specific instructions

These notes capture non-obvious gotchas for working in the Cursor Cloud VM. The
system packages, Rust/cargo tools (`cargo-make`, `sccache`, `cargo-deny`,
`taplo`, `sqlx-cli`), PostgreSQL, and the GNU C++ toolchain selection below are
already provisioned in the VM image; the startup update script only runs
`cargo fetch`. Build/lint/run commands themselves are documented above — this
section only adds the environment-specific setup those commands depend on.

### PostgreSQL is required at COMPILE time, not just runtime

`crates/api-db` uses `sqlx::query!`/`query_as!` macros that connect to a live,
migrated database during compilation (there is no committed `.sqlx` offline
cache). So `cargo build`/`cargo test`/`cargo clippy` for anything that pulls in
`carbide-api-db` need a running PostgreSQL with the schema applied and
`DATABASE_URL` exported.

PostgreSQL is **not** auto-started on boot. At the start of a session:

```bash
sudo pg_ctlcluster 16 main start
export DATABASE_URL="postgresql://ubuntu@%2Fvar%2Frun%2Fpostgresql/carbide_development"
# Apply any migrations added since the image was built (idempotent):
sqlx migrate run --source crates/api-db/migrations
```

The `ubuntu` superuser role and `carbide_development` database already exist
(peer auth over the unix socket). Always export `DATABASE_URL` before
`cargo build`/`test`/`clippy`.

### C++ toolchain: use GNU, not clang

`cc`/`c++` are pinned to `gcc`/`g++` 13 via `update-alternatives`. The Kea DHCP
bindings (`crates/dhcp`, C++) and other `cc-rs` C++ builds fail under clang 18
here (`fatal error: 'cstddef'/'atomic' file not found` — clang can't locate
libstdc++ headers). If a C++ build suddenly can't find standard headers, check
`c++ --version` is GCC.

### Linting

Run clippy excluding the fuzz crate, which needs clang's libFuzzer
(unavailable here):

```bash
cargo clippy --locked --workspace --exclude carbide-ssh-console-fuzz --all-targets
```

Do **not** use `cargo make clippy` in this VM — it adds `--all-features`, which
builds `carbide-ssh-console-fuzz`/`libfuzzer-sys` and fails. CI runs clippy in a
container that has working libFuzzer.

### Tests

A whole-workspace `cargo test` pulls in `carbide-api-integration-tests`, which
spawns a `vault` dev server binary that is **not installed** (downloads from
`releases.hashicorp.com` are blocked by network egress). Run targeted crates
instead, e.g. `cargo test -p carbide-api-db` (DB-backed, uses a template DB it
creates from `DATABASE_URL`), `-p carbide-network`, `-p carbide-uuid`.

### Running carbide-api (the Core API server) without Vault

`carbide-api` normally talks to Vault, but Vault is not installed. It can still
run end-to-end using the in-memory credential store and the "unsupported"
certificate provider (the Vault client is created lazily and never contacted):

```bash
sudo mkdir -p /app && sudo ln -sfn /workspace /app/code   # config uses /app/code absolute paths
# The committed dev/docker-env/carbide-api-config.toml is MISSING the [auth.trust]
# block the listener requires, so make a local copy and append it:
cp dev/docker-env/carbide-api-config.toml /tmp/carbide-api-config.local.toml
cat >> /tmp/carbide-api-config.local.toml <<'EOF'

[auth.trust]
spiffe_trust_domain = "nico.local"
spiffe_service_base_paths = ["/forge-system/sa/", "/default/sa/"]
spiffe_machine_base_path = "/forge-system/machine/"
additional_issuer_cns = []
EOF

export CARBIDE_API_DATABASE_URL="postgres://ubuntu@%2Fvar%2Frun%2Fpostgresql/carbide_development"
export DISABLE_TLS_ENFORCEMENT=true UNSUPPORTED_CERTIFICATE_PROVIDER=true \
       CARBIDE_CREDENTIAL_STORE=memory VAULT_ADDR=http://127.0.0.1:8200 \
       VAULT_TOKEN=notforprodtoken VAULT_KV_MOUNT_LOCATION=secret \
       VAULT_PKI_MOUNT_LOCATION=unsupported VAULT_PKI_ROLE_NAME=unsupported \
       VAULT_CACERT=/workspace/dev/certs/forge_root.pem
./target/debug/carbide-api run --config-path=/tmp/carbide-api-config.local.toml
```

The server listens on `:1079` (gRPC + `/admin` web UI, TLS) and `:1080`
(Prometheus metrics, plaintext). gRPC works without a client cert because TLS
enforcement is disabled — e.g.
`grpcurl -insecure 127.0.0.1:1079 list`. `SiteExplorer` logging a
"Missing credential machines/bmc/site/root" error every interval is expected
with the memory store and is non-fatal.

### Go `rest-api`

`GOTOOLCHAIN=auto` auto-downloads the pinned Go (`go.mod`) on first use. `make
build` / `go build ./...` work. Full `make test` needs a Docker PostgreSQL
container (port 30432) and `make kind-reset` needs Docker + kind — **Docker is
not installed** in this VM, so those flows require installing Docker first. See
[`rest-api/AGENTS.md`](rest-api/AGENTS.md) for the Go service commands.
