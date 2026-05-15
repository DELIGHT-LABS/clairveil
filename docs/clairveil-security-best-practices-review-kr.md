# Clairveil Security Best Practices Review

이 문서는 `security-best-practices` 관점으로 현재 Clairveil repo를 검토한 결과입니다. Clairveil은 reusable privacy module과 reference host를 제공하는 repository이므로, 여기서는 core/reusable 코드가 제공해야 하는 안전한 기본값과 downstream production project가 반드시 채워야 하는 운영 보안을 분리합니다.

## 1. 잘 되어 있는 부분

| 영역                       | 상태                                                                                                                     |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| On-chain tx validation     | root, nullifier, commitment, disclosure digest 등 주요 field를 canonical field bytes로 검증합니다.                       |
| Double-spend 방어          | transfer/withdraw 모두 nullifier 재사용을 상태 변경 전에 막습니다.                                                       |
| Mandatory audit disclosure | transfer는 chain audit master pubkey와 message의 audit disclosure target pubkey가 일치해야 통과합니다.                   |
| Merkle safety              | fixed-capacity guard, rebuild bound, missing leaf/node explicit failure, query/path error propagation이 들어가 있습니다. |
| Prepared payload integrity | transfer/withdraw prover payload와 proof에 payload hash가 있고, relay/broadcast 전에 검증합니다.                         |
| File permission            | local wallet cache와 prepared/proof JSON file을 `0600`으로 씁니다.                                                       |
| Prover service basics      | request body limit, read header/read timeout, idle timeout, optional bearer auth, readiness preflight가 있습니다.        |
| ZK artifact verification   | manifest/env checksum과 preflight mode가 있어 artifact mismatch를 감지할 수 있습니다.                                    |
| Conformance fixture        | JS SDK/외부 wallet이 따라야 할 query, payload hash, prover HTTP contract fixture가 있습니다.                             |

## 2. Production 전 반드시 결정할 항목

### 2.1 Remote prover 노출 정책

`clairveil-proverd`는 local daemon과 remote sidecar 양쪽을 지원하는 reference service입니다. 기본 compose sample은 host port를 `127.0.0.1`에 묶지만, Dockerfile command는 container 내부에서 `0.0.0.0:8080`으로 listen합니다. 따라서 downstream이 image를 그대로 외부 network에 노출하면 remote proof API가 열릴 수 있습니다.

Production remote prover에는 아래가 필요합니다.

- TLS termination 또는 mTLS
- mandatory authentication
- bearer token 대신 충분히 강한 service credential 또는 session policy
- IP allowlist 또는 private network
- per-user/per-wallet quota와 rate limit
- proof latency와 error rate monitoring
- request/response body logging 금지 또는 강한 redaction
- proving artifact directory read-only mount

### 2.2 Prover timeout과 DoS boundary

현재 service는 read header/read body/idle timeout과 request body limit을 둡니다. 다만 proof 생성은 오래 걸릴 수 있어서 default `WriteTimeout`은 `0`입니다. local daemon에는 실용적이지만, public remote service에는 long-running request가 worker를 오래 점유하는 DoS 표면이 됩니다.

Remote deployment는 둘 중 하나를 선택해야 합니다.

- synchronous HTTP를 유지한다면 write timeout, concurrency limit, queue limit을 명시합니다.
- proof job을 async queue로 바꾸고 request는 job id만 반환하게 합니다.

### 2.3 Prover payload privacy

Remote prover는 private seed를 직접 받지는 않지만, prepared payload에는 note amount, randomness, merkle path, nullifier, shielded pubkey, disclosure payload 등이 들어갑니다. 따라서 remote prover는 단순 CPU service가 아니라 privacy-sensitive service입니다.

권장 기본값은 아래입니다.

- 개발/고신뢰 환경: local daemon
- 사용자 privacy 우선 wallet: local daemon 또는 browser/WASM proving
- 운영 편의 우선 wallet: remote prover 가능, 단 remote prover를 trusted component로 threat model에 포함

### 2.4 Wallet storage encryption

현재 reference CLI는 local wallet JSON을 `0600`으로 저장합니다. 이것은 개발과 sample chain에는 충분히 실용적이지만 production wallet storage 기준으로는 plaintext at rest입니다.

Web wallet/JS SDK는 아래 중 하나를 선택해야 합니다.

- browser secure storage + user password derived encryption key
- OS keychain/secure enclave 연동
- hardware wallet 또는 external signer integration
- server-side wallet이면 KMS/HSM 기반 envelope encryption

### 2.5 Master auditor key custody

Clairveil repo는 audit master public key를 genesis/config에 넣고 disclosure decode flow를 제공합니다. 그러나 audit master private key custody는 downstream production project의 책임입니다. 이 키가 유출되면 모든 mandatory audit disclosure를 읽을 수 있습니다.

Production에서는 아래가 필요합니다.

- private key 생성 ceremony
- HSM/KMS 또는 equivalent custody
- operator 접근권한 분리
- break-glass 절차
- key rotation/migration plan
- decrypt operation audit log
- incident response plan

### 2.6 ZK artifact provenance

Checksum 검증은 file corruption과 단순 tamper를 잡는 데 도움이 됩니다. 하지만 production에서는 “이 artifact가 어떤 circuit source에서 어떤 절차로 생성됐는지”도 보증해야 합니다.

