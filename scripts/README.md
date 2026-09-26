# Scripts

This directory contains repeatable script entrypoints for Clairveil development, validation, and release handoff.

Korean version: [README-kr.md](README-kr.md)

## Script List

The audit-field prover-load entries below target the V2 contract. Legacy entries are explicitly labelled static, historical, or capacity-planning only. No checked-in script currently claims to provide an end-to-end native V2 node smoke.

- `docs-check.sh`: validates tracked and new working-tree Markdown links and fragments through the CommonMark/GFM AST, top-level documentation language pairs and index, the plan archive boundary, HEAD-reachable exact-SemVer annotated commit tags/real changelog headings, document placement, release-pack manifests, and packed-link closure.
- `markdown-ast.go`: provides the Goldmark CommonMark/GFM link and heading AST consumed by documentation and release checks; its unit test covers multiline references, nested parentheses, fragments, comments, and code fences.
- `generate-proto.sh`: regenerates privacy protobuf and gRPC Gateway Go files from both `proto/clairveil/privacy/v1` and `proto/clairveil/privacy/v2`.
- `govulncheck-with-policy.sh`: runs `govulncheck` and applies the repository vulnerability exception policy.
- `privacy-batch-joinsplit-localnet.sh`: validates the static legacy batch fixture and SDK conformance without starting a node or prover. Any nonzero `RUN_LOCALNET` is rejected before validation; live V2 evidence requires a separate, explicitly documented native harness.
- `privacy-bench.sh`: runs privacy circuit benchmarks and writes structured JSON/Markdown reports.
- `privacy-proverd-bench.sh`: runs in-process prover HTTP transport benchmarks.
- `privacy-proverd-load-bench.sh`: defaults to `audit_field_only`. Set `PROVERLOAD_AUDIT_REQUEST` to a complete V2 witness request and never retain or log it; the tool validates HTTP response version/circuit/artifact/PI23 framing, while the current transaction CLI performs exact-artifact cryptographic verification.
- `privacy-proverd-scale-bench.sh`: runs the external prover load benchmark with pool-oriented defaults and writes `privacy-proverd-scale` reports. Requires comma-separated `PROVERD_URLS` and enables unhealthy endpoint exclusion by default; public-claim eligibility still requires `unhealthy_endpoint_count=0`.
- `privacy-bulk-transfer-bench.sh`: generates synthetic bulk payroll throughput summaries for chunk size, prover count, and tx/sec planning.
- `reference-payroll-demo.sh`: runs the reference payroll product against local file state through the simulated daemon.
- `reference-payroll-rehearsal.sh`: generates legacy capacity-planning simulations for 1k, 10k, 100k, and 100 companies x 1k profiles. It rejects nonzero `RUN_LOCALNET` and produces no live evidence.
- `reservation-sql-integration.sh`: exercises the payroll reservation store against SQLite and PostgreSQL; it uses `CLAIRVEIL_TEST_POSTGRES_DSN` or starts a temporary PostgreSQL 17 Docker container.
- `privacy-bulk-readiness-check.sh`: runs critical unit/reservation checks and the synthetic bulk planning benchmark. It rejects nonzero `RUN_LOCALNET`; optional external prover-scale validation remains available through `RUN_PROVER_SCALE=1` and `PROVERD_URLS`.
- `privacy-public-capacity-report.sh`: merges explicitly supplied component reports, or defaults only to a current prover scale/load report. Removed V1 localnet/latency report families are never selected automatically.
- `privacy-benchmark-report.sh`: merges current default families into one human-readable `benchmarks/clairveil-benchmark-results-report-kr.md` summary. Historical report files can still be inspected by passing `SUMMARY_REPORTS` explicitly.
- `release-pack.sh`: creates the downstream handoff tarball and external sha256 file under `dist/`, with deterministic Git-derived file and canonical directory/metadata modes independent of caller umask. Final releases require an annotated exact-SemVer tag at the packed commit; untagged clean commits use a non-publishable `snapshot-<full-sha>` identity for CI.
- `release-pack-verify.sh`: verifies tag-or-snapshot commit binding, paired changelog headings for release tags, canonical/safe raw tar members, the exact selected Git file set, canonical manifest, checksums, required files, and every packed Git blob with its exact raw and extracted Git-derived permission mode. Default verification requires a clean tree and reuses an existing archive/checksum pair; explicit inputs must already exist, are never regenerated, and require an exact lowercase 40-character `RELEASE_PACK_EXPECTED_COMMIT`.
- `prepare-joinsplit-artifact-rotation-evidence.sh`: historical JoinSplit rotation evidence tooling; its legacy setup flags are unavailable in the current checkout. Reproduce only with the corresponding historical source.
- `validate-joinsplit-artifact-rotation-evidence.sh`: historical JoinSplit artifact-rotation, fresh-genesis, and regression gates. It cannot prepare artifacts with the current setup command and does not validate the current audit-field circuit set.
- `docker-proverd-build.sh`: validates the prover compose file, builds the reference prover Docker image, and inspects the image.
- `install-binaries.sh`: installs seven built project binaries (`clairveild`, `clairveil-setup`, legacy-only `clairveil-verify`, `clairveil-auditor`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`) into `GOBIN` or `GOPATH/bin`; the verifier is not part of the current audit-field flow.
