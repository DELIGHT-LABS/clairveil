# Session 1 Plan: 현재 Shielded Authorization과 JoinSplit 보안 수정

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Complete historical Gate 1; **2x2 disclosure constraint와 exact exploit regression 재진입 필요** (2026-07-12) |
| 선행 문서 | [BatchJoinSplit16x32 Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md) |
| 후속 세션 | [Session 2 Foundation](clairveil-batch-joinsplit-16x32-session-2-foundation-kr.md) |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `max` |
| 완료 목표 | current transfer/withdraw에서 확인된 inflation, authorization, disclosure, state-integrity, decoder 및 privacy-default 결함을 circuit과 host 양쪽에서 제거함 |

## 1. 시작 Gate

```bash
git status --short --branch
git log -5 --oneline
go test ./x/privacy/circuit ./x/privacy/types ./x/privacy/keeper -count=1
go test ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/withdraw -count=1
```

worktree가 clean하지 않으면 기존 변경을 되돌리지 않고 영향 범위를 먼저 확인함. 현재 코드가 계획의 전제와 다르면 실제 코드 기준으로 계획을 먼저 보정함.

## 2. 목적

16x32 회로는 current Note, nullifier, owner authorization을 확장함. 기존 결함을 먼저 제거하지 않으면 batch에서 피해 규모와 공격 표면이 확대됨.

이 세션은 batch를 구현하지 않음. 대신 current `JoinSplitCircuit`, `MsgTransfer`, `SpendCircuit`, `MsgWithdraw`가 안전한 공통 기준이 되도록 고침.

## 3. 확인된 보안 결함

### S1-01. Duplicate input/nullifier inflation

현재 2x2 회로와 keeper는 같은 note/nullifier를 두 input에 넣는 것을 명시적으로 막지 않음.

공격 witness:

- input 0과 1의 note field, commitment, Merkle path, owner key, signature가 같음.
- 두 public nullifier가 같음.
- output 합계를 실제 note amount의 두 배로 만듦.

protocol severity는 `Critical`, 현재 operational exposure는 배포 전이므로 `None`, production 공개 전 release blocker임.

### S1-02. Transfer owner signature가 final intent를 인증하지 않음

현재 signature는 사실상 `MiMC(amount, assetID, randomness)`만 인증함. prepared payload를 받은 malicious prover 또는 payload 탈취자는 signature를 유지한 채 recipient, amount, output, disclosure, ciphertext를 바꿔 새 proof를 만들 수 있음.

`creator`는 shielded owner가 아니라 fee-paying relayer이므로 공격자가 자기 account로 결과를 broadcast할 수도 있음.

### S1-03. Duplicate output commitment

같은 commitment를 두 output에 넣으면 동일 leaf value가 두 번 append되어 scanner/note identity/payroll evidence를 모호하게 함. current 2x2와 future batch 모두 active output commitment distinctness를 protocol invariant로 사용함.

### S1-04. Withdraw chain/expiry가 owner authorization에 묶이지 않음

현재 keeper는 `MsgWithdraw.chain_id`와 `expires_at_unix`를 확인하지만 `SpendCircuit` public statement와 owner signature는 이를 포함하지 않음.

따라서 proof를 받은 relayer가 다음을 할 수 있음.

- expiry를 임의로 미래로 연장함.
- 같은 root/asset 상태를 가진 다른 chain에서 message chain ID를 바꿔 proof를 재사용함.

recipient와 amount는 proof에 묶여 있으므로 직접적인 recipient theft와는 다르지만, owner가 설정한 handoff expiry와 chain boundary가 깨짐. current code가 배포 전인 지금 circuit에서 수정함.

### S1-05. Disclosure digest가 작은 평문의 offline oracle이 됨

현재 recipient/user, audit, self-view disclosure digest는 amount, asset, recipient처럼 후보 공간이 작은 값에 secret blinding 없이 결정적으로 계산됨. ciphertext를 복호화하지 못하는 관찰자도 후보 값을 대입해 digest를 비교할 수 있으므로 confidentiality claim이 약해짐.

