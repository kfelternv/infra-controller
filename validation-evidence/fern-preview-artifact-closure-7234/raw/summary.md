# Fern preview artifact-closure diagnosis — task 7234

## Scope and provenance

- Repository: `dsx-ai-factory/infra-controller`
- PR: `#6002`
- Exact head: `ee6b607e2bc38fd593e6a4f6badf66e352a6917f`
- Tree: `ac9d3daae484f2f7eacea889d604eb42692f3998`
- Tool: pinned Fern CLI `5.114.1`; Node `v24.21.0`; no `FERN_TOKEN`
- Source remained clean; no source edits, commits, publication, CI polling, compiler/build, or deployment.

At this exact head, `.github/workflows/fern-docs-preview-build.yml` uploads only `fern/`, `docs/`, `rest-api/flow/docs/`, `rest-api/openapi/`, and `preview-metadata/`. (The later `6fffdad` workflow additionally uploads `RELEASE.md`; it still does not include the external targets relevant to its eight errors.)

## Differential reproduction

An artifact-like tree was constructed by copying exactly the #6002 workflow inputs and synthetic metadata, preserving their repository-relative paths. Exact logs are adjacent to this summary.

| Input | Command | Exit | Result |
|---|---|---:|---|
| Complete exact-head checkout | `fern check --warnings` | 0 | 0 errors, 1 expected unauthenticated redirect warning |
| Artifact-like tree | `fern check --warnings` | 1 | 8 broken-link errors, exactly matching the #6002 annotations |
| Complete exact-head checkout | `fern check --local --warnings` | 0 | 0 errors, 1 expected unauthenticated redirect warning |
| Artifact-like tree | `fern check --local --warnings` | 1 | Same 8 broken-link errors |
| Complete exact-head checkout | `fern docs md check` | 0 | All 144 MDX files valid |
| Artifact-like tree | `fern docs md check` | 0 | All 144 MDX files valid; this command does not prove link closure |
| Artifact-like tree plus the five omitted target files | `fern check --local --warnings` | 0 | 0 errors, 1 expected unauthenticated redirect warning |

The five target files are present in the complete source and absent in the uploaded input:

- `.cargo/config.toml` (one link at this older #6002 head)
- `.envrc` (one link)
- `dev/docker/Dockerfile.build-artifacts-container-aarch64` (one link)
- `CONTRIBUTING.md` (one link)
- `helm-prereqs/README.md` (four links)

Fern's package-root link resolver emits the same resolved paths and source line/column locations as the hosted annotations. Adding those exact five files to the otherwise identical package changes only the closure and makes the same pinned resolver pass. Therefore the eight errors are caused by omitted artifact dependencies, not unresolved navigation/anchors in the complete source and not PR #6002's changed page.

At `6fffdad`, the first build-guide dependency changed from `.cargo/config.toml` to `dev/linkers/clang-mold`; lead independently established the other five-source set at that head. A correction intended to cover both frozen heads should include both paths.

## Minimal correction recommendation

Fix the shared preview build artifact, not PR #6002's changed documentation:

1. Add the repository-relative link targets to the `fern-preview` upload: `.cargo/config.toml`, `.envrc`, `dev/linkers/clang-mold`, `dev/docker/Dockerfile.build-artifacts-container-aarch64`, `CONTRIBUTING.md`, and `helm-prereqs/README.md`.
2. Set `include-hidden-files: true` on `actions/upload-artifact@v4`, because two required targets are dotfiles; keep the upload allowlist explicit rather than uploading the repository wholesale.
3. Add a pre-upload artifact-closure check (the same pinned `fern check --local`) against the staged artifact tree, so future off-package relative links fail in the source-bound build before the token-bearing companion workflow.

Equivalent alternative: change these repository-file links to stable public repository URLs. That avoids expanding the artifact, but modifies multiple unrelated docs and is less minimal than repairing the incomplete artifact contract.

No authenticated failed-job stdout is needed for this causal finding: the public annotations were exactly reproduced locally and the closure-only repair passed. A token-bearing hosted run remains necessary only to create and visually inspect the actual preview after the shared workflow is corrected.

## Raw evidence

- `provenance.txt`
- `target-matrix.tsv`
- `full-fern-check.log` / `.status`
- `packaged-fern-check.log` / `.status`
- `full-fern-check-local.log` / `.status`
- `packaged-fern-check-local.log` / `.status`
- `full-fern-md-check.log` / `.status`
- `packaged-fern-md-check.log` / `.status`
- `packaged-with-link-closure-fern-check-local.log` / `.status`
- `closure-checksums.txt`
