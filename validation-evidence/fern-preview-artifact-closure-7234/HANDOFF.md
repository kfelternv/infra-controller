# Root action handoff: exact-head hosted Fern previews

## Finding

The accepted differential at #6002 proves an incomplete `fern-preview`
artifact: complete source passes; the workflow-shaped artifact fails with the
same eight link annotations; adding only the five omitted link targets passes.
See `raw/summary.md` and raw logs/status files.

This is a shared workflow/artifact defect, not a defect in the changed page of
#6002 and not evidence that the other feature implementations are wrong.

## Minimal safe same-head recovery (root-owned external action)

To unblock hosted rendering **without changing any PR head or triggering CI**,
root can create one authenticated Fern preview from a protected full-source
checkout for each exact target in `SOURCE_HEADS.tsv`:

1. Materialize the exact commit in a clean scratch checkout and verify both
   `HEAD` and `HEAD^{tree}` against `SOURCE_HEADS.tsv`.
2. Install/use the Fern version pinned by that exact checkout's
   `fern/fern.config.json`.
3. Inject `FERN_TOKEN` only in root's protected execution environment. From the
   full checkout's `fern/` directory run, with a unique head-bound ID:

   ```bash
   fern generate --docs --preview --force --id pr-6907-879a859b
   fern generate --docs --preview --force --id pr-6002-ee6b607e
   fern generate --docs --preview --force --id pr-5660-6fffdad6
   ```

   Run each command in its corresponding full-source checkout, not from the
   downloaded partial CI artifact. Capture the complete command, configured
   Fern version, exit, and `Published docs to <URL>` output.
4. Bind each URL to its verified head/tree in the evidence record, then hand it
   to the existing independent browser reviewer/tester for the changed rendered
   pages. A URL alone is not a visual PASS. Delete previews when review no longer
   needs them, using the same protected root credentials and exact IDs.

This recovery creates hosted preview state, so it remains root-owned. It does
not require a source/head edit, CI rerun, public comment, or PR metadata change.

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

Set `include-hidden-files: true` on `actions/upload-artifact@v4`, otherwise the
dotfile dependencies remain omitted. Prefer a staged-artifact
`fern check --local` before upload so future off-package relative links fail in
the source-bound non-secret workflow rather than the token-bearing companion.

This durable repair is distinct from the same-head recovery above. It requires
shared workflow source review/publication and must wait for a proper authorized
home; it is not necessary to mutate any of the three current feature heads to
obtain exact-head hosted previews now.
