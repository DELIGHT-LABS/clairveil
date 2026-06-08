# Clairveil Privacy Accounting 설계 및 Hardening Note

이 문서는 `clairveil`을 최초 공개 또는 downstream 통합 전에 점검한 privacy accounting 설계 노트입니다. 2026-06 Zcash Orchard soundness 이슈는 검토 계기였지만, `clairveil`은 Orchard/halo2 코드를 사용하지 않습니다. 여기서 다루는 내용은 Clairveil 자체의 shielded note accounting, ZK circuit soundness, keeper reserve invariant를 공개 전 기준으로 정리한 것입니다.

이 문서는 live/public state가 없다는 전제를 둡니다. 이미 외부 사용자가 deposit, transfer, withdraw를 만들 수 있었던 deployment라면 이 문서만으로 충분하지 않고, deployment owner가 별도 운영 절차와 state review를 수행해야 합니다.

## 1. 설계 결론

| 항목 | 현재 설계 |
| --- | --- |
| Deposit accounting | `MsgDeposit.proof`와 `DepositCircuit`으로 locked transparent amount/asset을 note commitment에 binding합니다. |
| Amount domain | 모든 shielded amount는 non-negative 64-bit integer bound를 공유합니다. |
| Spend/withdraw | `SpendCircuit`이 note membership, owner signature, nullifier, amount/asset/recipient binding을 증명합니다. |
| Transfer | `JoinSplitCircuit`이 2-input/2-output amount conservation, nullifier, output commitment, disclosure digest를 증명합니다. |
| Signature/point hardening | gnark 표준 `eddsa.Verify`, point on-curve assertion, malformed point/scalar negative tests를 사용합니다. |
| Reserve accounting | keeper가 denom별 deposit/withdraw totals와 module-account balance를 비교하는 `reserve/{denom}` query를 제공합니다. |
| Artifact contract | active circuit set은 `privacy-accounting-v2`이며 deposit/spend/joinsplit artifacts를 모두 포함합니다. |

## 2. 공개 전 위험 모델

### 2.1 Deposit commitment와 입금 금액 불일치

예전 구조에서 가장 중요한 위험은 keeper가 `MsgDeposit.amount`와 `note_commitment` 내부 amount/asset의 일치를 알 수 없다는 점이었습니다. 공개 전 설계에서는 이 경계를 아래 방식으로 닫았습니다.

1. Client는 deposit note를 만들고 commitment를 계산합니다.
2. Client는 `DepositCircuit` proof를 생성합니다.
3. Keeper는 `MsgDeposit.amount`와 denom-derived asset id를 public input으로 deposit proof를 검증합니다.
4. Proof verification이 성공한 뒤에만 bank lock, reserve deposit 기록, Merkle append를 수행합니다.

따라서 "1uclair를 lock하면서 100uclair note commitment를 append"하는 경로는 keeper boundary에서 통과하지 못해야 합니다.

### 2.2 Amount field modulo wrap

`JoinSplitCircuit`의 amount conservation은 BN254 field equality만으로 해석되면 정수 보존과 다를 수 있습니다. 공개 전 설계에서는 모든 amount field에 같은 64-bit range constraint를 걸어 field modulo wrap을 차단합니다.

적용 대상:

- `DepositCircuit.Amount`
- `SpendCircuit.Amount`
- `JoinSplitCircuit.InputAmounts`
- `JoinSplitCircuit.OutputAmounts`
- keeper/SDK/payload amount validation

### 2.3 Signature와 curve point soundness

Spend/JoinSplit owner authorization은 gnark twisted Edwards EdDSA verifier를 사용합니다. 회로는 spend/view public key와 signature `R` point가 curve 위에 있는지 assert하고, signature scalar bound는 gnark `eddsa.Verify` 내부 constraint에 의존합니다.

공개 전 테스트는 malformed public key, malformed signature point, high scalar witness가 proof generation/verification을 통과하지 못하는지 확인합니다.

### 2.4 Merkle path helper

`PathHelper`는 circuit에서 `api.Select` selector로 쓰이며 gnark selector boolean constraint를 받습니다. 별도로 SDK와 JS schema도 helper 값을 `0` 또는 `1`로 제한합니다. 이 항목은 현재 exploit 후보라기보다 client contract hardening 항목입니다.

## 3. Reserve Accounting

Circuit soundness는 keeper-level reserve accounting과 함께 확인해야 합니다. Keeper는 denom별로 아래 값을 제공합니다.

```text
module_balance
total_deposited
total_withdrawn
expected_module_balance = total_deposited - total_withdrawn
invariant_holds = module_balance == expected_module_balance && expected_module_balance >= 0
```

Query path:

```text
GET /clairveil/privacy/v1/reserve/{denom}
```

현재 API에는 별도 reserve 조정 필드가 없습니다. Direct bank send, manual top-up, migration-time balance change가 deposit/withdraw accounting과 맞지 않으면 `invariant_holds=false`로 드러나야 합니다. 나중에 governance/admin/migration 조정이 필요해지면, 그 write path와 audit trail을 먼저 설계한 뒤 query field를 추가합니다.

