# Exact accepted diagnostic commands

These are the commands already executed in task 7234. They are retained for
provenance only and were **not rerun** while exporting this evidence.

Tool query, executed from the exact #6002 checkout:

```bash
export PATH=/home/developer/workspace/.tools-5660-contract/node/bin:/home/developer/workspace/.tools-5660-contract/npm-global/bin:$PATH
printf 'node='; node --version
printf 'fern='; fern --version
fern --help | sed -n '1,180p'
```

Observed exit `0`; the raw output is `raw/tool-version-and-help.log`:
Fern `5.114.1`, Node `v24.21.0`. `fern/fern.config.json` at the tested tree pins
`5.114.1`. A separate outer-shell provenance query outside the project directory
reported the installed launcher as `5.144.3`; it is intentionally not used as
the effective project version. The checks ran after changing into trees that
contain the `5.114.1` project pin.

The exact check wrapper executed these commands in the indicated trees and
wrote each exit to the adjacent `.status` file:

```bash
(cd /home/developer/workspace/.wt-6002-ee6b; fern check --warnings)
(cd /home/developer/workspace/evidence/fern-artifact-closure-7234/packaged; fern check --warnings)
(cd /home/developer/workspace/.wt-6002-ee6b; fern check --local --warnings)
(cd /home/developer/workspace/evidence/fern-artifact-closure-7234/packaged; fern check --local --warnings)
(cd /home/developer/workspace/.wt-6002-ee6b; fern docs md check)
(cd /home/developer/workspace/evidence/fern-artifact-closure-7234/packaged; fern docs md check)
(cd /home/developer/workspace/evidence/fern-artifact-closure-7234/packaged-with-link-closure; fern check --local --warnings)
```

The artifact-like tree was staged with the exact #6002 workflow allowlist:

```bash
mkdir -p "$PKG/rest-api/flow" "$PKG/rest-api" "$PKG/preview-metadata"
cp -a "$SRC/fern" "$PKG/"
cp -a "$SRC/docs" "$PKG/"
cp -a "$SRC/rest-api/flow/docs" "$PKG/rest-api/flow/"
cp -a "$SRC/rest-api/openapi" "$PKG/rest-api/"
printf '6002\n' > "$PKG/preview-metadata/pr_number"
printf 'pull-request/6002\n' > "$PKG/preview-metadata/head_ref"
```

The closure-only variant copied exactly the five omitted #6002 targets:

```bash
mkdir -p "$FIX/.cargo" "$FIX/dev/docker" "$FIX/helm-prereqs"
cp -a "$SRC/.cargo/config.toml" "$FIX/.cargo/config.toml"
cp -a "$SRC/.envrc" "$FIX/.envrc"
cp -a "$SRC/dev/docker/Dockerfile.build-artifacts-container-aarch64" "$FIX/dev/docker/"
cp -a "$SRC/CONTRIBUTING.md" "$FIX/"
cp -a "$SRC/helm-prereqs/README.md" "$FIX/helm-prereqs/"
```

No Fern token was present. No hosted preview, publication, CI trigger, source
edit, compiler build, or deployment occurred during the diagnosis.