- output마다 CSPRNG로 생성한 `user_disclosure_blinding`과 `full_disclosure_blinding`을 별도로 사용함.
- note randomness를 blinding으로 재사용하지 않음.
- blinding을 circuit digest와 해당 수신자가 복호화하는 disclosure plaintext 양쪽에 포함함.
- `all-private`는 user digest를 `0`으로 유지하되 audit/self-view full digest는 별도 blinding으로 보호함.

이 수정은 신규 batch에만 해당하지 않음. 현재 user disclosure, audit disclosure, self-view disclosure에도 이미 존재하는 문제임.

### S1-06. Withdraw recipient byte encoding이 모호함

현재 recipient address를 `big.Int.SetBytes(recipientAddr.Bytes())`로 field에 넣으면 선행 zero byte가 사라짐. 서로 다른 raw address가 같은 field 값으로 alias될 수 있으므로 SpendIntent에서 raw recipient를 직접 field로 해석하지 않음.

```text
recipient_digest = SHA-256(
  "clairveil.withdraw-recipient.v1" ||
  u32be(len(recipient_raw_address)) || recipient_raw_address
)
```

`RecipientDigestHi/Lo`를 public input과 owner intent에 넣고 128-bit range를 constrain함.

### S1-07. Commitment가 전역으로 유일하지 않음

현재 commitment tree는 같은 commitment append를 허용하고 latest index를 덮어쓸 수 있음. local output distinctness만으로는 과거 Deposit/JoinSplit output과의 충돌을 막지 못함.

- Deposit, JoinSplit2x2, 향후 BatchJoinSplit16x32, genesis import 전체에서 global commitment uniqueness를 강제함.
- `AppendCommitment` 자체도 duplicate에서 실패하도록 fail closed함.
- local duplicate 검사를 global lookup보다 먼저 수행함.
- duplicate rejection은 tree root, leaf count, latest index를 전혀 변경하지 않아야 함.

### S1-08. Cryptographic wire decoder가 비정상 encoding을 충분히 거부하지 않음

recipient/disclosure public key와 ECIES ephemeral point는 canonical encoding, curve membership, non-identity, subgroup membership을 공통 helper로 검증함. EdDSA signature는 정확히 64-byte `R || S`만 받고 canonical `R/S`와 scalar 범위를 확인하며 malformed input에서 panic하지 않아야 함. decoder/parser fuzz test를 release blocker로 둠.

### S1-09. ProverPool의 자동 multi-endpoint failover가 privacy boundary를 넓힘

현재 prover request는 witness와 disclosure metadata를 포함할 수 있는데 endpoint 실패 시 다른 prover로 자동 전달될 수 있음. 기본값을 single endpoint/no failover로 변경하고 multi-prover failover는 사용자 또는 제품 정책의 명시적 opt-in으로만 허용함. 기존 API, CLI 기본값, 문서와 테스트를 함께 바꿈.

### S1-10. Genesis root와 circuit artifact identity 검증이 충분하지 않음

- genesis의 historical root를 신뢰하지 않고 commitment prefix로 재계산해 일치 여부를 검증함.
- consensus/genesis에는 `circuit_set_id`, 정확한 VK checksum, public-input schema digest, descriptor set을 고정함.
- validator의 local artifact가 consensus identity와 다르면 readiness/startup에서 실패함.
- artifact override는 development-only로 제한함.

## 4. 고정 보안 불변식

### 4.1 Transfer

1. 두 input nullifier는 서로 다름.
2. 두 output commitment는 서로 다름.
3. 두 input은 같은 spend/view owner와 asset을 사용함.
4. input별 signature가 아니라 single owner signature 하나를 검증함.
5. owner signature는 final transfer intent 전체를 인증함.
6. exact message payload, chain domain, expiry가 proof와 signature에 묶임.
7. `creator`만 바꾸는 relayer sponsorship은 허용함.
8. disclosure digest는 독립적인 per-output secret blinding 없이는 생성하지 않음.
9. 모든 active output commitment는 현재 message 내부와 기존 global tree 전체에서 유일함.

### 4.2 Withdraw

