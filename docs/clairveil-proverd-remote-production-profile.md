# clairveil-proverd Remote Production Profile

This document summarizes the production profile a downstream project must decide when operating `clairveil-proverd` as a remote sidecar. The Clairveil repo provides a reference daemon and sample configuration. The importing project must decide whether the real remote prover belongs behind a public web wallet, backend wallet, validator-adjacent service, or private infrastructure.

Korean version: [clairveil-proverd-remote-production-profile-kr.md](clairveil-proverd-remote-production-profile-kr.md)

## 1. Core Principles

- `clairveil-proverd` does not receive private seeds directly, but it does receive prepared proof payloads.
- Prepared proof payloads can include note amount, randomness, Merkle path, nullifier, shielded public keys, and disclosure payloads.
- Final outputs are owner-intent-bound, but this does not make the prepared payload non-sensitive: it still contains private note witness and is authority-equivalent privacy metadata.
- Therefore a remote prover is not merely a CPU worker. It is a privacy-sensitive trusted component.
- A local daemon is suitable for development and high-trust user environments.
- A remote daemon is good for UX and operations convenience, but auth, rate limit, logging, and data retention policy are mandatory.
- The compose sample in this repo is a local reference. Do not promote it unchanged into a production manifest.

## 2. Supported Topologies

| Topology | Recommended Situation | Main Risk | Downstream Decision |
| --- | --- | --- | --- |
| Browser/WASM prover | UX where the user does not send privacy metadata outside the browser | proving performance, browser memory, artifact delivery | JS SDK and artifact loading method |
| Local daemon | desktop wallet, development, high-trust workstation | local process lifecycle, artifact install | daemon installer and local auth |
| Private remote sidecar | company-controlled wallet backend | insider access, metadata logging | mTLS, private network, retention |
| Public remote prover | general web wallet UX | DoS, abuse, metadata exposure | strong auth, quota, monitoring |

The Clairveil repo does not force one of these choices. `clairveil-proverd` and the prover HTTP contract are references that let downstream projects swap topology behind the same wallet adapter.

The client safe default is one configured prover endpoint with automatic failover disabled. Same-endpoint retry is allowed after timeout/response checks. Sending a witness-bearing request to a second endpoint requires explicit user/product-policy opt-in that names the added operator/privacy boundary; availability alone is not permission to widen disclosure.

## 3. Safe Baseline

Remote production must satisfy at least the baseline below.

| Control | Required Profile |
| --- | --- |
| Network | Put it behind a private network or edge proxy. Avoid direct public internet exposure. |
| TLS | TLS can be handled by an edge proxy, load balancer, service mesh, or mTLS instead of built into the app. Plain HTTP should be limited to private/local networks. |
| Auth | Proof routes must be protected by bearer token, mTLS identity, session-bound API token, or equivalent. |
| Rate limit | Apply quota per user, wallet, IP, and API token. |
| Body limit | Set `-max-request-bytes` explicitly and align the edge proxy body limit. |
| Timeout | Set read-header/read/idle timeouts. For long proof responses, choose either synchronous timeout policy or async queue. |
| Concurrency | Limit worker count and queue depth. Do not allow unlimited goroutine proof generation. |
| Logging | Do not log request/response body or bearer tokens. |
| Artifact | Provide the artifact directory as a read-only mount. Use manifest/checksum and strict preflight. |
| Health/metrics | Do not expose `/healthz`, `/readyz`, or `/debug/vars` to the public internet, or restrict them to internal auth/network. |

## 4. Docker Packaging Validation

The repository provides a target for validating the reference `clairveil-proverd` image build.

```bash
make docker-proverd-build
```

This command:

1. Validates `build/clairveil-proverd/compose.yaml` with `docker compose config`.
2. Builds the prover image with `build/clairveil-proverd/Dockerfile`.
3. Inspects the resulting image with `docker image inspect`.

The default image tag is `delightlabs/clairveil-proverd:local`. Override it if needed.

```bash
CLAIRVEIL_PROVERD_IMAGE=example.com/clairveil-proverd:rc1 make docker-proverd-build
```

To build a prover binary matching the local machine architecture, the script passes `go env GOARCH` as the `TARGETARCH` build arg by default. To validate another architecture, set `TARGETARCH=amd64` or `TARGETARCH=arm64` explicitly.

## 5. Reference Command

