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
4. `tree_state`, `events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config` query가 노출되어야 합니다.
5. snapshot/restore rehearsal을 release 전 수행해야 합니다.

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
| transfer | +2        |
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

Remote prover는 privacy-sensitive trusted component입니다.

운영 baseline:

- private network 또는 edge proxy 뒤에 배치
- TLS 또는 mTLS
- mandatory auth
- request body limit
- timeout과 concurrency limit
- redacted logging
- artifact directory read-only mount
- `/healthz`, `/readyz` internal-only

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

- tx count by type: deposit/transfer/withdraw
- transfer disclosure mode distribution
- proof generation latency
- prover error rate
- nullifier rejection count
- Merkle `leaf_count`와 usage ratio
- failed `merkle_path` query
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

## 10. Incident 대응 기준

| 상황                       | 대응                                                                                     |
| -------------------------- | ---------------------------------------------------------------------------------------- |
| audit key compromise       | disclosure access 중단, key rotation/migration plan 실행, affected disclosure scope 산정 |
| prover token leak          | token rotate, access log review, proof endpoint abuse 확인                               |
| artifact checksum mismatch | node/prover start 중지, artifact source 재검증, release blocker 처리                     |
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
7. chain-specific threat model이 작성되어 있습니다.
