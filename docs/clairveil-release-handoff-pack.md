# Clairveil Release Handoff Pack

This document collects the artifacts and validation steps needed when handing a Clairveil release to downstream chain, JS/TS SDK, web wallet, and prover operations teams.

Clairveil provides a reusable privacy core and reference host. The downstream project owns production-chain EVM integration, policy modules, precompiles, validator operations, audit private key custody, wallet storage encryption, and remote prover exposure policy.

Korean version: [clairveil-release-handoff-pack-kr.md](clairveil-release-handoff-pack-kr.md)

## 1. Artifacts Release Recipients Should Receive

| Item | File/Path | Recipient | Purpose |
| --- | --- | --- | --- |
| Go module | `go.mod`, `x/privacy`, `app`, `cmd/clairveild` | Core chain team | baseline for downstream Cosmos SDK app import or fork |
| Proto | `proto/clairveil/privacy/v1` | Core chain team, JS SDK team | tx/query type generation |
| SDK fixtures | `x/privacy/client/sdk/conformance/testdata` | JS SDK team, web wallet team | wallet/prover/query contract conformance |
| JSON Schema | `docs/schemas/clairveil-js-wallet-contract.schema.json` | JS SDK team, web wallet team | machine-readable fixture shape validation |
| Prover service | `cmd/clairveil-proverd`, `x/privacy/client/sdk/proverservice`, `x/privacy/client/sdk/provertransport` | Prover operations, JS SDK team | local/remote companion prover contract |
| ZK artifact tooling | `cmd/clairveil-setup`, `cmd/clairveil-verify`, `x/privacy/zk` | Core chain team, prover operations | artifact generation, checksum, preflight |
| Walkthrough | `docs/clairveil-local-privacy-walkthrough.md` | Integrators | local end-to-end manual verification |
| Circuit guide | `docs/clairveil-circuits.md` | Core chain team, prover operations, security reviewers | Deposit/Spend/JoinSplit circuit and artifact impact explanation |
| CLI reference | `docs/clairveil-cli-reference.md` | Integrators, wallet/SDK teams | user-facing commands and flags |
| Testing guide | `docs/clairveil-testing-guide.md` | Maintainers, integrators | test matrix and release validation commands |
| Operations guide | `docs/clairveil-operations-guide.md` | Operators, security reviewers | node/prover/artifact/Merkle/audit operations baseline |
| Privacy accounting design note | `docs/clairveil-privacy-accounting-design-note.md` | Core chain team, security reviewers | deposit binding, amount bounds, reserve invariant, and artifact contract rationale |
| Maintainer instructions | `docs/clairveil-maintainer-instructions.md` | Maintainers | documentation and validation rules by change type |
| Integration guide | `docs/clairveil-downstream-cosmos-integration-guide.md` | Core chain team | app wiring and responsibility checklist |
| Client product brief | `docs/clairveil-client-product-brief.md` | Wallet/app product and client teams | product capability scope and client profiles |
| Client UX flows | `docs/clairveil-client-ux-flows.md` | Wallet/app product and client teams | setup, scan, transfer, withdraw, disclosure, and recovery flows |
| Client risk decisions | `docs/clairveil-client-risk-decisions.md` | Product, security, operations | storage, prover, audit, disclosure, and telemetry decisions |
| Client API checklist | `docs/clairveil-client-api-checklist.md` | Client SDK and app teams | chain/prover API, fixture, release gate, and compatibility checks |
| JS SDK handoff | `docs/clairveil-js-sdk-handoff.md` | JS SDK team, web wallet team | SDK implementation checklist |
| Release policy | `docs/clairveil-release-versioning-policy.md`, `docs/clairveil-release-note-template.md` | Maintainers, release recipients | tag, changelog, release note, compatibility impact rules |
| Prover profile | `docs/clairveil-proverd-remote-production-profile.md` | Prover operations | remote prover production controls |
| Merkle restore SOP | `docs/clairveil-merkle-restore-sop.md` | Core chain team, operators | tree state validation after snapshot/restore/migration |
| Security docs | `docs/clairveil-threat-model.md`, `docs/clairveil-security-best-practices-review.md` | Security reviewers, operators | trust boundary and residual risk review |

