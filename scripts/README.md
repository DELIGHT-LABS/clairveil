# Scripts

This directory contains repeatable script entrypoints for Clairveil development, validation, and release handoff.

Korean version: [README-kr.md](README-kr.md)

## Script List

- `docs-check.sh`: validates tracked and new working-tree Markdown links and fragments through the CommonMark/GFM AST, top-level documentation language pairs, complete documentation/plan indexes, HEAD-reachable exact-SemVer annotated commit tags/real changelog headings, document placement, release-pack manifests, and packed-link closure; it excludes `examples/clairveil-dapp/**`.
- `markdown-ast.go`: provides the Goldmark CommonMark/GFM link and heading AST consumed by documentation and release checks; its unit test covers multiline references, nested parentheses, fragments, comments, and code fences.
- `generate-proto.sh`: regenerates privacy protobuf and gRPC Gateway Go files from `proto/clairveil/privacy/v1`.
- `govulncheck-with-policy.sh`: runs `govulncheck` and applies the repository vulnerability exception policy.
- `localnet-smoke.sh`: builds `clairveild`, creates a temporary local validator genesis, applies `RPC_PORT`, `P2P_PORT`, `ABCI_PORT`, `GRPC_PORT`, `API_PORT`, and `PPROF_PORT` overrides, starts the node briefly, and verifies block commit.
- `privacy-e2e-smoke.sh`: validates the full local privacy flow: deposit, transfer, disclosure decode, direct withdraw, and relayed withdraw.
- `privacy-batch-joinsplit-localnet.sh`: validates the batch reference integration 16x32 fixture by default; with `RUN_LOCALNET=1`, starts a real node and `clairveil-proverd` and executes 1/1, 3/4 mixed disclosure, 31+change, exact32, padding, real process restart/retry, non-zero-height genesis export/import, typed cursor/path continuation, and reserve/asset/wallet round trips. It requires `grpcurl`. Set `CLAIRVEIL_BATCH_ARTIFACT_DIR` to reuse a previously verified development artifact directory instead of generating another setup.
- `privacy-bench.sh`: runs privacy circuit benchmarks and writes structured JSON/Markdown reports.
- `privacy-proverd-bench.sh`: runs in-process prover HTTP transport benchmarks.
- `privacy-proverd-load-bench.sh`: summarizes external `clairveil-proverd` load against one already running prover via `PROVERD_URL`, or a round-robin prover pool via `PROVERD_URLS`. Set `PROVERLOAD_ALLOW_UNHEALTHY_ENDPOINTS=1` to exclude endpoints that fail preflight while recording unhealthy endpoint counts.
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
- `init-localnet.sh`: prepares a default local chain home for manual `clairveild start` workflows.
