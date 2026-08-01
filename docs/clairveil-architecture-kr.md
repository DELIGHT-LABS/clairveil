# Clairveil 아키텍처

> English version: [clairveil-architecture.md](clairveil-architecture.md)

이 문서는 현재 repository 경계와 data flow를 설명합니다. Normative wire field와 hash는 [문서 index](README-kr.md)가 연결하는 `proto/clairveil/privacy/v1`, circuit contract, schema, conformance fixture에 있습니다.

## 1. 시스템 경계

```text
wallet / Go SDK / payroll
        | query, signed owner intent, prepared proof request
        v
downstream Cosmos app 또는 clairveild ---- optional HTTP ----> clairveil-proverd
        |                                                   |
        | Msg/Query                                         | R1CS + PK
        v                                                   v
     x/privacy -------------------------------------- ZK artifact set
        |
        +-- consensus verification과 atomic state transition
        +-- Merkle/nullifier/reserve/asset/scan/audit state
        +-- privacy module account의 bank keeper transfer
```

`clairveild`는 standalone reference host입니다. Downstream production chain은 `x/privacy`를 import하고 wiring하지만 chain configuration, validator 운영, artifact distribution, audit-key custody, wallet security, EVM/policy integration은 계속 직접 소유합니다.

## 2. Repository 지도

| Path | 책임 |
| --- | --- |
| `app/` | Reference Cosmos SDK application과 keeper/module wiring |
| `cmd/clairveild` | Daemon root command와 reference chain process |
| `x/privacy/types` | Message, query, genesis/state contract, key, fixed encoding |
| `x/privacy/keeper` | Proof check, bank transition, Merkle/nullifier/reserve/asset/scan state |
| `x/privacy/circuit` | Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32 circuit |
| `x/privacy/zk` | Artifact manifest, identity, setup, preflight, proof registry |
| `x/privacy/client/cli` | Wallet/operator tx와 direct query command |
| `x/privacy/client/sdk` | Go wallet, prepared-payload, scanner, prover transport, conformance helper |
| `cmd/clairveil-proverd` | Bounded reference HTTP prover service |
| `cmd/clairveil-payroll*` | Reference payroll control plane과 daemon |
| `proto/clairveil/privacy/v1` | Public Msg, Query, genesis wire API |
| `scripts/` | Setup, localnet/e2e, evidence, benchmark, release automation |
| `docs/`, `plans/`, `tmpdocs/` | 현재 지식, 구현 계획, ignored archive/draft |

## 3. Consensus state

`x/privacy`의 Cosmos module consensus version은 2입니다. KV state에는 아래가 들어 있습니다.

- 사용된 nullifier와 commitment index
- 현재 Merkle node, historical root, root snapshot
- audit key identity/epoch와 disclosure configuration
- privacy event sequence와 typed scan summary/output
- denom별 deposit/withdraw reserve accounting
- denom/asset ID별 asset registry entry
- exact consensus circuit-set identity

Deposit/withdraw는 bank keeper를 통해 privacy module account와 transparent account 사이에서 coin을 이동합니다. Reserve query는 counter와 module-account balance를 비교합니다. Direct top-up이나 무관한 bank movement가 있으면 reported invariant가 실패할 수 있습니다.

## 4. Transaction data flow

### Deposit

Wallet이 shielded recipient를 파생하고 note commitment와 deposit proof를 만든 뒤 `MsgDeposit`을 broadcast합니다. Keeper는 proof/asset binding을 검증하고 transparent fund를 module account로 옮기며 commitment, reserve/scan state, encrypted note event를 기록합니다.

### JoinSplit2x2 transfer

Wallet은 note scan/selection, Merkle path query, chain/expiry/output/disclosure intent 고정, canonical payload signing을 수행한 뒤 local 또는 명시적으로 선택한 prover 하나에서 증명합니다. `MsgTransfer` verification은 used nullifier, invalid root/expiry/signature/proof, disclosure contract 위반을 거부한 뒤 input 소비와 output append를 atomically 수행합니다.

### BatchJoinSplit16x32 transfer

