# Root action handoff: exact-head hosted Fern previews

## Finding

The accepted differential at #6002 proves an incomplete `fern-preview`
artifact: complete source passes; the workflow-shaped artifact fails with the
same eight link annotations; adding only the five omitted link targets passes.
See `raw/summary.md` and raw logs/status files.

This is a shared workflow/artifact defect, not a defect in the changed page of
#6002 and not evidence that the other feature implementations are wrong.

## Minimal safe same-head recovery (root-owned external action)

Preserve the intentional two-stage trust boundary in
`.github/workflows/fern-docs-preview-comment.yml`: the job that receives
`DOCS_FERN_TOKEN` must not check out a pull-request branch or execute repository
scripts. To unblock hosted rendering **without changing a PR head or triggering
CI**, root can recover each exact target in `SOURCE_HEADS.tsv` as follows:

1. In a non-secret source-capture stage, materialize the exact commit and verify
   both `HEAD` and `HEAD^{tree}` against `SOURCE_HEADS.tsv`.
2. Stage a source-bound docs artifact using the existing workflow paths plus
   only the repository-relative link targets required by that head:

   - `.cargo/config.toml` for #6002's older source, or
     `dev/linkers/clang-mold` for #6907/#5660's newer source
   - `.envrc`
   - `dev/docker/Dockerfile.build-artifacts-container-aarch64`
   - `CONTRIBUTING.md`
   - `helm-prereqs/README.md`

   Retain the existing `fern/`, `docs/`, `rest-api/flow/docs/`,
   `rest-api/openapi/`, `preview-metadata/`, and source-applicable `RELEASE.md`
   entries. Do not upload the full repository, use a recursive hidden-file glob,
   or include other scripts or local files.
3. Record the verified head, tree, project Fern version, and a path/content
   digest manifest in the artifact. Reject unexpected paths, links, or digest
   mismatches. The source-capture stage may run the fixed
   `fern check --local --warnings` closure check against the staged artifact; it
   has no token. Treat `.envrc`, Dockerfiles, and other referenced files as
   inert link targets: do not source or execute them.
4. In root's protected authenticated stage, download and verify only that
   allowlisted artifact. Do not check out the PR. Use the reviewed/pinned Fern
   version and execute only the fixed generator command from the artifact's
   `fern/` directory, with a unique head-bound ID:

   ```bash
   fern generate --docs --preview --force --id pr-6907-879a859b
   fern generate --docs --preview --force --id pr-6002-ee6b607e
   fern generate --docs --preview --force --id pr-5660-6fffdad6
   ```

   Capture the complete command, configured Fern version, exit, and
   `Published docs to <URL>` output. Do not expand job permissions, expose the
   token to source-capture work, or run artifact-provided shell commands.
5. Bind each URL to its verified head/tree and artifact manifest, then hand it
   to the existing independent browser reviewer/tester for the changed rendered
   pages. A URL alone is not a visual PASS. Delete previews when review no longer
   needs them, using the same protected root credentials and exact IDs.

This recovery creates hosted preview state, so it remains root-owned. It does
not require a source/head edit, CI rerun, public comment, PR metadata change, or
weaker credential isolation.

## Durable shared-workflow repair (separate disposition)

Do **not** place this baseline repair in #6907, #6002, or #5660. When an
appropriate shared-workflow change is authorized, keep the artifact allowlist
explicit and add these repository-relative dependencies:

- `.cargo/config.toml` (needed by #6002's older source)
- `.envrc`
- `dev/linkers/clang-mold` (needed by #6907/#5660's newer source)
- `dev/docker/Dockerfile.build-artifacts-container-aarch64`
- `CONTRIBUTING.md`
- `helm-prereqs/README.md`

Set `include-hidden-files: true` on the specific allowlisted
`actions/upload-artifact@v4` upload so the explicitly named dotfile dependencies
are retained. Do not pair it with a repository-wide or hidden-file glob. Prefer
a staged-artifact `fern check --local` before upload so future off-package
relative links fail in the source-bound non-secret workflow rather than the
token-bearing companion. Keep the companion artifact-only: no PR checkout,
permission expansion, or new secret exposure.

This durable repair is distinct from the same-head recovery above. It requires
shared workflow source review/publication and must wait for a proper authorized
home; it is not necessary to mutate any of the three current feature heads to
obtain exact-head hosted previews now.
