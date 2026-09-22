# Clairveil 아키텍처

> English version: [clairveil-architecture.md](clairveil-architecture.md)

이 문서는 현재 repository 경계와 data flow를 설명합니다. Current asset/audit-management message와 live audit configuration query는 `proto/clairveil/privacy/v2`에 있고 wallet scan/tree/reserve query는 `proto/clairveil/privacy/v1`에 남아 있습니다. Circuit contract, schema, conformance fixture는 [문서 index](README-kr.md)에서 연결합니다.

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
        +-- Merkle/nullifier/reserve/asset/scan state와 audit-key schedule
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
| `proto/clairveil/privacy/v1` | Wallet scan/tree/reserve query API와 보존 legacy contract |
| `proto/clairveil/privacy/v2` | Current asset/admin Msg API와 live audit configuration/key query API |
| `scripts/` | Setup, localnet/e2e, evidence, benchmark, release automation |
| `docs/`, `tmpdocs/` | 현재 공개 지식과 ignored plan/archive/draft |

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

Wallet이 shielded recipient를 파생하고 output effect와 deposit proof를 만든 뒤 mandatory audit authorization이 포함된 V2 `MsgDeposit`을 broadcast합니다. Keeper는 proof, asset, authorization binding을 검증하고 transparent fund를 module account로 옮기며 commitment와 reserve/typed scan state를 갱신합니다. Execution event에는 external auditor가 쓰는 최소 original-transaction 위치 metadata만 있으며 encrypted payload archive는 없습니다.

### JoinSplit2x2 transfer

Wallet은 note scan/selection, Merkle path query, chain/expiry/output/disclosure intent 고정, canonical payload signing을 수행한 뒤 local 또는 명시적으로 선택한 prover 하나에서 증명합니다. `MsgTransfer` verification은 used nullifier, invalid root/expiry/signature/proof, disclosure contract 위반을 거부한 뒤 input 소비와 output append를 atomically 수행합니다.

### BatchJoinSplit16x32 transfer

batch reference integration flow는 1..16 input과 1..32 payment/change/padding output을 준비하고 proof 하나와 `MsgBatchTransfer` 하나를 만듭니다. `transfer-batch-16x32`가 이 flow입니다. Legacy `transfer-batch` command는 여러 독립 `MsgTransfer`를 한 Cosmos transaction에 넣으며 batch circuit protocol이 아닙니다.

### Withdraw

Owner는 transparent recipient, chain, expiry, nullifier, proof를 prepared payload에 binding합니다. Relayer는 Cosmos `creator`를 바꿀 수 있지만 owner-bound 값은 바꿀 수 없습니다. 성공하면 note를 소비하고 module account의 transparent amount를 recipient에게 보냅니다.

모든 keeper transition은 atomic이어야 합니다. Duplicate/conflicting nullifier, proof error, gas failure, mid-transition error가 발생하면 commitment, scan record, reserve delta, bank movement가 부분적으로 남으면 안 됩니다.

## 5. Proving과 artifact 경계

현재 실행 가능한 set은 development-only `privacy-note-v1-u128-audit-field-v1` identity입니다. 네 개의 ordered audit-field circuit과 exact local artifact가 필요합니다. Validator는 matching VK를, `clairveil-proverd`는 선택한 R1CS/PK도 load합니다. Checksum environment variable은 preflight input일 뿐 consensus를 override하지 못합니다. 이전 `privacy-note-v1` NoteV1/batch set은 현재 V2 runtime이 아닌 보존 legacy 자료입니다.

Prepared prover request에는 private note witness가 들어 있습니다. Same-endpoint retry가 두 번째 prover로 failover할 권한을 뜻하지 않습니다. Multi-prover failover는 explicit privacy decision이어야 합니다. `clairveil-proverd`는 bounded reference 구현이며 그 자체로 production trust boundary가 되지 않습니다.

## 6. Client와 audit 경계

### 6.1 Audit-field V2 proving 경계

현재 remote route는 `POST /v2/prover/audit-field`뿐입니다. Request/response envelope `v1`은 `privacy-note-v1-u128-audit-field-v1`, exact artifact hash, base64 `[]byte` final PI23, complete witness를 담습니다. Response는 모든 binding field를 반복하며 client는 V2 message를 만들기 전에 exact local artifact identity와 final PI23으로 local verification을 수행합니다. [HTTP API](clairveil-proverd-http-api-kr.md)를 보세요. `/v1` deposit/transfer/withdraw/batch 설명은 보존 legacy history일 뿐입니다.

Wallet은 typed chain data를 scan하고 note decrypt를 시도해 ownership을 복구합니다. `view_tags`는 untrusted performance hint일 뿐입니다. Client는 cursor 저장, rescan, prepared payload/note cache 암호화가 필요하고 nullifier query를 privacy-sensitive하게 다뤄야 합니다.

모든 V2 asset transaction에는 mandatory audit authorization이 있습니다. User-selected disclosure와 sender self-view disclosure는 서로 다른 envelope입니다. On-chain validation은 frozen digest/envelope contract를 검증하지만 audit epoch private key custody와 authorization은 외부 운영 책임입니다. Provenance는 external atomic cache의 original successful transaction과 execution event에서 파생하며 consensus state에는 transaction audit ledger나 replay archive가 없습니다.

## 7. 호환성과 authority

현재 fixed client contract는 `/v2/prover/audit-field`, request/response envelope `v1`, `privacy-note-v1-u128-audit-field-v1`, base64 byte slice, final PI23 local verification입니다. 이전 NoteV1 artifact, queued proof, cached prepared payload, note/scan cache, legacy genesis는 V2 runtime과 호환되지 않습니다. 해당 fixture는 역사 conformance evidence로 보존하되 V2에 제출하지 마세요.

자료가 충돌할 때 해당 contract의 판단 순서는 아래와 같습니다.

1. 실행 동작은 compiled proto/message/query definition과 keeper validation
2. Frozen cross-language encoding은 normative circuit contract, schema, conformance fixture
3. Command surface는 CLI help와 CLI reference
4. Integration 배경과 운영 맥락은 current guide

Release file membership은 `scripts/release-pack-paths.txt`와 `scripts/release-pack-required-files.txt`만 정의합니다. [Maintainer instructions](../CONTRIBUTING-kr.md)를 참고하세요.
