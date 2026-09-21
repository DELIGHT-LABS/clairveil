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
| `make init` | builds/installs binaries and prints the required `clairveild init --audit-config ... --chain-id ...` command shape; it does not initialize a home |
| `make docs-check` | Markdown, EN/KR pairs, manifests, and prover schema fixtures |
| `make examples` | JS audit key, fixture validator, and prover HTTP client checks |
| `make privacy-batch-joinsplit-localnet` | static legacy batch fixture and SDK conformance; starts no node/prover |
| `make privacy-bulk-readiness-check` | focused unit/reservation checks and synthetic legacy capacity planning; rejects `RUN_LOCALNET` |
| `make docker-proverd-build` | Dockerfile and compose build validation |

`make release-check` runs `ci`, `vulncheck`, the static legacy batch conformance gate, and default static/unit/synthetic bulk-readiness checks. It starts no node or prover and therefore provides no live V2 or capacity evidence.

## Batch and payroll gates

| Gate | It verifies | It does not verify |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | Static legacy fixture and SDK conformance; it starts no process. | V2 runtime validation. |
| `make reference-payroll-demo` | Legacy multi-message repository-local regression. | A real node or one-proof batch transfer. |
| `make reference-payroll-rehearsal` | Legacy simulation and capacity-planning reports. | A live node, V2 runtime validation, or one-proof production capacity. |

`privacy-batch-joinsplit-localnet`, `privacy-bulk-readiness-check`, and `reference-payroll-rehearsal` reject nonzero `RUN_LOCALNET` before their work. A native CLI walkthrough is separate from these gates and requires the public audit config, matching artifact bundle, node startup, transaction execution, rescan, and auditor checks to be recorded explicitly.

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

There is no checked-in end-to-end native V2 smoke target. Keep historical localnet/latency reports and legacy load examples reference-only.

`make init` does not create a home or `clairveil.env`; it only installs binaries and prints the next command. For a manual V2 native run, follow the [getting-started initialization](clairveil-getting-started.md#initialize-and-start), choose an explicit `--home` when isolation is needed, and complete the normal Cosmos account/genesis-account/gentx/collect-gentxs preparation required by the selected chain mode before start. Pass the public audit configuration to `init/start`, the artifact directory to `start`, and point `clairveil-proverd` at the same artifacts with `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR`. The private audit key belongs to the external auditor, not node initialization.

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

Run an available relevant check whenever its commands or expected behavior changes; do not report the unavailable wrappers as passing evidence. For
remote profiles and Merkle restore/recovery boundaries, use the
[operations guide](clairveil-operations-guide.md).
