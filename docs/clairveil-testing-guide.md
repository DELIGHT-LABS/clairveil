# Clairveil Testing Guide

Korean version: [clairveil-testing-guide-kr.md](clairveil-testing-guide-kr.md)

Use this guide to select the smallest test that proves the change. For setup and the
privacy end-to-end walkthrough, use [getting started](clairveil-getting-started.md).

## Everyday and release validation

For an ordinary PR:

```bash
make ci
make vulncheck
```

Neither command needs a running `clairveild`. `make ci` runs documentation checks,
Go tests and builds, and the JS examples. For a release candidate or a broad change:

```bash
make release-check
```

| Command | Use it for |
| --- | --- |
| `make test` | all Go unit and integration tests |
| `make build` | all project binaries and build-only load tools |
| `make init` | a manual development-chain home; its default home is `~/.clairveil` |
| `make docs-check` | Markdown, EN/KR pairs, manifests, and prover schema fixtures |
| `make examples` | JS audit key, fixture validator, and prover HTTP client checks |
| `make localnet-smoke` | daemon startup from a fresh genesis |
| `make privacy-e2e-smoke` | deposit, transfer, disclosure, direct and relayed withdrawal flow |
| `make docker-proverd-build` | Dockerfile and compose build validation |

`make release-check` runs `ci`, `vulncheck`, `localnet-smoke`,
`privacy-e2e-smoke`, the **static** batch gate, and
`RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`.
It does not run the real one-proof batch gate, any `reference-payroll-*` target, or
an actual 16x32 capacity workload.

## Batch and payroll gates

| Gate | It verifies | It does not verify |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | Static BatchJoinSplit16x32 fixture and SDK conformance. | It starts neither node nor prover and produces no proof. |
| `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` | The actual local node/prover one-proof workflow. | Throughput or production capacity. |
| `make reference-payroll-demo` | Legacy multi-message repository-local regression. | A real node or one-proof batch transfer. |
| `make reference-payroll-live-localnet` | Legacy multi-message `transfer-batch` localnet regression. | The one-proof workflow or a production-capacity claim. |
| `make reference-payroll-rehearsal` | Legacy simulation, regression, and capacity-planning reports. | One-proof production capacity. |

Run the real one-proof gate separately when release or mainnet acceptance claims it:

```bash
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

A 16x32 production-capacity claim needs a tag/commit-bound artifact from the actual
16x32 workload. Record proof/sec, tx/sec, item/sec, RSS, CPU, shape distribution,
retries/replanning/manual review, circuit/artifact identity, checksums, and execution
environment. Keep synthetic and legacy payroll results labelled as such. For the
one-proof and legacy reference contracts, see
[reference-payroll README](../examples/reference-payroll/README.md).

The expensive full-shape feasibility gate is opt-in; it performs development setup
and proving and is not production artifact generation or a trusted setup:

```bash
CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v
```

The disclosure-blinding resource comparison is also opt-in:

```bash
CLAIRVEIL_RUN_JOINSPLIT_BLINDING_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestJoinSplitDisclosureBlindingSeparationResourceGate$' -count=1 -v
```

## Focused Go and API checks

Use package tests while changing their contracts:

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v
```

For deposit proving and common HTTP policy:

```bash
go test ./x/privacy/client/sdk/deposit -count=1
go test ./x/privacy/client/sdk/provertransport -count=1
go test ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/conformance -count=1
go test ./cmd/clairveil-proverd -count=1
```

The suite covers fixture shape and semantic behavior, including NoteV1, fixed
payloads, batch state/gas/rollback, scan/path snapshots, prover admission, and
HTTP errors. The normative route contract, including deposit, is the
[proverd HTTP API](clairveil-proverd-http-api.md#deposit); schema shape versus
semantic validation is described in [schemas/README.md](schemas/README.md).

## JS examples

```bash
make examples
```

The target runs this command inventory:

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/js-sdk-fixture-validator run validate
npm --prefix examples/js-sdk-prover-http-client run demo
```

## Local homes and ports

`make localnet-smoke` and `make privacy-e2e-smoke` create independent temporary
homes/work directories and do not attach to an existing `~/.clairveil` node. They
can still collide with another process using their default Tendermint/RPC service
ports. Use the relevant port overrides for an E2E run:

```bash
RPC_PORT=27657 P2P_PORT=27656 GRPC_PORT=9190 API_PORT=1417 make privacy-e2e-smoke
```

For repeatable local initialization without changing the default home:

```bash
tmp="$(mktemp -d)"
GOBIN="$tmp/bin" CLAIRVEIL_HOME="$tmp/home" make init
source "$tmp/home/clairveil.env"
"$tmp/bin/clairveild" start --home "$tmp/home"
```

`CLAIRVEIL_HOME`, `KEEP_HOME=1`, `START_SECONDS`, `CHAIN_ID`, and `CLAIRVEILD_BIN`
configure the localnet smoke. `CLAIRVEIL_E2E_WORK_DIR`, `KEEP_WORK_DIR=1`,
`CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, `CHAIN_ID`, and the port variables configure
the privacy E2E smoke. The privacy E2E flow is the automated validation counterpart
to the walkthrough now included in [getting started](clairveil-getting-started.md).

## Release pack and documentation changes

Run packaging from a clean committed tree. A publishable release also needs an
annotated exact-SemVer tag pointing at that commit:

```bash
make release-pack
make release-pack-verify
```

`release-pack-verify` checks the external and internal checksums, required handoff
files, and manifest commit. An untagged clean snapshot is packaging-CI material, not
a publishable release. For release process and ownership rules, see
[CONTRIBUTING.md](../CONTRIBUTING.md).

For documentation-only edits, run:

```bash
make docs-check
git diff --check
```

Run the relevant smoke test whenever its commands or expected behavior changes. For
remote profiles and Merkle restore/recovery boundaries, use the
[operations guide](clairveil-operations-guide.md).