batch reference integration flow는 1..16 input과 1..32 payment/change/padding output을 준비하고 proof 하나와 `MsgBatchTransfer` 하나를 만듭니다. `transfer-batch-16x32`가 이 flow입니다. Legacy `transfer-batch` command는 여러 독립 `MsgTransfer`를 한 Cosmos transaction에 넣으며 batch circuit protocol이 아닙니다.

### Withdraw

Owner는 transparent recipient, chain, expiry, nullifier, proof를 prepared payload에 binding합니다. Relayer는 Cosmos `creator`를 바꿀 수 있지만 owner-bound 값은 바꿀 수 없습니다. 성공하면 note를 소비하고 module account의 transparent amount를 recipient에게 보냅니다.

모든 keeper transition은 atomic이어야 합니다. Duplicate/conflicting nullifier, proof error, gas failure, mid-transition error가 발생하면 commitment, scan record, reserve delta, bank movement가 부분적으로 남으면 안 됩니다.

## 5. Proving과 artifact 경계

Active set `privacy-note-v1`은 deposit, spend, joinsplit, `batch-joinsplit-16x32-v1`입니다. Consensus는 ordered circuit identity, VK hash, public-input schema hash를 pin합니다. Validator는 matching VK, prover는 선택한 circuit의 R1CS/PK도 load합니다. Checksum environment variable은 preflight input일 뿐 consensus를 override하지 못합니다.

Prepared prover request에는 private note witness가 들어 있습니다. Same-endpoint retry가 두 번째 prover로 failover할 권한을 뜻하지 않습니다. Multi-prover failover는 explicit privacy decision이어야 합니다. `clairveil-proverd`는 bounded reference 구현이며 그 자체로 production trust boundary가 되지 않습니다.

## 6. Client와 audit 경계

### 6.1 Deposit proving 경계

Deposit proving은 local 또는 `POST /v1/prover/deposit`을 사용합니다. Client가 note, commitment, encrypted note를 구성하고 remote prover는 versioned circuit witness만 받아 commitment를 재계산하여 commitment-bound proof를 반환합니다. Client는 response를 검증한 뒤 `MsgDeposit`을 조립·서명합니다. Envelope와 nested payload/proof version은 분리됩니다. Transport policy는 [general HTTP API](clairveil-proverd-http-api-kr.md), route wire contract는 [deposit API](clairveil-proverd-deposit-api-kr.md)가 소유합니다.

Wallet은 typed chain data를 scan하고 note decrypt를 시도해 ownership을 복구합니다. `view_tags`는 untrusted performance hint일 뿐입니다. Client는 cursor 저장, rescan, prepared payload/note cache 암호화가 필요하고 nullifier query를 privacy-sensitive하게 다뤄야 합니다.

모든 transfer에는 mandatory audit disclosure가 있습니다. User-selected disclosure와 sender self-view disclosure는 서로 다른 envelope입니다. On-chain validation은 frozen digest/envelope contract를 검증하지만 audit private key custody와 authorization은 외부 운영 책임입니다.

## 7. 호환성과 authority

현재 fixed client contract는 `privacy-fixed-v1`, transfer payload `v5`, transfer/withdraw proof contract `v2`입니다. 이전 artifact, queued proof, cached prepared payload, note/scan cache, three-circuit genesis와 호환되지 않습니다. Exact artifact set 재생성, fresh genesis/reset, incompatible job/cache 삭제, rescan으로 upgrade합니다.

자료가 충돌할 때 해당 contract의 판단 순서는 아래와 같습니다.

1. 실행 동작은 compiled proto/message/query definition과 keeper validation
2. Frozen cross-language encoding은 normative circuit contract, schema, conformance fixture
3. Command surface는 CLI help와 CLI reference
4. 배경과 역사는 current guide와 completed plan record

Release file membership은 `scripts/release-pack-paths.txt`와 `scripts/release-pack-required-files.txt`만 정의합니다. [Release policy](clairveil-release-versioning-policy-kr.md)와 [maintainer instructions](clairveil-maintainer-instructions-kr.md)를 참고하세요.
