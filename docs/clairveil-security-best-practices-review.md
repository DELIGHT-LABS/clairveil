# Clairveil Security Best Practices Review

This document is the current review of the Clairveil repo from a security-best-practices perspective. Clairveil provides a reusable privacy module and reference host, so this document separates the safe defaults that core/reusable code should provide from the operational security that downstream production projects must supply.

Korean version: [clairveil-security-best-practices-review-kr.md](clairveil-security-best-practices-review-kr.md)

## 1. Areas In Good Shape

| Area | Status |
| --- | --- |
| On-chain tx validation | Validates major fields such as roots, nullifiers, commitments, and disclosure digests as canonical field bytes. |
| Double-spend protection | Both transfer and withdraw reject nullifier reuse before state mutation. |
| Mandatory audit disclosure | Transfer passes only when the chain audit master pubkey matches the message audit disclosure target pubkey. |
| Merkle safety | Fixed-capacity guard, rebuild bound, missing leaf/node explicit failure, and query/path error propagation are implemented. |
| Prepared payload integrity | Transfer/withdraw prover payloads and proofs carry payload hashes, and they are verified before relay/broadcast. |
| File permission | Local wallet cache and prepared/proof JSON files are written with `0600`. |
| Prover service basics | Request body limit, read header/read timeout, idle timeout, optional bearer auth, and readiness preflight exist. |
| ZK artifact verification | Manifest/env checksum and preflight modes can detect artifact mismatch. |
| Conformance fixture | Query, payload hash, and prover HTTP contract fixtures exist for JS SDK/external wallets. |

## 2. Production Decisions Required Before Launch

### 2.1 Remote Prover Exposure Policy

`clairveil-proverd` is a reference service that supports both local daemon and remote sidecar modes. The default compose sample binds the host port to `127.0.0.1`, but the Dockerfile command listens on `0.0.0.0:8080` inside the container. If downstream exposes the image unchanged to an external network, the remote proof API can become reachable.

A production remote prover needs:

- TLS termination or mTLS
- mandatory authentication
- sufficiently strong service credential or session policy instead of a weak bearer token
- IP allowlist or private network
- per-user/per-wallet quota and rate limit
- proof latency and error rate monitoring
- request/response body logging disabled or strongly redacted
- read-only mount for proving artifact directory

### 2.2 Prover Timeout And DoS Boundary

The current service sets read-header/read-body/idle timeouts and request body limits. However, proof generation can take a long time, so the default `WriteTimeout` is `0`. This is practical for local daemons, but for public remote services, long-running requests become a DoS surface by occupying workers for a long time.

Remote deployment must choose one of these paths.

- If keeping synchronous HTTP, define write timeout, concurrency limit, and queue limit.
- If using async proof jobs, return only a job id from the request.

### 2.3 Prover Payload Privacy

A remote prover does not directly receive private seeds, but prepared payloads include note amount, randomness, Merkle path, nullifier, shielded pubkeys, disclosure payloads, and related metadata. Therefore a remote prover is a privacy-sensitive service, not just a CPU service.

Recommended defaults:

- development/high-trust environment: local daemon
- user-privacy-first wallet: local daemon or browser/WASM proving
- operations-convenience-first wallet: remote prover is possible, but include it as a trusted component in the threat model

### 2.4 Wallet Storage Encryption

The current reference CLI stores local wallet JSON with `0600`. This is practical enough for development and the sample chain, but it is plaintext at rest by production wallet standards.

A web wallet/JS SDK must choose one of:

- browser secure storage plus user password-derived encryption key
- OS keychain/secure enclave integration
- hardware wallet or external signer integration
- KMS/HSM-based envelope encryption for server-side wallets

### 2.5 Master Auditor Key Custody

The Clairveil repo provides the flow for putting the audit master public key in genesis/config and decoding disclosure. Audit master private key custody is the responsibility of the downstream production project. If this key leaks, all mandatory audit disclosures become readable.

Production needs:

- private key generation ceremony
- HSM/KMS or equivalent custody
- separated operator permissions
- break-glass procedure
- key rotation/migration plan
- decrypt-operation audit log
- incident response plan