1. owner signature는 root, nullifier, amount, asset, recipient, chain domain, expiry를 인증함.
2. keeper는 chain domain을 current `ctx.ChainID()`에서 계산함.
3. expiry 이후 state write 전에 실패함.
4. `creator`는 relayed withdraw를 위해 intent에서 제외함.
5. proof/message의 chain ID 또는 expiry를 바꾸면 verification이 실패함.
6. recipient는 length-prefixed raw bytes의 SHA-256 digest로 인증함.
7. expiry 경계는 `block_time >= expires_at_unix`이면 만료로 고정함.

## 5. Canonical digest contract

### 5.1 SHA-256 to two field limbs

canonical byte digest와 chain domain은 SHA-256으로 고정함. 결과를 field modulus로 reduce하지 않고 두 128-bit big-endian limb로 나눔.

```text
digest = SHA-256(domain || canonical_bytes)
digest_hi = uint128_be(digest[0:16])
digest_lo = uint128_be(digest[16:32])
```

각 limb는 128-bit range constraint를 적용할 수 있고 BN254 field에 canonical하게 들어감. 다음 항목에 같은 representation을 사용함.

- `ChainDomainHi`, `ChainDomainLo`
- `PayloadDigestHi`, `PayloadDigestLo`
- `RecipientDigestHi`, `RecipientDigestLo`

native와 circuit 모두 각 limb가 128-bit 범위임을 검증함. field modulus reduction이나 non-canonical oversized integer를 허용하지 않음.

### 5.2 Chain domain

```text
chain_domain_digest = SHA-256(
  "clairveil.chain-domain.v1" ||
  u32be(len(chain_id)) || chain_id ||
  u32be(len(circuit_set_id)) || circuit_set_id
)
```

SDK는 configured chain ID로 계산하고 keeper는 current context chain ID로 다시 계산함. circuit set ID가 바뀌면 chain domain도 바뀜.

### 5.3 Canonical transfer payload

JSON/protobuf raw serialization을 hash source로 사용하지 않음. 다음 effect-bearing field를 versioned canonical binary encoder로 순서대로 encoding함.

- format version
- root
- ordered nullifiers
- ordered output commitments
- ordered ciphertexts
- ordered view tags
- user policy/mode/digest/target/payload
- audit digest/target/payload
- self-view digest/payload
- expiry

가변 byte field는 `u32be(length) || bytes` 형식을 사용함.

제외 field:

- Groth16 proof
- `creator`
- tx signature/sequence/gas/memo
- payload digest 자신

Keeper는 message에서 payload digest를 직접 계산함. wire에 같은 digest를 별도 field로 중복 저장하지 않음.

### 5.4 Domain constants

최소 다음 domain을 field constant와 byte domain으로 구분해 정의하고 golden vector로 고정함.

```text
clairveil.chain-domain.v1
clairveil.transfer-payload.v1
clairveil.withdraw-recipient.v1
clairveil.user-disclosure.v2
clairveil.full-disclosure.v2
CLAIRVEIL_TRANSFER_INTENT_V2
CLAIRVEIL_SPEND_INTENT_V2
CLAIRVEIL_NULLIFIER_SET_V1
CLAIRVEIL_COMMITMENT_SET_V1
```

작은 임의 정수 tag를 다른 hash family에 재사용하지 않음.

## 6. TransferIntentV2

current 2x2의 ordered nullifier/commitment set digest를 circuit과 native helper가 동일하게 계산함.

```text
transfer_intent = MiMC(
  TRANSFER_INTENT_V2_DOMAIN,
  chain_domain_hi,
  chain_domain_lo,
  circuit_kind,
  merkle_root,
  2,
  2,
  asset_id,
  nullifier_set_digest,
  commitment_set_digest,
  user_disclosure_digest,
  full_disclosure_digest,
  payload_digest_hi,
  payload_digest_lo,
  expires_at_unix,
)
```

두 input spend/view key가 slot 0과 같은지 먼저 constrain하고, `OwnerSignature` 하나를 `InputSpendPubKeys[0]`로 검증함. per-input authorization message와 `[2]InputSignatures`를 제거함.

single signature가 충분한 이유:

