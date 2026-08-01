# Clairveil Security Best Practices Review

이 문서는 `security-best-practices` 관점으로 현재 Clairveil repo를 검토한 결과입니다. Clairveil은 reusable privacy module과 reference host를 제공하는 repository이므로, 여기서는 core/reusable 코드가 제공해야 하는 안전한 기본값과 downstream production project가 반드시 채워야 하는 운영 보안을 분리합니다.

## 1. 잘 되어 있는 부분

| 영역                       | 상태                                                                                                                     |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| On-chain tx validation     | root, nullifier, commitment, disclosure digest 등 주요 field를 canonical field bytes로 검증합니다.                       |
| Double-spend 방어          | transfer/withdraw 모두 nullifier 재사용을 상태 변경 전에 막습니다.                                                       |
| Mandatory audit disclosure | transfer는 chain audit master pubkey와 message의 audit disclosure target pubkey가 일치해야 통과합니다.                   |
| Merkle safety              | fixed-capacity guard, rebuild bound, missing leaf/node explicit failure, query/path error propagation이 들어가 있습니다. |
| Prepared payload integrity | `TransferIntentV2`/`SpendIntentV2`가 chain, expiry, recipient/output/disclosure effect를 묶고 transfer는 final owner signature 하나를 사용하며 creator replacement는 relayer-safe합니다. |
| Scan hint safety          | Transfer view tag는 shape를 검증하고 untrusted hint로 취급합니다. 안전한 기본 scan은 mismatch에서도 full decrypt할 수 있습니다. |
| File permission            | local wallet cache와 prepared/proof JSON file을 `0600`으로 씁니다.                                                       |
| Prover service basics      | request body limit, read header/read timeout, idle timeout, optional bearer auth, readiness preflight가 있습니다.        |
| ZK artifact verification   | consensus가 exact ordered VK/public-input schema hash를 고정하고 local verifier mismatch는 startup/readiness를 막으며 env checksum은 override할 수 없습니다. |
| Proof verification cost    | cheap canonical proof framing 뒤 decode/VK load/cryptographic verification 전에 fixed gas를 precharge합니다. |
| Batch chain core           | `MsgBatchTransfer`가 frozen 12-field witness를 다시 derive하고 bounded resource category 전체를 precharge하며 proof 검증 뒤 globally unique nullifier/commitment와 typed scan state를 atomic하게 commit합니다. |
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

`ProverPool`은 witness-bearing request를 기본적으로 endpoint 하나에만 보냅니다. 같은 payload를 다른 operator에게 보내면 privacy boundary가 넓어지므로 automatic failover는 비활성화되어 있습니다. Multi-endpoint failover는 endpoint set과 경고를 보여 준 뒤 사용자 또는 product policy가 명시적으로 opt-in한 경우에만 허용합니다.

Owner intent가 output, disclosure envelope, ciphertext, chain, expiry를 불변으로 만들어도 prepared prover payload는 여전히 private note witness를 포함합니다. Authority-equivalent privacy-sensitive material로 취급하고 log, crash report, telemetry에 남기거나 proof workflow 이후 보관하지 않습니다.

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

