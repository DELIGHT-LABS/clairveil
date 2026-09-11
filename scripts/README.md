# Scripts

This directory contains repeatable script entrypoints for Clairveil development, validation, and release handoff.

Korean version: [README-kr.md](README-kr.md)

## Script List

The current localnet, E2E, and prover-load entries below use the reviewed V2 runtime. Remaining legacy entries are explicitly labelled static, historical, or capacity-planning only.

- `docs-check.sh`: validates tracked and new working-tree Markdown links and fragments through the CommonMark/GFM AST, top-level documentation language pairs and index, the plan archive boundary, HEAD-reachable exact-SemVer annotated commit tags/real changelog headings, document placement, release-pack manifests, and packed-link closure.
- `markdown-ast.go`: provides the Goldmark CommonMark/GFM link and heading AST consumed by documentation and release checks; its unit test covers multiline references, nested parentheses, fragments, comments, and code fences.
- `generate-proto.sh`: regenerates privacy protobuf and gRPC Gateway Go files from `proto/clairveil/privacy/v1`.
- `govulncheck-with-policy.sh`: runs `govulncheck` and applies the repository vulnerability exception policy.
- `localnet-smoke.sh` and `privacy-e2e-smoke.sh`: stable entrypoints for the reviewed audit-field V2 runner. They require `CLAIRVEILD_BIN`, `CLAIRVEIL_PROVERD_BIN`, matching `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR`, `CLAIRVEIL_AUDIT_RUNTIME_DIR`, and `CLAIRVEIL_AUDIT_SECRET_FILE`; they verify V2 DeliverTx and forced rescans.
- `privacy-batch-joinsplit-localnet.sh`: keeps its default static fixture/conformance gate. `RUN_LOCALNET=1` runs the same reviewed V2 runner with one small `transfer-batch-16x32` proof, not a 16x32 capacity workload.
- `privacy-bench.sh`: runs privacy circuit benchmarks and writes structured JSON/Markdown reports.
- `privacy-proverd-bench.sh`: runs in-process prover HTTP transport benchmarks.
- `privacy-proverd-load-bench.sh`: defaults to `audit_field_only`. Set `PROVERLOAD_AUDIT_REQUEST` to a complete V2 witness request and never retain or log it; the tool validates HTTP response version/circuit/artifact/PI23 framing, while the CLI smoke performs exact-artifact cryptographic verification.
- `privacy-proverd-scale-bench.sh`: runs the external prover load benchmark with pool-oriented defaults and writes `privacy-proverd-scale` reports. Requires comma-separated `PROVERD_URLS` and enables unhealthy endpoint exclusion by default; public-claim eligibility still requires `unhealthy_endpoint_count=0`.
- `privacy-bench-localnet.sh`: runs localnet privacy smoke and writes fee, gas, reserve, and localnet summaries.
- `privacy-localnet-tps-bench.sh`: wraps localnet smoke output as a `chain_tps` benchmark family.
- `privacy-transfer-batch-localnet-bench.sh`: runs the localnet smoke with an extra multi-message `transfer-batch` tx enabled. Set `TRANSFER_BATCH_COUNT=N` and `TRANSFER_BATCH_AMOUNT=A` to vary the batch envelope.
- `privacy-bulk-transfer-bench.sh`: generates synthetic bulk payroll throughput summaries for chunk size, prover count, and tx/sec planning.
- `reference-payroll-demo.sh`: runs the reference payroll product against local file state through the simulated daemon.
- `reference-payroll-live-localnet.sh`: runs payroll input, reservation, `transfer-batch`, recipient scan, settle, and final report against a real localnet.
- `reference-payroll-rehearsal.sh`: generates reference payroll capacity simulation reports for 1k, 10k, 100k, and 100 companies x 1k profiles, with an optional small live localnet smoke.
- `reservation-sql-integration.sh`: exercises the payroll reservation store against SQLite and PostgreSQL; it uses `CLAIRVEIL_TEST_POSTGRES_DSN` or starts a temporary PostgreSQL 17 Docker container.
- `privacy-bulk-readiness-check.sh`: runs bulk-transfer production-readiness checks, including critical unit tests, reservation failure invariants, synthetic bulk bench, and optional localnet/prover-pool checks.
- `privacy-user-latency-bench.sh`: runs localnet privacy smoke with wallet-flow latency tracing enabled and writes `privacy-user-latency` reports. Set `USER_LATENCY_REPEAT=N` to collect repeated samples; `RUN_PROFILE=public_claim` requires at least 100 repeats unless explicitly overridden for a blocked dry run.
- `privacy-public-capacity-report.sh`: merges component reports into a public capacity aggregate and keeps the aggregate ineligible when any component or per-claim evidence fails the public gate. When both prover reports exist, the default input set prefers `privacy-proverd-scale` over `privacy-proverd-load` to avoid conflicting alternative `prover_rps` evidence.
- `privacy-benchmark-report.sh`: merges family `latest.json` reports into one human-readable `benchmarks/clairveil-benchmark-results-report-kr.md` summary. `privacy-public-capacity-report.sh` calls it by default at the end; set `GENERATE_HUMAN_BENCHMARK_REPORT=0` to disable that.
- `release-pack.sh`: creates the downstream handoff tarball and external sha256 file under `dist/`, with deterministic Git-derived file and canonical directory/metadata modes independent of caller umask. Final releases require an annotated exact-SemVer tag at the packed commit; untagged clean commits use a non-publishable `snapshot-<full-sha>` identity for CI.
- `release-pack-verify.sh`: verifies tag-or-snapshot commit binding, paired changelog headings for release tags, canonical/safe raw tar members, the exact selected Git file set, canonical manifest, checksums, required files, and every packed Git blob with its exact raw and extracted Git-derived permission mode. Default verification requires a clean tree and reuses an existing archive/checksum pair; explicit inputs must already exist, are never regenerated, and require an exact lowercase 40-character `RELEASE_PACK_EXPECTED_COMMIT`.
- `prepare-joinsplit-artifact-rotation-evidence.sh`: from a clean tree, prepares previous/current JoinSplit artifact directories outside the repository for source-bound rotation evidence.
- `validate-joinsplit-artifact-rotation-evidence.sh`: runs the exact JoinSplit artifact-rotation, fresh-genesis, and regression evidence gates using supplied or freshly prepared artifact sets.
- `docker-proverd-build.sh`: validates the prover compose file, builds the reference prover Docker image, and inspects the image.
- `install-binaries.sh`: installs six built project binaries (`clairveild`, `clairveil-setup`, legacy-only `clairveil-verify`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`) into `GOBIN` or `GOPATH/bin`; the verifier is not part of the current typed-note flow.
- `init-localnet.sh`: prepares a V2 fresh-genesis home from an existing reviewed runtime bundle, matching artifacts, and an offline audit-secret file; it does not generate keys, artifacts, or runtime archives.