- 각 input commitment가 해당 spend key를 포함함.
- 각 input membership과 nullifier가 circuit에서 검증됨.
- 모든 input spend key가 slot 0 key와 같음.
- intent가 두 nullifier와 두 output commitment를 모두 묶음.

JoinSplit2x2 public input 순서는 아래로 고정하고 manifest의 public-input schema digest에 반영함.

```text
1  MerkleRoot
2  ChainDomainHi
3  ChainDomainLo
4  ExpiresAtUnix
5  Nullifier0
6  Nullifier1
7  Commitment0
8  Commitment1
9  UserPrivacyPolicy
10 UserDisclosureDigest
11 FullDisclosureDigest
12 PayloadDigestHi
13 PayloadDigestLo
```

`circuit_kind`는 public input을 중복 추가하지 않고 intent domain의 compile-time constant로 고정함. `FullDisclosureDigest`는 output 0의 audit/self-view 검증이 공유하는 blinded full digest로 정의함. output별 확장은 Session 2에서 고정함.

```text
user_digest = MiMC(USER_DISCLOSURE_V2_DOMAIN, disclosed_fields..., user_disclosure_blinding)
full_digest = MiMC(FULL_DISCLOSURE_V2_DOMAIN, full_fields..., full_disclosure_blinding)
```

## 7. SpendIntentV2

`SpendCircuit` public input에 `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`를 추가함.

```text
spend_intent = MiMC(
  SPEND_INTENT_V2_DOMAIN,
  chain_domain_hi,
  chain_domain_lo,
  circuit_kind,
  merkle_root,
  nullifier,
  amount,
  asset_id,
  recipient_digest_hi,
  recipient_digest_lo,
  expires_at_unix,
)
```

owner signature는 이 digest를 인증함. keeper는 message의 `chain_id`를 신뢰해 field를 만들지 않고 current context에서 계산하며, message chain ID가 context와 다른 경우 먼저 실패함.

`expires_at_unix`는 positive signed 64-bit 범위로 host validation하고 circuit에서는 non-negative 64-bit representation으로 constrain함.

SpendV2 public input 순서는 아래로 고정함.

```text
1 MerkleRoot
2 ChainDomainHi
3 ChainDomainLo
4 ExpiresAtUnix
5 Nullifier
6 Amount
7 RecipientDigestHi
8 RecipientDigestLo
9 AssetID
```

## 8. 구현 Work Package

### A. Duplicate nullifier/input 방어

수정 후보:

- `x/privacy/circuit/joinsplit.go`
- `x/privacy/types/msg.go`
- `x/privacy/keeper/msg_server.go`
- `x/privacy/client/sdk/transfer`
- `x/privacy/client/sdk/payroll`
- 관련 tests

구현 원칙:

- circuit에 nullifier distinctness를 추가함.
- types/keeper/SDK는 reusable canonical `[32]byte` local set을 사용함.
- keeper local duplicate 검사는 global `HasNullifier`보다 먼저 실행함.
- payroll operation 안에서도 note/nullifier uniqueness를 검증함.

### B. Duplicate output commitment 방어

- circuit distinctness
- types/keeper local set
- SDK fresh randomness와 result check
- tree append 전 failure
- global `HasCommitment` lookup과 duplicate-rejecting `AppendCommitment`
- Deposit/JoinSplit/genesis import 공통 invariant

### C. Canonical digest/intent helper

책임:

- SHA-256 canonical encoder
- two-limb digest conversion
- chain domain
- 2x2 ordered set digest
- TransferIntentV2
- SpendIntentV2
- field/byte domain constants
- golden vectors

Circuit package가 client/keeper를 import하지 않도록 ownership boundary를 지킴. circuit/native 구현은 fixture로 교차 검증함.

최소 golden vector:

- chain ID/circuit set -> hi/lo
- canonical transfer message bytes -> hi/lo
- reordered nullifier/commitment -> different set digest
- transfer intent
- spend intent
- one-byte payload mutation

### D. JoinSplit circuit/SDK 재구성

- `[2]InputSignatures`를 `OwnerSignature` 하나로 교체함.
- output/disclosure/ciphertext를 먼저 확정한 뒤 payload digest와 intent를 계산하고 한 번 서명함.
- public input에 chain domain limbs, payload digest limbs, expiry를 추가함.
- Keeper public witness builder도 같은 순서를 사용함.
- prepared transfer payload/proof version을 올림.
- legacy decode를 제공하지 않음.

