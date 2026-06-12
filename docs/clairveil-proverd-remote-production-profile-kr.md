# clairveil-proverd Remote Production Profile

이 문서는 `clairveil-proverd`를 remote sidecar로 운영하려는 downstream project가 결정해야 할 production profile을 정리합니다. Clairveil repo는 reference daemon과 sample configuration을 제공합니다. 실제 remote prover를 public web wallet, backend wallet, validator-adjacent service, private infra 중 어디에 둘지는 가져다 쓰는 프로젝트가 결정해야 합니다.

## 1. 핵심 원칙

- `clairveil-proverd`는 private seed를 직접 받지 않지만 prepared proof payload를 받습니다.
- prepared proof payload에는 note amount, randomness, merkle path, nullifier, shielded public keys, disclosure payload가 포함될 수 있습니다.
- 따라서 remote prover는 단순 CPU worker가 아니라 privacy-sensitive trusted component입니다.
- local daemon은 개발과 고신뢰 사용자 환경에 적합합니다.
- remote daemon은 UX와 운영 편의에는 좋지만 auth, rate limit, logging, data retention 정책이 반드시 필요합니다.
- 이 repo의 compose sample은 local reference입니다. Production manifest로 그대로 승격하면 안 됩니다.

## 2. 지원 topology

| Topology               | 권장 상황                                         | 주요 위험                                       | Downstream 결정                  |
| ---------------------- | ------------------------------------------------- | ----------------------------------------------- | -------------------------------- |
| Browser/WASM prover    | 사용자가 privacy metadata를 외부로 보내지 않는 UX | proving 성능, browser memory, artifact delivery | JS SDK와 artifact loading 방식   |
| Local daemon           | desktop wallet, dev, 고신뢰 workstation           | local process lifecycle, artifact install       | daemon installer와 local auth    |
| Private remote sidecar | company-controlled wallet backend                 | 내부자 접근, metadata logging                   | mTLS, private network, retention |
| Public remote prover   | 일반 web wallet UX                                | DoS, abuse, metadata exposure                   | strong auth, quota, monitoring   |

Clairveil repo는 이 중 하나를 강제하지 않습니다. `clairveil-proverd`와 prover HTTP contract는 downstream이 같은 wallet adapter 뒤에서 topology를 바꿀 수 있게 하기 위한 reference입니다.

## 3. Safe baseline

Remote production에서 최소한 아래 baseline을 만족해야 합니다.

| Control     | Required profile                                                                                                                             |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| Network     | private network 또는 edge proxy 뒤에 둡니다. Public internet direct exposure는 피합니다.                                                     |
| TLS         | app 내장 TLS가 아니라 edge proxy, load balancer, service mesh, 또는 mTLS로 처리해도 됩니다. Plain HTTP는 private/local network로 제한합니다. |
| Auth        | proof route는 bearer token, mTLS identity, session-bound API token 등으로 반드시 보호합니다.                                                 |
| Rate limit  | per user, per wallet, per IP, per API token quota를 둡니다.                                                                                  |
| Body limit  | `-max-request-bytes`를 명시하고 edge proxy body limit도 맞춥니다.                                                                            |
| Timeout     | read-header/read/idle timeout을 명시합니다. Long proof response는 sync timeout 또는 async queue 중 하나를 선택합니다.                        |
| Concurrency | worker 수와 queue depth를 제한합니다. Unlimited goroutine proof generation을 허용하지 않습니다.                                              |
| Logging     | request/response body와 bearer token을 log에 남기지 않습니다.                                                                                |
| Artifact    | artifact directory는 read-only mount로 제공합니다. Manifest/checksum과 strict preflight를 사용합니다.                                        |
| Health/metrics | `/healthz`, `/readyz`, `/debug/vars`는 public internet에 노출하지 않거나 internal auth/network로 제한합니다.                             |

## 4. Docker packaging 검증

Repository는 reference `clairveil-proverd` image build 검증 타깃을 제공합니다.

```bash
make docker-proverd-build
```

이 명령은 아래를 수행합니다.

1. `build/clairveil-proverd/compose.yaml`을 `docker compose config`로 검증합니다.
2. `build/clairveil-proverd/Dockerfile`로 prover image를 build합니다.
3. 생성된 image를 `docker image inspect`로 확인합니다.

기본 image tag는 `delightlabs/clairveil-proverd:local`입니다. 필요하면 아래처럼 바꿀 수 있습니다.

```bash
CLAIRVEIL_PROVERD_IMAGE=example.com/clairveil-proverd:rc1 make docker-proverd-build
```

local machine architecture와 맞는 prover binary를 만들기 위해 script는 기본적으로 `go env GOARCH`를 `TARGETARCH` build arg로 전달합니다. 다른 architecture를 검증하려면 `TARGETARCH=amd64` 또는 `TARGETARCH=arm64`를 명시합니다.

