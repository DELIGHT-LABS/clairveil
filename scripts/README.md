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
- `privacy-proverd-load-bench.sh`: summarizes external `clairveil-proverd` load against an already running prover.
- `privacy-bench-localnet.sh`: runs localnet privacy smoke and writes fee, gas, reserve, and localnet summaries.
- `privacy-localnet-tps-bench.sh`: wraps localnet smoke output as a `chain_tps` benchmark family.
- `privacy-user-latency-bench.sh`: runs localnet privacy smoke with wallet-flow latency tracing enabled and writes `privacy-user-latency` reports.
- `privacy-public-capacity-report.sh`: merges component reports into a public capacity aggregate and keeps the aggregate ineligible when any component or per-claim evidence fails the public gate.
- `release-pack.sh`: creates the downstream handoff tarball and external sha256 file under `dist/`.
- `release-pack-verify.sh`: verifies the handoff tarball checksum, internal `SHA256SUMS.txt`, required files, and manifest commit.
- `docker-proverd-build.sh`: validates the prover compose file, builds the reference prover Docker image, and inspects the image.
- `install-binaries.sh`: installs built Clairveil binaries into `GOBIN` or `GOPATH/bin`.
- `init-localnet.sh`: prepares a default local chain home for manual `clairveild start` workflows.