### E. Spend circuit/SDK 재구성

- signature message를 SpendIntentV2로 교체함.
- chain domain limbs와 expiry를 public witness에 추가함.
- prepared withdraw prover payload가 owner-signed chain/expiry와 일치하는지 검증함.
- relayer가 expiry/chain ID를 바꾸면 proof verification이 실패함.
- recipient와 creator의 역할을 문서에서 구분함.
- raw recipient를 length-prefixed digest로 바꾸고 leading-zero alias vector를 추가함.

### F. Proto/CLI/prover contract

- `MsgTransfer`에 `expires_at_unix`를 추가함.
- `MsgWithdraw`의 existing chain/expiry field는 유지하되 proof-bound 의미로 바꿈.
- `make proto`로 generated code를 갱신함.
- transfer/withdraw prepared payload/proof version을 올림.
- CLI가 absolute expiry와 chain ID를 명시적으로 보여줌.
- prover request/response, fixture, schema, JS handoff를 갱신함.
- `creator` replacement positive test를 유지함.

### G. Circuit set/artifact

- changed Deposit/NoteV1은 Session 2 범위이므로 이 세션에서는 JoinSplit/Spend circuit set 의미만 갱신함.
- source와 manifest descriptor/checksum contract를 commit함.
- 임시 development artifact를 생성해 strict preflight/E2E를 수행함.
- R1CS/PK/VK binary는 commit하지 않음.
- formal trusted setup은 수행하지 않음.
- consensus/genesis circuit descriptor와 local manifest/VK checksum/schema digest를 정확히 비교함.
- mismatch validator가 시작 또는 readiness를 통과하지 못하는 integration test를 추가함.

### H. Disclosure/decoder/privacy default 수정

- current user/audit/self-view digest에 독립적인 CSPRNG blinding을 추가함.
- disclosure plaintext/envelope version을 올리고 decrypt 후 digest 재계산 계약을 고정함.
- recipient/disclosure/ephemeral curve point와 EdDSA signature 공통 canonical decoder를 구현함.
- malformed point/signature fuzz와 no-panic property test를 추가함.
- scanner는 view-tag mismatch에서도 full decrypt를 시도하는 현재 안전 기본값을 유지함. tag-only fast scan은 명시적 privacy/performance opt-in으로만 제공함.
- `ProverPool`은 기본 no-failover이며 explicit opt-in 없이는 payload를 다른 endpoint에 보내지 않음.

### I. Genesis/state/gas hardening

- historical roots를 commitment prefix에서 재계산하고 forged root genesis를 거부함.
- current proof verification의 explicit gas precharge를 cheap framing 검사 뒤, cryptographic work 전에 수행함.
- failure path가 tree/nullifier/scan state를 부분 갱신하지 않음을 검증함.

### J. Public docs

실제 영향을 받는 한영 문서:

- circuit guide
- threat model
- security review
- JS SDK handoff
- client API checklist/UX
- CLI reference
- prover profile
- relayed withdraw handoff/expiry 설명

prepared prover payload는 final output을 변경할 수 없게 되지만 여전히 note witness를 포함하는 privacy-sensitive payload임을 설명함.

breaking handoff 항목으로 disclosure plaintext version, Spend/Transfer public-input 순서, recipient digest, prover failover 기본값, artifact identity를 명시함.

## 9. 공격 회귀 테스트

### T1. Exploit-shaped duplicate input

same note, commitment, path, owner, nullifier를 두 input에 넣고 outputs를 `2 * amount`로 구성함. 수정 후 prover가 실패해야 함.

### T2. Host local duplicate

global nullifier store가 비어 있어도 types/keeper에서 proof verification 전에 실패해야 함.

### T3. Duplicate output commitment

circuit/types/keeper 모두 실패해야 함.

### T4. Transfer output redirection

valid owner signature 후 recipient, amount, commitment를 변경하면 실패해야 함.

### T5. Transfer metadata substitution

