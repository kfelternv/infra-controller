# Fern artifact-closure evidence export

Evidence-only commit based on PR #6002 exact source
`ee6b607e2bc38fd593e6a4f6badf66e352a6917f` / tree
`ac9d3daae484f2f7eacea889d604eb42692f3998`.

- `raw/summary.md`: accepted diagnosis.
- `raw/*-fern-*.log` and `.status`: exact full/artifact/closure outputs and exits.
- `raw/target-matrix.tsv` and `raw/closure-checksums.txt`: five-file closure proof.
- `raw/6002-workflow-upload-excerpt.txt`: source-bound upload allowlist.
- `raw/tool-version-and-help.log`: exact effective Fern/Node version output.
- `COMMANDS.md`: exact executed check/staging commands.
- `SOURCE_HEADS.tsv`: public exact-head/tree binding for all three recovery targets.
- `HANDOFF.md`: root-owned immediate recovery versus durable shared repair.

The 70 MiB staged source copies were not duplicated here: this commit is based
on the exact tested source tree, the upload excerpt defines the partial artifact,
and the raw differential logs plus target matrix/checksums preserve the result.
No product or workflow source is changed by this evidence ref.
