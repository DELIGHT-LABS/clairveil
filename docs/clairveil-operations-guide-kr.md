# Clairveil 운영 가이드

이 문서는 Clairveil을 downstream project가 가져다 쓸 때 운영 관점에서 놓치지 말아야 할 항목을 정리합니다.

Clairveil repo 자체는 production chain이 아니라 reusable privacy core와 reference host입니다. 따라서 이 문서는 “Clairveil이 제공하는 baseline”과 “downstream 운영팀이 반드시 결정해야 하는 것”을 분리해서 설명합니다.

## 1. 운영 책임 경계

| 영역             | Clairveil repo                        | Downstream project                                                |
| ---------------- | ------------------------------------- | ----------------------------------------------------------------- |
| Privacy module   | `x/privacy` 구현과 reference app 제공 | app wiring, store key, module account, governance/policy 결합     |
| Reference node   | `clairveild` local 검증               | validator 운영, sentry, snapshot, upgrade, monitoring             |
| ZK artifact      | 생성/검증 tooling 제공                | artifact signing, provenance, reproducible build, release custody |
| Prover           | `clairveil-proverd` reference service | topology, auth, quota, deployment, logging, retention             |
| Audit disclosure | genesis pubkey와 decode flow 제공     | master auditor private key custody, rotation, access control      |
| Wallet           | CLI/SDK helper와 fixture 제공         | browser/mobile storage encryption, UX, telemetry redaction        |

## 2. Node 운영 baseline

Production-like node는 최소 아래를 만족해야 합니다.

1. genesis에 audit master pubkey가 설정되어야 합니다.
2. ZK artifact preflight는 `strict`로 운영해야 합니다.
3. privacy module account가 bank module account로 올바르게 등록되어야 합니다.
4. `tree_state`, `commitment_info`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, batch `nullifiers` query가 노출되어야 합니다.
5. snapshot/restore rehearsal을 release 전 수행해야 합니다.
6. `Msg/BatchTransfer`는 four-circuit `privacy-note-v1` consensus identity와 일치하는 local batch VK가 있을 때만 활성화해야 합니다.

Reference local start 예:

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict

