#
# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Top-level Makefile for infra-controller.
#
# This is a thin, discoverable entrypoint that delegates to the native build
# tool for each component:
#
#   * Rest (Go services under rest-api/) is built via rest-api/Makefile.
#   * Core (Rust workspace) is built via cargo and cargo-make.
#
# Each component's own build files (rest-api/Makefile, Makefile.toml,
# Makefile-build.toml, Makefile-package.toml) continue to work directly;
# this file is an additive convenience layer that gives CI and users a
# single entrypoint for the most common operations.
#
# Run `make help` (default goal) for an inventory of available targets.

SHELL := /bin/bash

.DEFAULT_GOAL := help

# =============================================================================
# Help (default goal)
# =============================================================================

.PHONY: help
help: ## Show this help and exit (default goal)
	@echo "infra-controller top-level entrypoints. Run 'make <target>'."
	@echo ""
	@echo "Combined (runs rest + core):"
	@grep -E '^(build|test|lint|fmt|clean):.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "} {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Rest (Go services in rest-api/):"
	@grep -E '^rest-[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "} {printf "  %-22s %s\n", $$1, $$2}'
	@echo "  rest-api/<target>      Pass any target through to rest-api/Makefile"
	@echo ""
	@echo "Core (Rust crates):"
	@grep -E '^core-[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "} {printf "  %-22s %s\n", $$1, $$2}'
	@echo ""
	@echo "Discoverability:"
	@echo "  cargo make --list-all-steps    Full cargo-make task inventory (Rust)"
	@echo "  cat rest-api/Makefile          See rest-api/ targets directly"

# =============================================================================
# Combined targets (rest + core)
# =============================================================================

.PHONY: build test lint fmt clean

build: core-build rest-build ## Build all rest Go binaries and run cargo build for core

test: core-test rest-test ## Run rest unit tests and cargo test for core

lint: core-lint rest-lint ## Lint rest (go vet + golangci-lint + revive) and core (clippy)

fmt: core-fmt rest-fmt ## Format both rest (go fmt) and core (cargo fmt)

clean: rest-clean ## Clean rest build artifacts and test containers (cargo target/ is preserved; run 'cargo clean' manually if you really want to nuke it)

# =============================================================================
# Rest (Go services in rest-api/)
# =============================================================================

.PHONY: rest-build rest-test rest-lint rest-fmt rest-clean \
        rest-docker-build rest-docker-build-local rest-helm-lint

rest-build: ## Build all rest-api Go binaries into rest-api/build/binaries/
	$(MAKE) -C rest-api build

rest-test: ## Run all rest-api unit tests (auto-manages postgres + mock servers)
	$(MAKE) -C rest-api test

rest-lint: ## Lint rest-api: go vet + golangci-lint + revive
	$(MAKE) -C rest-api lint-go

rest-fmt: ## go fmt check on rest-api (fails if tree changed)
	$(MAKE) -C rest-api fmt-go

rest-clean: ## Tear down test postgres, mocks, kind, and remove rest build artifacts
	$(MAKE) -C rest-api clean

rest-docker-build: ## Build production docker images for rest services
	$(MAKE) -C rest-api docker-build

rest-docker-build-local: ## Build local-dev docker images for rest services
	$(MAKE) -C rest-api docker-build-local

rest-helm-lint: ## helm lint the rest umbrella and site-agent charts
	$(MAKE) -C rest-api helm-lint

# Pattern-rule escape hatch: pass ANY target through to rest-api/Makefile.
# Usage:
#   make rest-api/test-api
#   make rest-api/kind-reset
#   make rest-api/generate-sdk
rest-api/%:
	$(MAKE) -C rest-api $*

# =============================================================================
# Core (Rust crates)
# =============================================================================

.PHONY: core-build core-test core-lint core-fmt core-verify

core-build: ## cargo build (all workspace crates)
	cargo build

core-test: ## cargo test (all workspace crates)
	cargo test

core-lint: ## Run clippy on the core workspace
	cargo make clippy

core-fmt: ## cargo fmt --all
	cargo fmt --all

core-verify: ## cargo make pre-commit-verify (full CI shape, requires nightly toolchain setup)
	cargo make pre-commit-verify