## 2. Pre-Release Repository Maintainer Validation

Before creating a release tag, the maintainer runs:

```bash
make release-check
make release-pack
make release-pack-verify
```

`make release-check` runs the following steps:

```text
make ci
make vulncheck
make localnet-smoke
make privacy-e2e-smoke
```

Each step means:

| Step | Meaning |
| --- | --- |
| `make ci` | Verifies Go tests, Go binary builds, and JS/TS examples. |
| `make vulncheck` | Runs the govulncheck policy gate. New actionable vulnerabilities fail the check. |
| `make localnet-smoke` | Confirms the reference daemon can init and start from genesis. |
| `make privacy-e2e-smoke` | Verifies deposit, transfer, public disclosure, recipient disclosure, audit disclosure, direct withdraw, and relayed withdraw on a local node. |

`make release-check` is intentionally heavy for every pull request. The default PR checks are `.github/workflows/test.yml` with `make ci` and `.github/workflows/security.yml` with `make vulncheck`; release-candidate validation is run manually with `make release-check`.

For prover Docker packaging, run:

```bash
make docker-proverd-build
```

This command validates compose config, Dockerfile build, and image inspect. It requires a Docker daemon, so it is not included in the default `release-check`.

`make release-pack` creates `dist/clairveil-handoff-<version>.tar.gz` and its `.sha256` file. This pack is a downstream handoff contract bundle, not a full source distribution. It includes license/notice, major handoff/security/operations docs, circuit/CLI/testing/maintainer docs, Merkle restore SOP, proto, JSON Schema, conformance fixtures, client/JS examples, prover Docker sample, release pack scripts, `RELEASE-MANIFEST.txt`, and `SHA256SUMS.txt`.

`make release-pack-verify` verifies the handoff pack's external `.sha256`, internal `SHA256SUMS.txt`, required handoff files, and that the default archive manifest commit matches current `HEAD`. When `RELEASE_PACK_ARCHIVE` is not set, it regenerates the default pack before validation so stale local archives do not mask missing files. This step checks that the tarball is not just created, but suitable to hand off as a release contract bundle.

## 3. Pre-Release Maintainer Checklist

1. Confirm `git status --short` is empty.
2. Pass `make release-check`.
3. Run `make release-pack` to create the handoff tarball and checksum.
4. Run `make release-pack-verify` to verify the handoff tarball checksum, internal checksums, required files, and manifest commit.
5. If a remote prover image will be delivered or operated, pass `make docker-proverd-build`.
6. Confirm the artifact list in `docs/clairveil-release-handoff-pack.md` matches the current repository structure.
7. Confirm `docs/schemas/clairveil-js-wallet-contract.schema.json` is validated against the latest fixtures by `make examples`.
8. Confirm `x/privacy/client/sdk/conformance/testdata` fixtures come from the same release commit delivered to the downstream JS SDK team.
9. Include ZK artifact checksums and preflight mode policy in the release note.
10. If Merkle snapshot/restore/migration behavior changed, include the sample path recomputation procedure from `docs/clairveil-merkle-restore-sop.md` in the release note.
11. Ensure accepted vulnerability policy exceptions such as `GO-2024-2584` and `GO-2026-4479` remain listed in the release note known risks.
12. State in the release note that the downstream project owns audit master private key custody, wallet storage encryption, and remote prover topology in separate operations documents.
13. Use the release note template in `docs/clairveil-release-versioning-policy.md` to document compatibility impact and downstream action.

## 4. Downstream Core Chain Team Acceptance Criteria

The core chain team confirms:

1. Pin the `github.com/DELIGHT-LABS/clairveil` module version or fork commit.
2. Wire the `x/privacy` module, keeper, store key, module account permissions, and tx/query command wiring into the downstream app.
3. Confirm `proto/clairveil/privacy/v1` service paths and generated types do not conflict with the downstream API gateway.
4. Decide the downstream denom, chain-id, and fee/gas policy, then document any values that differ from tutorials, fixtures, or e2e config.
5. Set the audit master public key in production-like genesis.
6. Run ZK artifact preflight in `strict` mode for release candidates and production-like nodes.
7. Write downstream EVM, policy module, and precompile integration tests separately from Clairveil repository smoke tests.

## 5. JS/TS SDK And Web Wallet Team Acceptance Criteria

The JS/TS SDK and web wallet teams confirm:

1. Use `docs/clairveil-js-sdk-handoff.md` as the baseline document.
2. Validate fixture shape with `docs/schemas/clairveil-js-wallet-contract.schema.json`.
3. Include `x/privacy/client/sdk/conformance/testdata` fixtures in SDK CI.
4. Port payload hash recomputation, relay withdraw handoff mapping, route/version checks, and prefix checks from `examples/js-sdk-fixture-validator` into SDK tests.
5. Reflect timeout, bearer auth, and payload hash equality checks from `examples/js-sdk-prover-http-client` in the prover adapter implementation.
6. Treat wallet note cache, root seed derived secrets, viewing keys, disclosure keys, and prepared payload/proof JSON as privacy-sensitive data; do not leave them in plaintext browser storage.
7. If using a remote prover, reflect prover-visible metadata and trust boundaries in the user privacy UX and threat model.

## 6. Prover Operations Team Acceptance Criteria

The prover operations team confirms:

1. Use `docs/clairveil-proverd-remote-production-profile.md` as the baseline document.
2. Decide whether the remote prover topology is a public service, private sidecar, local daemon, or browser/WASM prover.
3. For remote deployment, define TLS/mTLS, auth, quota, rate limit, body limit, timeout, redacted logging, and health/readiness exposure policy.
4. Run the prover artifact directory read-only and treat checksum mismatch as a release blocker.
5. Preserve `payload_hash` equality checks on both the SDK and server sides for proof requests/responses.

## 7. Known Risk And Accepted Exceptions

Release recipients must know the following risks.

| Item | Status | Recipient Action |
| --- | --- | --- |
| `GO-2024-2584` | Accepted in the repository `govulncheck` policy as a Cosmos SDK no-fixed-version actionable finding | Reassess in the downstream production risk register and realign dependencies if an upstream fixed path becomes available. |
| `GO-2026-4479` | Accepted in the repository `govulncheck` policy for the pion/dtls v2 no-fixed-version actionable finding reachable through the Cosmos SDK/CometBFT server stack | Reassess in the downstream production risk register and realign dependencies if an upstream fixed path becomes available. |
| Audit master private key custody | Clairveil provides public key config and disclosure decode flow only | Downstream owns HSM/KMS, access control, rotation, and incident response. |
| Wallet local storage | The reference CLI uses `0600` plaintext JSON | Web wallets and production wallets must implement encrypted storage and telemetry redaction. |
| Remote prover metadata exposure | A remote prover can see proof input metadata | Include the remote prover as a trusted component in user privacy UX and the deployment threat model. |
| ZK artifact provenance | The repository provides checksum/preflight tooling, but ceremony and release-signing policy are downstream responsibilities | Production releases should define artifact signing, provenance, and reproducibility policy separately. |

## 8. Handoff Completion Criteria

Release handoff is complete when:

1. The maintainer passed `make release-check`.
2. The maintainer delivered an archive/checksum that passed `make release-pack` and `make release-pack-verify`.
3. The core chain team confirmed the downstream app import/fork commit and module wiring plan.
4. The JS/TS SDK team imported fixtures and JSON Schema into its own CI.
5. The web wallet team reflected wallet storage encryption and prover topology in its design document.
6. The prover operations team selected the remote/local prover production profile.
7. The security/operations team recorded accepted vulnerabilities, audit key custody, and ZK artifact provenance in the risk register.

This document is not a replacement for a release package archive. It is a handoff index that lets teams start integration from the same release commit, fixtures, schema, and verification commands.