필요한 작업은 아래입니다.

- artifact generation command 고정
- circuit source commit hash 기록
- manifest signing
- trusted setup/proving key provenance 문서화
- strict preflight 기본화
- release artifact checksum CI 검증

## 3. Repo 기준 권장 개선 사항

| Priority | 항목                                                                    | 이유                                                                                                                                            |
| -------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | Downstream security gate 문서를 release checklist와 연결                | 이 repo가 sample/reference라는 점을 외부 사용자가 명확히 이해해야 합니다.                                                                       |
| P1       | Remote prover production profile 유지                                   | local sample은 안전하지만 remote 운영에 필요한 auth/rate-limit/TLS/queue 정책은 downstream이 놓치기 쉽습니다.                                   |
| P2       | HTTP prover client 사용 예제에 explicit timeout 유지                    | SDK consumer가 timeout 없는 remote prover client를 쓰지 않도록 유도해야 합니다.                                                                 |
| P2       | `provertransport.HTTPHandler` 직접 노출 금지 문서화                     | body limit은 `proverservice.Handler` wrapper에서 적용됩니다. raw transport handler를 public server에 바로 붙이면 body limit을 잃을 수 있습니다. |
| P2       | Wallet storage encryption requirement를 JS SDK handoff에 더 강하게 표시 | 현재 파일 permission은 reference CLI 기준이고 web wallet 기준 보안은 별도입니다.                                                                |
| P3       | Docker image digest pinning/SBOM/vuln scan policy                       | reference image는 동작 확인용이고 production supply-chain policy는 downstream에서 확정해야 합니다.                                              |
| P3       | Health/readiness route exposure policy                                  | local sample에는 편리하지만 remote에서는 metadata exposure와 probing 표면이 됩니다.                                                             |

현재 repository는 `.github/workflows/security.yml`에서 `make vulncheck`를 실행하도록 설정합니다. 이 검사는 Go dependency와 standard library 사용 경로를 `govulncheck`로 확인하는 baseline입니다. Scanner는 patched Go toolchain baseline을 고정해서 local developer 환경의 오래된 Go standard library 때문에 결과가 흔들리지 않게 합니다. `GO-2024-2584`와 `GO-2026-4479`의 `pion/dtls` v2 경로는 현재 reference app의 upstream dependency path에 fixed version이 없어서 repo policy에서 accepted actionable vulnerability로 추적합니다. Downstream project는 이 예외들을 production risk register에서 다시 평가하고, 여기에 image scan, SBOM, secret scan, artifact signing 검사를 추가해야 합니다.

## 4. 현재 발견한 코드 레벨 주의점

현재 sampled surface에서 즉시 치명적인 P0/P1 implementation bug는 확인하지 못했습니다. 다만 아래는 downstream SDK/service 구현자가 혼동하면 문제가 될 수 있는 지점입니다.

- `x/privacy/client/sdk/proverservice/service.go`의 body limit은 proof route에만 적용됩니다. 이는 의도적으로 맞지만, downstream이 health/readiness를 외부에 노출할지 여부는 별도로 결정해야 합니다.
- `x/privacy/client/sdk/provertransport/http.go`의 raw `HTTPHandler`는 `io.ReadAll`로 body를 읽습니다. public service로 노출할 때는 반드시 `proverservice.Handler`나 별도 `MaxBytesReader` wrapper를 사용해야 합니다.
- `cmd/clairveil-proverd/main.go`는 bearer token env가 비어 있으면 `auth_enabled=false`로 실행됩니다. local daemon에는 편리하지만 remote service에서는 금지해야 합니다.
- `build/clairveil-proverd/compose.yaml`은 host bind를 `127.0.0.1`로 제한합니다. 단, Dockerfile 자체는 `0.0.0.0:8080` listen이므로 downstream compose/k8s manifest에서 network policy를 다시 확인해야 합니다.
- prepared payload JSON과 wallet JSON은 `0600`으로 저장되지만 암호화되지는 않습니다. production wallet은 별도 encryption layer가 필요합니다.

## 5. Downstream 개발자에게 전달할 최소 지침

JS/TS SDK, web wallet, downstream Cosmos SDK chain 개발자에게는 아래를 명시해서 넘기는 것이 좋습니다.

1. Clairveil repo는 production chain이 아니라 reusable privacy core와 reference host입니다.
2. `clairveild`는 sample chain이며 downstream chain은 자체 app, policy, EVM/precompile, genesis, denom, prefix에 맞게 통합해야 합니다.
3. Prover는 local/remote/browser 중 선택 가능하지만 remote prover는 privacy-sensitive trusted service입니다.
4. 모든 transfer에는 mandatory audit disclosure가 들어가야 하며, audit master pubkey는 downstream genesis/config에서 설정합니다.
5. Audit master private key custody는 downstream 책임입니다.
6. Wallet local storage는 production에서 반드시 암호화해야 합니다.
7. Disclosure plaintext는 복호화 결과만 믿으면 안 되고 digest verification을 통과해야 합니다.
8. Production artifact는 checksum뿐 아니라 provenance와 signing policy를 가져야 합니다.
9. Snapshot/restore/migration 후에는 `docs/clairveil-merkle-restore-sop-kr.md`에 따라 샘플 Merkle path를 재계산해야 합니다.