ciphertext, view tag, user/audit/self-view payload/target를 변경하면 keeper-computed payload digest와 proof가 달라져 실패해야 함.

### T6. Transfer cross-chain/expiry

chain A용 proof를 chain B context에서 검증하거나 expiry를 연장하면 실패해야 함.

### T7. Withdraw expiry extension

valid relayed withdraw proof의 message expiry만 미래로 변경하면 실패해야 함.

### T8. Withdraw cross-chain replay

같은 root state를 fixture로 만든 다른 chain domain에서 proof가 실패해야 함.

### T9. Relayer replacement

transfer/withdraw 모두 `creator`만 변경한 제출은 성공해야 함.

### T10. Disclosure dictionary attack

동일한 공개 후보 field를 사용해도 서로 다른 blinding의 digest가 달라야 하고, blinding 없이 후보 amount/recipient를 대입한 공격자가 digest를 맞히지 못해야 함.

### T11. Recipient leading-zero alias

서로 다른 `00 || x`와 `x` raw address가 서로 다른 recipient digest와 SpendIntent를 만들어야 함.

### T12. Global commitment collision

과거 Deposit/2x2 output과 같은 commitment를 새 Deposit/2x2 output으로 제출하면 proof verification 또는 append 전에 거부되고 state가 불변이어야 함.

### T13. Malformed crypto encoding

identity/non-canonical/non-subgroup point, truncated/oversized/non-canonical signature, malformed envelope를 fuzz하고 panic 없이 거부해야 함.

### T14. Prover privacy default

첫 endpoint timeout 시 opt-in이 없으면 두 번째 endpoint가 호출되지 않아야 함. opt-in 모드에서도 endpoint 목록과 privacy warning이 명시되어야 함.

### T15. Forged genesis/artifact mismatch

forged historical root와 consensus descriptor에 맞지 않는 local VK/manifest로 startup/readiness가 실패해야 함.

## 10. 검증

```bash
go test ./x/privacy/circuit -count=1
go test ./x/privacy/types ./x/privacy/keeper -count=1
go test ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/withdraw -count=1
go test ./x/privacy/client/sdk/provertransport ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/payroll -count=1
make proto
go test ./x/privacy/... -count=1
make examples
make privacy-e2e-smoke
make reference-payroll-live-localnet
make release-check
make release-pack
make release-pack-verify
git diff --check
```

환경 의존 실패는 명령, 로그 요약, 원인, 재실행 조건을 completion record에 남김.

## 11. Commit 전략

1. `fix: reject duplicate joinsplit inputs and outputs`
2. `feat: bind transfers to a single owner intent`
3. `fix: bind withdraw proofs to chain and expiry`
4. `feat: version shielded prover and client contracts`
5. `docs: document shielded intent authorization`

실제 구현에서는 disclosure/decoder, global commitment/genesis, prover privacy default를 독립 commit으로 더 나눔. 각 commit은 관련 regression test가 통과해야 함.

각 commit은 관련 unit test가 통과하는 상태로 남김.

## 12. 범위 밖

- final NoteV1 formula 적용(단, current disclosure blinding과 global uniqueness 수정은 이 세션에서 수행함)
- fixed-size note/disclosure encoding
- BatchJoinSplit16x32
- MsgBatchTransfer
- batch scanner/payroll adapter
- formal trusted setup
- external audit
- migration/legacy compatibility

NoteV1의 새 commitment/nullifier formula는 Session 2에서 production circuit 전체에 적용함. 그러나 현재 wire decoder의 subgroup/canonical 검증은 기존 기능에도 영향을 주므로 Session 1에서 수정함. 이 세션 중 새 Critical/High current issue를 발견하면 범위 밖으로 미루지 않고 blocker로 기록함.

## 13. Acceptance Criteria

2026-07-12 independent revalidation이 아래 두 acceptance 증거를 재개방했다. Pairwise distinctness 구현 자체는 존재하지만 exact exploit-shaped regression이 아니며, SDK CSPRNG independence만으로 circuit-level secret-reuse invariant가 닫히지 않는다.