Active identity는 `privacy-note-v1`입니다. `privacy_zk_manifest.json` schema `v2`는 required order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`, exact VK SHA-256, public-input schema SHA-256까지 genesis/state `CircuitSetIdentity` schema `v1`과 일치해야 합니다. Validator는 VK만 필요하고 prover는 proving 시 R1CS/PK를 lazy load합니다. Repo가 생성한 artifact는 development 전용이며 formal trusted setup, signed production release, external audit가 아닙니다.

## 3. Repo 기준 권장 개선 사항

| Priority | 항목                                                                    | 이유                                                                                                                                            |
| -------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | Downstream security gate 문서를 release checklist와 연결                | 이 repo가 sample/reference라는 점을 외부 사용자가 명확히 이해해야 합니다.                                                                       |
| P1       | Remote prover production profile 유지                                   | local sample은 안전하지만 remote 운영에 필요한 auth/rate-limit/TLS/queue 정책은 downstream이 놓치기 쉽습니다.                                   |
| P2       | HTTP prover client 사용 예제에 explicit timeout 유지                    | SDK consumer가 timeout 없는 remote prover client를 쓰지 않도록 유도해야 합니다.                                                                 |
| P2       | `provertransport.HTTPHandler` 직접 노출 금지 지침 유지                 | Raw handler도 admission 전 hard body cap을 적용하지만 bearer auth, gzip wire/decompressed limit, health/readiness policy, server timeout은 service wrapper가 담당합니다. |
| P2       | Wallet storage encryption requirement를 JS SDK handoff에 더 강하게 표시 | 현재 파일 permission은 reference CLI 기준이고 web wallet 기준 보안은 별도입니다.                                                                |
| P3       | Docker image digest pinning/SBOM/vuln scan policy                       | reference image는 동작 확인용이고 production supply-chain policy는 downstream에서 확정해야 합니다.                                              |
| P3       | Health/readiness route exposure policy                                  | local sample에는 편리하지만 remote에서는 metadata exposure와 probing 표면이 됩니다.                                                             |

현재 repository는 `.github/workflows/security.yml`에서 `make vulncheck`를 실행합니다. 이 baseline은 Go dependency/standard library reachable path를 검사하고 patched Go `1.25.12` toolchain을 고정합니다. `GO-2024-2584`, `GO-2026-4479`의 `pion/dtls` v2, `GO-2026-5932`만 no-fixed-version 예외로 좁게 추적합니다. 마지막 항목은 Cosmos SDK가 local ASCII key armor에 `x/crypto/openpgp/armor`를 사용해서 reachable하며 Clairveil은 OpenPGP signing/encryption을 사용하지 않습니다. Fixed version이 생기면 각 예외는 즉시 무효가 됩니다. Downstream project는 이를 production risk register에서 다시 평가하고 image scan, SBOM, secret scan, artifact signing을 추가해야 합니다.

## 4. 현재 발견한 코드 레벨 주의점

보안 강화 기준선 remediation은 known current duplicate-input/output, intent substitution, replay, disclosure oracle, decoder, failover default, genesis/artifact identity, proof gas issue를 닫았습니다. 해당 범위에 미해결 Critical/High finding은 없습니다. 다만 아래는 downstream SDK/service 구현자가 혼동하면 문제가 될 수 있는 지점입니다.

2026-07-13 독립 공개 검증 record는 `PUBLICATION_READY_EXPERIMENTAL`입니다. batch chain core는 batch protocol contract가 동결한 `DISCLOSURE-BLINDING-SEPARATION` V1을 production `99,775` constraints(`+10`)에 구현하고 native/prepared와 structured 2x2 pre-sign validation을 정렬했으며 JoinSplit development identity만 회전했습니다. security, protocol, chain-core, and client-integration gates와 독립 공개 검증은 `PROVER-FAILOVER-LIVE-EVIDENCE` live no-failover evidence와 structured batch-signing boundary의 non-canonical BN254 alias 거부를 포함해 closure됐습니다. Stable validation error는 secret-free이며 proving/signature release 전에 반환합니다. 이 처분은 production release 승인이 아닙니다.

- `x/privacy/client/sdk/proverservice/service.go`의 body limit은 proof route에만 적용됩니다. 이는 의도적으로 맞지만, downstream이 health/readiness를 외부에 노출할지 여부는 별도로 결정해야 합니다.
- `x/privacy/client/sdk/provertransport/http.go`의 raw `HTTPHandler`는 transfer, withdraw, batch 모두 admission 전 shared bounded reader를 사용합니다. Public service는 bearer auth, gzip wire/decompressed limit, health/readiness policy, server timeout을 위해 계속 `proverservice.Handler` 또는 동등한 wrapper를 사용해야 합니다.
- `cmd/clairveil-proverd/main.go`는 bearer token env가 비어 있으면 `auth_enabled=false`로 실행됩니다. local daemon에는 편리하지만 remote service에서는 금지해야 합니다.
- `build/clairveil-proverd/compose.yaml`은 host bind를 `127.0.0.1`로 제한합니다. 단, Dockerfile 자체는 `0.0.0.0:8080` listen이므로 downstream compose/k8s manifest에서 network policy를 다시 확인해야 합니다.
- prepared payload JSON과 wallet JSON은 `0600`으로 저장되지만 암호화되지는 않습니다. production wallet은 별도 encryption layer가 필요합니다.
- Transfer/prover contract version은 의도적인 breaking change입니다. Transfer payload `v5`, transfer proof/request/response `v2`, withdraw prover/final payload와 proof/request/response `v2`, disclosure plaintext/query `privacy-fixed-v1`이며 legacy payload는 compatibility decode하지 말고 다시 생성해야 합니다.

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
10. Legacy prepared payload를 거부하고 `SpendIntentV2`/`TransferIntentV2` public-input 순서를 정확히 보존하며 `privacy-note-v1` 적용 시 cached proof job/artifact를 reset해야 합니다.

## 6. NoteV1과 batch chain-core security addendum

### 6.1 Deposit prover 경계

`POST /v1/prover/deposit`을 특별 client shortcut이 아닌 bounded raw-handler/auth/redaction surface로 취급합니다. Versioned circuit witness만 받고 bearer auth, gzip/body limit, admission, readiness, timeout, secret-free error, `no-store`를 위해 service wrapper를 사용합니다. Request/response body와 witness value는 log하지 않습니다. `Content-Type: application/json`, `405`의 `Allow: POST`, media failure `415`, proving 전 `400`, validated prover invocation 뒤 `500`을 강제합니다. [general HTTP API](clairveil-proverd-http-api-kr.md)와 [deposit API](clairveil-proverd-deposit-api-kr.md)를 참조합니다.

현재 production circuit과 state는 `privacy-note-v1` NoteV1 commitment/nullifier/tree contract와 canonical key validation을 공유합니다. Canonical note, disclosure, encrypted-envelope byte는 versioned `privacy-fixed-v1`입니다. Raw ciphertext, JSON plaintext, 잘못된 envelope kind, non-canonical field/key data, non-zero reserved byte, trailing byte는 fail closed해야 합니다. `AssetRegistryV1`이 consensus-authoritative one-to-one denom/32-byte asset-ID mapping입니다. Global commitment uniqueness는 SDK-only precheck가 아니라 consensus state입니다.

이 계약은 이전 state와 artifact와 의도적으로 호환되지 않습니다. Fresh genesis를 사용하고 wallet note/scan cache와 prepared/proof job을 제거하며 exact `privacy-note-v1` artifact set을 다시 생성한 뒤 rescan합니다. Permissive compatibility decoder나 in-place migration을 추가하지 않습니다. Unified scan order는 `(height, global_sequence, output_index)`이고 spend witness는 exact public root의 path snapshot을 사용해야 합니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical path/root query는 wallet interest를 노출하므로 privacy warning을 유지하고 필요하면 privacy-preserving infrastructure를 사용합니다.

`BatchJoinSplit16x32`는 네 번째 required production circuit이고 `MsgBatchTransfer`/`BatchTransferOutput`과 keeper handler가 구현되었습니다. Circuit은 capacity 16/32, active-prefix/zero-disabled rule, independent membership, owner/key constraint, active-only distinctness, value conservation, vector formula, output별 independent user/full disclosure blinding, single owner signature, public-input 순서 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`를 유지합니다. Schema SHA-256은 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`입니다.

정확한 shared blinding contract는 output slot별 의미이며 무조건적인 global pairwise claim이 아닙니다. `DBS-01`은 enabled non-all-private disclosure에서 user-vs-output randomness를 gate하고, `DBS-02`/`DBS-03`은 모든 enabled output에서 full-vs-output 및 full-vs-user를 유지합니다. All-private user blinding은 zero이며 disabled policy/randomness/blinding은 모두 zero입니다. Batch는 gated inequality site 96개를 이미 강제합니다. JoinSplit2x2는 recipient output `0`에만 적용되고 output `1`, input randomness, cross-output reuse는 이 세 circuit relation 범위 밖입니다. 더 강한 SDK global-freshness/structured-signer requirement는 별도 boundary로 유지합니다.

Keeper는 `BatchGasModelV1` precharge 전에 cheap bounded framing만 허용합니다. Canonical effect validation, audit identity, global nullifier/commitment lookup, historical-root/capacity, public-witness derivation, VK load, proof verification은 precharge 뒤 실행합니다. Proof가 성공해야 cache-context transition에 도달하고 nullifier, commitment, root snapshot, `privacy-scan-v2` record, minimal event가 atomic하게 commit됩니다. `TestBatchTransferCoreRejectionsAndAtomicScanFailure`와 `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`가 internal/cross-message rollback을 검증합니다.

측정된 development batch artifact identity는 R1CS `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, VK `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`입니다. 이 checksum은 artifact signing, provenance, reproducible generation, formal setup, external review를 대체하지 않습니다.

