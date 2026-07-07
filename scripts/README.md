# Scripts

This directory contains repeatable script entrypoints for Clairveil development, validation, and release handoff.

Korean version: [README-kr.md](README-kr.md)

## Script List

- `generate-proto.sh`: regenerates privacy protobuf and gRPC Gateway Go files from `proto/clairveil/privacy/v1`.
- `govulncheck-with-policy.sh`: runs `govulncheck` and applies the repository vulnerability exception policy.
- `localnet-smoke.sh`: builds `clairveild`, creates a temporary local validator genesis, starts the node briefly, and verifies block commit.
- `privacy-e2e-smoke.sh`: validates the full local privacy flow: deposit, transfer, disclosure decode, direct withdraw, and relayed withdraw.
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
- `privacy-bulk-readiness-check.sh`: runs bulk-transfer production-readiness checks, including critical unit tests, reservation failure invariants, synthetic bulk bench, and optional localnet/prover-pool checks.
- `privacy-user-latency-bench.sh`: runs localnet privacy smoke with wallet-flow latency tracing enabled and writes `privacy-user-latency` reports. Set `USER_LATENCY_REPEAT=N` to collect repeated samples; `RUN_PROFILE=public_claim` requires at least 100 repeats unless explicitly overridden for a blocked dry run.
- `privacy-public-capacity-report.sh`: merges component reports into a public capacity aggregate and keeps the aggregate ineligible when any component or per-claim evidence fails the public gate. When both prover reports exist, the default input set prefers `privacy-proverd-scale` over `privacy-proverd-load` to avoid conflicting alternative `prover_rps` evidence.
- `privacy-benchmark-report.sh`: merges family `latest.json` reports into one human-readable `benchmarks/clairveil-benchmark-results-report-kr.md` summary. `privacy-public-capacity-report.sh` calls it by default at the end; set `GENERATE_HUMAN_BENCHMARK_REPORT=0` to disable that.
- `release-pack.sh`: creates the downstream handoff tarball and external sha256 file under `dist/`.
- `release-pack-verify.sh`: verifies the handoff tarball checksum, internal `SHA256SUMS.txt`, required files, and manifest commit.
- `docker-proverd-build.sh`: validates the prover compose file, builds the reference prover Docker image, and inspects the image.
- `install-binaries.sh`: installs built Clairveil binaries into `GOBIN` or `GOPATH/bin`.
- `init-localnet.sh`: prepares a default local chain home for manual `clairveild start` workflows.