- [ ] same note/commitment/path/helper/nullifier와 doubled output을 사용한 exact duplicate input exploit witness가 distinctness constraint 때문에 실패함. (`S4-B03` 미검증)
- [x] duplicate nullifier/commitment가 types/keeper에서 빠르게 실패함.
- [x] commitment가 Deposit/2x2/genesis를 가로질러 전역으로 유일함.
- [x] SDK/payroll이 같은 note 재사용을 거부함.
- [x] 2x2가 single owner signature를 사용함.
- [x] transfer recipient/amount/payload substitution이 실패함.
- [x] transfer chain/expiry 변경이 실패함.
- [x] SpendIntentV2가 chain domain과 expiry를 proof-bound함.
- [x] withdraw expiry extension과 cross-chain replay가 실패함.
- [x] recipient leading-zero alias가 불가능함.
- [ ] current 2x2 user/audit/self-view disclosure가 circuit에서도 note randomness 및 서로의 exact reuse를 거부함. (`S4-B02`; SDK CSPRNG/dictionary regression만 통과)
- [x] malformed point/signature/envelope가 panic 없이 거부됨.
- [x] prover endpoint failover가 기본 비활성화되고 explicit opt-in만 허용됨.
- [x] forged historical root와 artifact identity mismatch가 startup/readiness에서 실패함.
- [x] proof verification gas가 cryptographic work 전에 precharge됨.
- [x] transfer/withdraw creator replacement가 성공함.
- [x] SHA-256 two-limb vector가 circuit/native/keeper에서 일치함.
- [x] prepared payload/prover/fixture/schema version이 일치함.
- [x] development artifact strict preflight와 E2E가 통과함.
- [x] 한영 문서가 실제 trust boundary와 일치함.
- [x] generated artifact binary/secret이 tracked되지 않음.
- [x] master ledger가 갱신됨.

## 14. Session 2 Handoff

## Completion Record

- 시작 commit: `e427370`
- 완료 commit: `14d85f5` (`Session 1` 구현·공개 계약, review-fix, prepared-transfer canonical key hardening 완료 기준; 이 최종 Completion Record/Ledger bookkeeping은 후속 문서 commit)
- circuit set ID: `privacy-intent-v2`; consensus `CircuitSetIdentity` schema `v1`; artifact manifest schema `v2`
- TransferIntent/SpendIntent version: `TransferIntentV2`, `SpendIntentV2`
- prepared payload/prover versions: transfer payload `v5`, transfer proof/request/response `v2`; withdraw prover/final payload, proof, request/response, relay schema/handoff `v2`; disclosure plaintext/query `v5`
- SHA-256 limb fixture:
  - chain domain `clairveil-localnet-1` + `privacy-intent-v2`: hi `264159934158684874548762591990728337770`, lo `270095241217876371844524170513424412119`
  - canonical transfer payload fixture: hi `167934897245902538552295964807751055480`, lo `315400652074988150791302303081971100397`
  - withdraw recipient raw bytes `00 01 02 03`: hi `211336406829810441348458686997852034571`, lo `265630251913956315626555014078061856515`
- 추가한 security invariant:
  - 2x2 input nullifier와 output commitment local distinctness, Deposit/2x2/genesis 전체의 global commitment uniqueness, duplicate 실패 시 state 불변성을 강제함.
  - 같은 spend/view owner의 단일 `OwnerSignature`가 ordered nullifier/commitment, final output/disclosure/ciphertext effect, current chain domain, absolute expiry를 포함한 `TransferIntentV2` 전체를 인증함. `creator`는 relayer 교체를 위해 의도적으로 제외함.
  - `SpendIntentV2`가 current chain domain, `block_time >= expiry` 경계, length-prefixed raw recipient SHA-256 limbs를 proof/signature에 묶어 cross-chain replay, expiry 연장, leading-zero alias를 거부함.
  - user/full disclosure에 서로 독립적인 CSPRNG blinding을 사용하고, point/signature/envelope를 canonical·curve·non-identity·prime-subgroup 조건으로 panic 없이 거부함.
  - prover multi-endpoint failover는 명시적 opt-in이 없으면 비활성화되고, historical root 재계산, consensus artifact identity 일치, proof canonical framing과 cryptographic work 전 gas precharge를 강제함.