Example command for a local or private network profile:

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/srv/clairveil/artifacts
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd   -listen 127.0.0.1:8080   -read-header-timeout 5s   -read-timeout 30s   -write-timeout 0s   -idle-timeout 2m   -max-request-bytes 8388608
```

`-write-timeout 0s` is the reference default because synchronous proof generation can take a long time. A remote public service must choose one of these options.

- Set a sufficiently long finite write timeout after benchmarking.
- Convert proof requests into async jobs and make the HTTP request return only a job id.

## 6. Auth Policy

If `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN` is empty, `clairveil-proverd` can accept proof routes without auth. This is convenient for local daemons and smoke tests, but not suitable for remote deployment.

Remote profiles should follow these rules.

- Generate bearer tokens as random secrets with at least 128 bits of entropy.
- Do not put tokens in images, git, or static config.
- Inject tokens through environment secrets, secret managers, Kubernetes Secrets, Vault, KMS-backed config, or equivalent.
- Document token rotation.
- Do not leave the `Authorization` header in access logs.

If bearer token alone is not enough for a public service, add mTLS, OAuth/OIDC gateway, wallet session binding, and per-account quota.

## 7. Prover Payload Privacy

Assume a remote prover operator can see:

- transfer/withdraw amount and asset metadata;
- input/output shielded public keys;
- Merkle path and root;
- nullifier;
- disclosure payload/ciphertext;
- request timing and client identity metadata.

Therefore the remote prover must be a component trusted by the user. If the wallet UX requires the user not to trust a remote prover, provide local daemon or browser/WASM proving.

Current request/response/proof contracts are `v2` for transfer and withdraw. Transfer prepared payload is `v5`; withdraw prover/final payloads are `v2`; disclosure plaintext/query is `privacy-fixed-v1`. Reject legacy inputs. Request bodies, bearer credentials, signatures, disclosure plaintext/blindings, and proofs must be excluded from logs, traces, crash dumps, and analytics.

## 8. Do Not Expose The Raw Handler

`x/privacy/client/sdk/provertransport.HTTPHandler` in the Go SDK is a low-level handler for testing and reusing the transport contract. The handler itself reads the request body with `io.ReadAll`.

When exposing a production HTTP server, use one of these options.

- `x/privacy/client/sdk/proverservice.Handler`
- a custom wrapper that applies `http.MaxBytesReader`
- edge proxy body limit plus app-level body limit

If the raw `provertransport.HTTPHandler` is attached directly to a public server, the request body limit can be missed.

## 9. Artifact Profile

A remote prover lazily reads proving keys and R1CS artifacts; validators need only VK files. The active set is `privacy-note-v1`. `privacy_zk_manifest.json` schema `v2` must exactly match consensus `CircuitSetIdentity` schema `v1`, including ordered circuit descriptors, VK SHA-256, and public-input schema SHA-256. Environment checksum values cannot override consensus identity, and mismatch must fail startup/readiness. In production:

- use `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict`;
- deploy the required `privacy_zk_manifest.json` with release artifacts; checksum env values may add consistency checks but cannot replace the structured manifest;
- mount the artifact directory as read-only;
- record circuit source commit, generation command, checksum, and signer for artifact releases;
- block stale artifact and chain verifier artifact mismatch at the release gate.

Artifacts produced by the repository setup flow are development artifacts. No formal trusted setup or external audit is implied; production must supply its own ceremony/provenance and signing policy.

## 10. Observability

Recommended metrics:

- request count by route/status
- proof latency histogram by route
- proof failure count by error code
- auth failure count
- body limit rejection count
- readiness/preflight failure count
- worker queue depth
- CPU, memory, and open file descriptor usage

The reference `clairveil-proverd` exposes process telemetry at `/debug/vars` for benchmark/load-test collection. Treat it as an internal operations endpoint. It can reveal process sizing and traffic timing information, so expose it only on loopback, a private network, or an authenticated operations plane.

Do not log:

- bearer token
- full prepared payload JSON
- proof bytes
- disclosure payload body
- user seed, private key, mnemonic

## 11. Downstream Acceptance Checklist

Before operating a remote prover in a production-like environment, confirm:

1. Proof routes are not reachable without auth.
2. Public ingress has TLS or private service mesh transport.
3. Edge and app body limits are both configured.
4. Read/header/idle/write timeout policy is documented.
5. Concurrent proof limit or async queue exists.
6. Request/response body logging is disabled or redacted.
7. Artifact directory is read-only and preflight is strict.
8. Health/readiness/metrics routes are internal-only.
9. JS SDK uses request timeout and validates response version plus payload hash.
10. Remote prover is included in the downstream threat model as a trusted privacy-sensitive component.
11. Automatic multi-prover failover is disabled, or every additional endpoint has explicit informed opt-in.
12. Manifest/VK/public-input schema identity exactly matches the chain before readiness.

## 12. Related Files

- `cmd/clairveil-proverd/main.go`
- `build/clairveil-proverd/Dockerfile`
- `build/clairveil-proverd/compose.yaml`
- `build/clairveil-proverd/clairveil-proverd.env.example`
- `x/privacy/client/sdk/proverservice/service.go`
- `x/privacy/client/sdk/provertransport/http.go`
- `examples/js-sdk-prover-http-client`

## 13. Session 2 Admission And Artifact Addendum

The active consensus circuit set is `privacy-note-v1`; the fixed note/disclosure/envelope contract is `privacy-fixed-v1`. This is incompatible with earlier cached artifacts and proof jobs. Deploy from fresh genesis, remove old R1CS/PK/VK sets and queued/cached requests, regenerate the exact active set, and require clients to clear note/scan caches and rescan. Session 2's `BatchJoinSplit16x32` circuit is a feasibility benchmark only and must not be installed, advertised, or routed as a production proving circuit.

The artifact registry is role-aware. Validator readiness requires consensus identity and reads only the requested VKs. Prover readiness reads only selected R1CS/PK pairs and loads them lazily; it must still fail closed when supplied consensus metadata does not match. Do not preload unrelated proving keys merely because they exist in the manifest.

Reference admission defaults are per circuit: `max_in_flight=1` and `max_queued=4`. The request-body default is `max_request_bytes=8388608` (8 MiB) and must be positive; `0` is invalid and never means unlimited. Queue saturation returns a retryable busy response. Operators should export in-flight, queued, rejected, canceled, queue-wait, prove-time, CPU, and RSS metrics without request or witness content.

Only expose the bounded `proverservice.Handler`; never mount `provertransport.HTTPHandler` directly on a public listener or behind a proxy and assume the proxy is the only body bound. Keep automatic prover failover disabled, because another endpoint expands the private-witness trust boundary. Context cancellation stops the caller's wait, but an already running in-process gnark proof can continue until return and retains its permit; the reference service cannot preempt the solver. Use isolated, memory-limited worker processes and terminate them for hard cancellation or OOM containment.

The future 16x32 public schema is reserved in this exact order: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. Reserving a schema is not authorization to create a production endpoint or artifact.