## 4. Hardening 작업 단위

### Phase 0: Regression/negative tests

완료. 공개 repo에는 mitigated behavior 중심의 tests를 둡니다.

- forged deposit reject
- amount overflow/wrap reject
- invalid path helper reject
- malformed point/scalar reject
- reserve invariant mismatch signal
- keeper-level deposit proof tamper reject

### Phase 1: Amount model 상수화

완료. `x/privacy/types/amount.go`의 `ShieldedAmountBitLength`, `MaxShieldedAmount`, `ValidateShieldedAmount`를 circuit, keeper, SDK, payload validation에서 공유합니다.

### Phase 2: Deposit binding

완료. `DepositCircuit`, `MsgDeposit.proof`, keeper proof verification, CLI/SDK deposit proof builder, deposit artifact loader/setup이 추가되었습니다.

### Phase 3: Spend/JoinSplit range constraints

완료. Spend/JoinSplit amount fields는 같은 64-bit bound로 constrain됩니다. Zero-value dummy input/output은 non-negative bound 안에서 허용됩니다.

### Phase 4: Signature/point constraints hardening

완료. `SpendCircuit`와 `JoinSplitCircuit`은 gnark 표준 `eddsa.Verify`를 사용하고, public key/signature points에 on-curve assertion을 적용합니다. Malformed point/scalar negative tests와 proving benchmarks가 있습니다.

### Phase 5: Client/prover contract 갱신

완료. Transfer/withdraw prover payload contract는 version/hash 검증을 유지하며 새 circuit set id를 반영합니다. Deposit은 현재 별도 HTTP prover endpoint 없이 CLI/SDK가 Groth16 proof bytes를 생성해 `MsgDeposit.proof`에 넣는 계약입니다. Downstream JS/TS client가 remote deposit proving을 원하면 별도 deposit prover endpoint 또는 local/WASM prover adapter를 추가해야 합니다.

### Phase 6: Artifact rotation

완료. `privacy-accounting-v2` active circuit set id와 deposit/spend/joinsplit artifact descriptors가 추가되었습니다. Generated binary artifacts는 source repo에 커밋하지 않고 `clairveil-setup`에서 재생성합니다.

## 5. Client Contract Notes

Client와 downstream SDK는 아래 경계를 지켜야 합니다.

- `MsgDeposit`은 proof-less format을 지원하지 않습니다.
- Deposit builder는 note commitment, encrypted note, `DepositCircuit` proof를 함께 생성해야 합니다.
- Transfer/withdraw prepared payload는 version과 payload hash를 검증해야 합니다.
- Merkle path helper는 `0` 또는 `1`만 허용해야 합니다.
- `circuit_config`의 `active_set_id`와 artifact descriptors를 확인해야 합니다.
- `reserve/{denom}` query 결과는 deposit/withdraw flow 이후 `invariant_holds=true`여야 합니다.

## 6. 공개 전 Validation Checklist

최소 검증:

```bash
go test ./x/privacy/circuit ./x/privacy/keeper ./x/privacy/types
go test ./x/privacy/client/sdk/...
make examples
make build
```

Release candidate 검증:

```bash
make ci
make privacy-e2e-smoke
make release-pack-verify
```

Artifact 검증:

1. `clairveil-setup`으로 deposit/spend/joinsplit R1CS/PK/VK를 새로 생성합니다.
2. `privacy_zk_manifest.json`의 `active_set_id`가 `privacy-accounting-v2`인지 확인합니다.
3. checksum env를 strict preflight와 함께 사용합니다.
4. downstream handoff 문서와 fixture schema가 새 contract를 반영하는지 확인합니다.

## 7. 외부 상태가 이미 있는 경우

이 섹션은 공개 전 기본 경로가 아니라 contingency note입니다.

외부 사용자가 이미 privacy tx를 만들 수 있었거나 production-like state가 존재한다면, deployment owner는 code patch 완료 여부와 별개로 state exposure를 먼저 평가해야 합니다. Audit disclosure는 transfer output의 recipient amount/from/to 검증에는 유용하지만, 초기 deposit commitment가 실제 locked amount와 일치했는지를 소급해서 암호학적으로 완전히 증명하지는 못합니다.

평가에는 최소 아래 자료가 필요합니다.

- module account balance
- deposit/withdraw totals
- deposit records
- known commitments and nullifiers
- wallet note opening
- transfer audit disclosures
- full lineage reconstruction

그 상태에서의 migration, claim, reset 정책은 chain deployment owner가 별도 운영 문서로 결정해야 합니다.

## 8. 관련 문서

- `docs/clairveil-circuits-kr.md`
- `docs/clairveil-threat-model-kr.md`
- `docs/clairveil-security-best-practices-review-kr.md`
- `docs/clairveil-release-handoff-pack-kr.md`
- `docs/clairveil-downstream-cosmos-integration-guide-kr.md`