clairveild start --minimum-gas-prices 0uclair
```

## 3. ZK artifact 운영

`clairveil-setup`은 R1CS/PK/VK와 checksum manifest를 생성합니다.

```bash
clairveil-setup --out artifacts/privacy
```

`privacy-note-v1`은 `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1` exact order의 descriptor를 요구합니다. Validator는 consensus identity를 검사하고 네 VK만 load하며 prover-role readiness는 선택한 R1CS/PK pair를 lazy load합니다. 기록된 Session 3A development batch artifact에서 R1CS는 `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK는 `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, VK는 `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`입니다. Generation peak RSS는 `3,308,797,952 B`, role readiness peak RSS는 `1,295,482,880 B`였습니다.

이는 development identity일 뿐입니다. Formal trusted setup, artifact signing, reproducible production generation, custody, distribution은 downstream release 책임입니다.

Production에서는 아래 정책이 필요합니다.

- artifact 생성 commit 기록
- artifact generation command 기록
- checksum manifest 보관
- artifact signer 또는 release signer 지정
- runtime preflight `strict`
- stale artifact와 verifier mismatch를 release blocker로 처리

관련 문서:

- `docs/clairveil-circuits-kr.md`
- `docs/clairveil-proverd-remote-production-profile-kr.md`
- `docs/clairveil-security-best-practices-review-kr.md`

## 4. Merkle tree 운영

현재 privacy pool은 depth 32 single Merkle tree입니다.

| tx       | leaf 변화 |
| -------- | --------- |
| deposit  | +1        |
| native 2x2 transfer | +2        |
| batch transfer | +1..32 |
| withdraw | +0        |

운영자가 봐야 할 값:

- `leaf_count`
- `max_leaves`
- `remaining_leaves`
- current root
- historical root retention

권장 alert threshold:

| 사용률 | 의미                                           |
| ------ | ---------------------------------------------- |
| 50%    | 장기 capacity trend 확인 시작                  |
| 70%    | 새 pool/circuit upgrade 논의 시작              |
| 85%    | upgrade plan 확정 필요                         |
| 95%    | 신규 대량 유입 제한 또는 migration window 준비 |

Snapshot/restore/migration 후에는 [clairveil-merkle-restore-sop-kr.md](clairveil-merkle-restore-sop-kr.md)에 따라 샘플 Merkle path를 직접 재계산해야 합니다.

## 5. Prover 운영

`clairveil-proverd`는 private seed를 직접 받지는 않지만 prepared proof payload를 받습니다. 이 payload에는 amount, note randomness, Merkle path, nullifier, shielded public key, disclosure metadata가 포함될 수 있습니다.

Reference prover는 BatchJoinSplit16x32를 `POST /v1/proofs/batch-transfer`로만 제공합니다. `MsgBatchTransfer` witness를 generic 또는 기존 JoinSplit endpoint로 보내면 안 됩니다. Batch route는 strict framing/body bound와 circuit별 queue/in-flight admission을 적용하고 실제 prove call이 끝날 때까지 permit을 유지합니다. Automatic multi-prover failover는 비활성화되어 있습니다.

Remote prover는 privacy-sensitive trusted component입니다.

운영 baseline:

- private network 또는 edge proxy 뒤에 배치
- TLS 또는 mTLS
- mandatory auth
- request body limit
- timeout과 concurrency limit
- redacted logging
- artifact directory read-only mount
- `/healthz`, `/readyz`, `/debug/vars` internal-only

자세한 내용은 [clairveil-proverd-remote-production-profile-kr.md](clairveil-proverd-remote-production-profile-kr.md)를 기준으로 합니다.

## 6. Audit key 운영

모든 transfer에는 mandatory audit disclosure가 포함됩니다. 따라서 audit master private key는 모든 shielded transfer의 from/to/amount/asset 정보를 볼 수 있는 강한 권한입니다.

Production에서는 아래가 필요합니다.

- key generation ceremony
- HSM/KMS 또는 equivalent custody
- decrypt 권한 분리
- access log와 approval workflow
- rotation/migration plan
- compromised key incident response
- auditor UX에서 disclosure verification 강제

Clairveil repo는 private key custody를 구현하지 않습니다.

## 7. Wallet 운영

Reference CLI는 local JSON file을 restrictive permission으로 저장합니다. 이것은 개발 편의 baseline이지 production wallet storage가 아닙니다.

Production wallet은 아래를 결정해야 합니다.

- root seed와 derived secret encryption
- viewing key storage policy
- note cache encryption
- prepared payload/proof JSON retention
- telemetry redaction
- remote prover trust boundary UX
- disclosure decode 결과의 verification 표시

## 8. Monitoring

권장 metric:

- tx count by type: deposit/native-transfer/batch-transfer/withdraw
- batch input/output count, deterministic precharge, out-of-gas rejection, atomic rollback error
- transfer disclosure mode distribution
- proof generation latency
- prover error rate
- nullifier rejection count
- Merkle `leaf_count`와 usage ratio
- failed `merkle_path` query
- reserve `invariant_holds=false`
- artifact preflight failure
- remote prover auth failure
- remote prover body limit rejection

권장 log redaction:

- private seed, mnemonic, scalar
- viewing key, disclosure private key
- prepared payload body
- proof bytes
- bearer token
- decrypted disclosure payload

## 9. Release 운영

Release 전 maintainer baseline:

```bash
make release-check
make release-pack
make release-pack-verify
```

Prover image를 함께 넘기면:

```bash
make docker-proverd-build
```

Release note에는 최소 아래를 포함합니다.

- proto/fixture/schema/CLI/prover contract impact
- ZK artifact impact
- accepted vulnerability
- downstream action required
- artifact checksum/provenance policy
- circuit-set/public-witness/gas/scan-schema version 영향(`privacy-note-v1`, `BatchGasModelV1`, `privacy-sequence-v1`, `privacy-scan-v2`)

## 10. Incident 대응 기준

| 상황                       | 대응                                                                                     |
| -------------------------- | ---------------------------------------------------------------------------------------- |
| audit key compromise       | disclosure access 중단, key rotation/migration plan 실행, affected disclosure scope 산정 |
| prover token leak          | token rotate, access log review, proof endpoint abuse 확인                               |
| artifact checksum mismatch | node/prover start 중지, artifact source 재검증, release blocker 처리                     |
| reserve invariant mismatch | release/rollout 중지, module account balance와 deposit/withdraw totals 비교, direct send, top-up, migration write 조사 |
| Merkle restore mismatch    | node resume 금지, offline rebuild 또는 restore 재시도                                    |
| wallet cache corruption    | cache backup 후 rescan, 사용자의 seed/key 보존 여부 확인                                 |

## 11. Mainnet 전 최소 gate

Clairveil core를 downstream mainnet에 붙이기 전 최소 gate:

1. downstream app e2e가 deposit/transfer/disclosure/withdraw를 통과합니다.
2. JS/web wallet이 conformance fixture와 live chain test를 통과합니다.
3. remote/local/browser prover topology가 결정되어 있습니다.
4. audit key custody가 문서화되어 있고 rehearsal이 끝났습니다.
5. artifact signing/provenance 정책이 있습니다.
6. snapshot/restore rehearsal과 Merkle path sample 검증이 끝났습니다.
7. 지원 denom별 deposit/withdraw e2e 이후 `reserve/{denom}`이 `invariant_holds=true`를 반환합니다.
8. chain-specific threat model이 작성되어 있습니다.
9. Release commit 기준 `TestBatchTransferDirectCoreIntegration`, atomic scan-failure test, cross-message 2x2+batch/batch+batch rollback test가 통과합니다.
10. Session 3B SDK, remote batch prover route, typed scanner/decrypt 경로, one-proof payroll integration, CLI/tutorial, conformance fixture, 실제 localnet workflow를 함께 통과시키고 formal setup, production artifact release, external audit, downstream wallet product는 별도 gate로 유지합니다.