batch reference integration은 one-proof batch planner/preparer, bounded remote HTTP prover route, typed scanner/decrypt flow, durable payroll graph, staged CLI/tutorial의 experimental reference Go surface를 제공합니다. 이는 downstream JS/TS SDK, audited production workflow, production deployment profile이 아닙니다. One-proof `MsgBatchTransfer` path와 기존 multi-message `transfer-batch` flow를 구분하고 raw transport handler를 노출하지 않으며 모든 prover request를 매우 민감한 witness data로 취급합니다. Deposit CLI output은 `NotePlaintextV1` 또는 randomness를 출력하면 안 됩니다.

Artifact access와 proving은 계속 bounded해야 합니다.

- Validator는 exact consensus identity 비교 후 요청된 VK만 load하고 prover는 선택한 R1CS/PK pair를 lazy load합니다. Mismatch는 readiness를 실패시킵니다.
- Reference admission default는 circuit별 in-flight 1개, queued 4개이며 request body는 positive 8 MiB로 제한합니다. 0은 invalid입니다.
- Public deployment는 bounded `proverservice.Handler`를 사용하고 raw transport handler는 절대 직접 노출하지 않습니다. Automatic prover failover는 계속 비활성화합니다.
- Context cancellation은 이미 실행 중인 in-process solver를 preempt하지 못해 반환할 때까지 memory와 admission permit을 유지할 수 있습니다. Hard cancellation 또는 OOM containment가 security requirement이면 supervised, memory-limited worker process를 사용합니다.