### 2.6 ZK Artifact Provenance

Checksum verification helps catch file corruption and simple tampering. In production, the project must also prove which circuit source and process produced the artifacts.

Required work:

- pin artifact generation command
- record circuit source commit hash
- sign manifest
- document trusted setup/proving key provenance
- make strict preflight the default
- verify release artifact checksums in CI

## 3. Recommended Repo-Level Improvements

| Priority | Item | Reason |
| --- | --- | --- |
| P1 | Link downstream security gates with the release checklist | External users must clearly understand that this repo is sample/reference. |
| P1 | Maintain the remote prover production profile | The local sample is safe, but downstream teams can easily miss auth/rate-limit/TLS/queue policy for remote operations. |
| P2 | Keep explicit timeout in the HTTP prover client example | Prevent SDK consumers from writing remote prover clients with no timeout. |
| P2 | Document that `provertransport.HTTPHandler` must not be directly exposed | Body limits are applied in the `proverservice.Handler` wrapper. Directly attaching the raw transport handler to a public server can lose the body limit. |
| P2 | Emphasize wallet storage encryption requirements in JS SDK handoff | Current file permission is only for the reference CLI; web wallet security is separate. |
| P3 | Docker image digest pinning/SBOM/vuln scan policy | The reference image is for behavior validation, and downstream must define production supply-chain policy. |
| P3 | Health/readiness route exposure policy | Convenient for local samples, but a metadata/probing surface remotely. |

The repository currently configures `.github/workflows/security.yml` to run `make vulncheck`. This baseline checks Go dependency and standard-library reachable paths with `govulncheck`. The scanner pins a patched Go toolchain baseline so local developer machines with older Go standard libraries do not make results unstable. `GO-2024-2584` and the `pion/dtls` v2 path of `GO-2026-4479` are tracked as accepted actionable vulnerabilities in repo policy because the current upstream dependency paths have no fixed versions for this reference app. Downstream projects must re-evaluate these exceptions in their production risk register and add image scan, SBOM, secret scan, and artifact-signing checks.

## 4. Current Code-Level Notes

No immediately critical P0/P1 implementation bug was found in the sampled surface. These points can still become issues if downstream SDK/service implementers misunderstand them.

- The body limit in `x/privacy/client/sdk/proverservice/service.go` applies only to proof routes. This is intentional, but downstream must separately decide whether health/readiness should be externally exposed.
- The raw `HTTPHandler` in `x/privacy/client/sdk/provertransport/http.go` reads body with `io.ReadAll`. Public services must use `proverservice.Handler` or a separate `MaxBytesReader` wrapper.
- `cmd/clairveil-proverd/main.go` runs with `auth_enabled=false` when the bearer token env is empty. This is convenient locally, but must be forbidden for remote services.
- `build/clairveil-proverd/compose.yaml` limits host bind to `127.0.0.1`. However, the Dockerfile itself listens on `0.0.0.0:8080`, so downstream compose/k8s manifests must re-check network policy.
- Prepared payload JSON and wallet JSON are stored with `0600`, but they are not encrypted. Production wallets need an encryption layer.

## 5. Minimum Guidance To Downstream Developers

JS/TS SDK, web wallet, and downstream Cosmos SDK chain developers should receive at least this guidance.

1. The Clairveil repo is not a production chain. It is a reusable privacy core and reference host.
2. `clairveild` is a sample chain, and downstream chains must integrate it with their own app, policy, EVM/precompile, genesis, denom, and prefix.
3. Proving can be local, remote, or browser-based, but a remote prover is a privacy-sensitive trusted service.
4. Every transfer must include mandatory audit disclosure, and the audit master pubkey is configured by downstream genesis/config.
5. Audit master private key custody is downstream responsibility.
6. Wallet local storage must be encrypted in production.
7. Disclosure plaintext must not be trusted just because it decrypted; digest verification must pass.
8. Production artifacts need provenance and signing policy in addition to checksum.
9. After snapshot/restore/migration, recompute sample Merkle paths according to `docs/clairveil-merkle-restore-sop.md`.