## 5. Reference command

Local 또는 private network profile에서 시작하는 command 예시는 아래입니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/srv/clairveil/artifacts
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd \
  -listen 127.0.0.1:8080 \
  -read-header-timeout 5s \
  -read-timeout 30s \
  -write-timeout 0s \
  -idle-timeout 2m \
  -max-request-bytes 8388608
```

`-write-timeout 0s`는 synchronous proof generation이 오래 걸릴 수 있는 reference default입니다. Remote public service에서는 아래 중 하나를 선택해야 합니다.

- benchmark 후 충분히 긴 finite write timeout을 둡니다.
- proof request를 async job queue로 바꾸고 HTTP request는 job id만 반환하게 합니다.

## 6. Auth policy

`CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`이 비어 있으면 `clairveil-proverd`는 proof route를 auth 없이 받을 수 있습니다. 이것은 local daemon과 smoke test에는 편리하지만 remote deployment에는 적합하지 않습니다.

Remote profile에서는 아래를 지킵니다.

- bearer token은 최소 128-bit 이상의 random secret으로 생성합니다.
- token을 image, git, static config에 넣지 않습니다.
- token은 environment secret, secret manager, Kubernetes Secret, Vault, KMS-backed config 등으로 주입합니다.
- token rotation 절차를 문서화합니다.
- access log에는 `Authorization` header를 남기지 않습니다.

Bearer token만으로 충분하지 않은 public service라면 mTLS, OAuth/OIDC gateway, wallet session binding, per-account quota를 별도로 둡니다.

## 7. Prover payload privacy

Remote prover operator는 아래 정보를 볼 수 있다고 가정해야 합니다.

- transfer/withdraw amount와 asset metadata
- input/output shielded public key
- merkle path와 root
- nullifier
- disclosure payload/ciphertext
- request timing과 client identity metadata

따라서 remote prover는 사용자가 trust하는 component여야 합니다. 사용자가 remote prover를 신뢰하지 않아야 하는 wallet UX라면 local daemon 또는 browser/WASM proving을 제공해야 합니다.

## 8. Raw handler 사용 금지

Go SDK의 `x/privacy/client/sdk/provertransport.HTTPHandler`는 transport contract를 테스트하고 재사용하기 위한 낮은 수준의 handler입니다. 이 handler 자체는 `io.ReadAll`로 request body를 읽습니다.

Production HTTP server로 노출할 때는 아래 중 하나를 사용해야 합니다.

- `x/privacy/client/sdk/proverservice.Handler`
- 자체 wrapper에서 `http.MaxBytesReader`를 적용한 handler
- edge proxy body limit + app-level body limit 조합

Raw `provertransport.HTTPHandler`를 public server에 직접 붙이면 request body limit을 놓칠 수 있습니다.

## 9. Artifact profile

Remote prover는 proving key와 R1CS artifact를 읽습니다. Production에서는 아래를 지킵니다.

- `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict`를 사용합니다.
- `privacy_zk_manifest.json` 또는 checksum env를 release artifact와 함께 배포합니다.
- artifact directory는 read-only volume으로 mount합니다.
- artifact release는 circuit source commit, generation command, checksum, signer를 함께 기록합니다.
- stale artifact와 chain verifier artifact mismatch를 release gate에서 막습니다.

## 10. Observability

권장 metric은 아래입니다.

- request count by route/status
- proof latency histogram by route
- proof failure count by error code
- auth failure count
- body limit rejection count
- readiness/preflight failure count
- worker queue depth
- CPU, memory, open file descriptor usage

Reference `clairveil-proverd`는 benchmark/load-test 수집을 위해 `/debug/vars`에서 process telemetry를 노출합니다. 이 endpoint는 internal operations endpoint로 취급합니다. Process sizing과 traffic timing 정보를 드러낼 수 있으므로 loopback, private network, 또는 인증된 operations plane에서만 노출합니다.

Log에는 아래를 남기지 않습니다.

- bearer token
- full prepared payload JSON
- proof bytes
- disclosure payload body
- user seed, private key, mnemonic

## 11. Downstream acceptance checklist

Remote prover를 production-like 환경에 올리기 전 아래를 확인합니다.

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

## 12. 관련 파일

- `cmd/clairveil-proverd/main.go`
- `build/clairveil-proverd/Dockerfile`
- `build/clairveil-proverd/compose.yaml`
- `build/clairveil-proverd/clairveil-proverd.env.example`
- `x/privacy/client/sdk/proverservice/service.go`
- `x/privacy/client/sdk/provertransport/http.go`
- `examples/js-sdk-prover-http-client`