- downstream breaking contract와 migration/reset 지침:
  - legacy prepared payload/proof/request/response와 disclosure file은 compatibility decode하지 않고 위 version으로 다시 생성함.
  - `privacy-intent-v2` 적용 시 cached proof job과 R1CS/PK/VK/manifest를 폐기·재생성하고 validator의 local VK identity를 consensus genesis/state와 맞춤. 기존 개발 체인은 새 genesis identity로 reset/reinitialize하며 production migration은 이 세션 범위에 포함하지 않음.
  - JS/TS client는 exact public-input 순서, big-endian non-reduced 128-bit SHA limbs, canonical transfer binary effect encoder를 그대로 구현해야 함.
- 실행한 검증과 결과:
  - 시작 gate의 circuit/types/keeper 및 transfer/withdraw unit test: 통과.
  - `make proto`: 통과, generated diff 없음.
  - `go test ./x/privacy/... -count=1`, `GOTOOLCHAIN=go1.25.12 go test ./... -count=1`: 통과.
  - `make examples`, `make privacy-e2e-smoke`, `make reference-payroll-live-localnet`: 통과. Payroll live 검증이 실제 `tx_hash` evidence 계약 불일치를 발견했고 `7807a85`에서 수정한 뒤 전체 재실행해 통과함.
  - `make vulncheck`: 통과. 새 fixed-version finding은 Go `1.25.12`와 dependency update로 제거했고, no-fixed-version 예외만 exact ID/module 정책으로 제한함.
  - `make release-check`: 통과. 내부 `make ci`, `make vulncheck`, `make localnet-smoke`, `make privacy-e2e-smoke`, `TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`가 모두 통과함.
  - `FuzzDecodeCanonicalPointNoPanic`, `FuzzDecodeCanonicalEdDSASignatureNoPanic`, `FuzzAsymDecryptMalformedEnvelopeNoPanic` 각 5초: 통과(각각 112,954 / 80,964 / 835,066 executions).
  - `make release-pack`, `make release-pack-verify`: 통과(필수 파일 110개와 내·외부 checksum/manifest commit 검증). `git diff --check`: 통과.
- 미해결 finding:
  - 현재 transfer/withdraw protocol의 미해결 Critical/High finding은 없음.
  - upstream fixed version이 없는 `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`는 exact policy exception과 downstream risk로 계속 추적함. `GO-2026-5932`는 Cosmos SDK의 local ASCII key armor 경로만 reachable하며 Clairveil은 OpenPGP signing/encryption을 사용하지 않음. Fixed path가 생기면 예외는 즉시 실패함.
  - JS example `npm audit`의 low-severity finding 1건, prepared witness JSON의 plaintext-at-rest 위험, formal trusted setup과 외부 audit 미수행은 공개 known risk/후속 production gate로 남음.
- Session 2가 재사용할 helper/vector:
  - `x/privacy/types/intent.go`의 `SplitDigestToLimbs`, `ComputeChainDomainV1`, `ComputeWithdrawRecipientDigestV1`, `CanonicalTransferPayloadBytesV1`, `ComputeTransferPayloadDigestV1`, `ComputeOrderedSetDigestV1`, `ComputeTransferIntentV2`, `ComputeSpendIntentV2`와 `x/privacy/types/intent_test.go` golden vector.
  - `x/privacy/zk/schema.go`의 ordered public-input schema/digest helper, `x/privacy/zk/manifest.go`의 exact descriptor/identity helper, `x/privacy/crypto/decoder.go`의 canonical decoder.
  - `x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json`, `privacy_prover_http_api_contract.json`, `privacy_relay_withdraw_contract.json`, `privacy_send_capable_reference_flow.json`, `privacy_browser_signer_provider_contract.json`의 versioned fixture.
- worktree 상태: Completion Record/Ledger commit 후 tracked worktree clean. `dist/` release pack과 `benchmarks/` readiness summary만 gitignored 검증 산출물이며 generated R1CS/PK/VK 또는 secret은 tracked되지 않음. Session 2 구현은 시작하지 않음.

미해결 Critical/High finding이 있으면 Session 2를 시작하지 않음.
