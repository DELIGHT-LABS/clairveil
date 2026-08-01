# Clairveil `clairveil-proverd` Deposit API 구현 계획

> 상태: **Completed record**
>
> 작업 branch: `feature/enhancement-proverd`
>
> 작성일: 2026-08-01
>
> 완료일: 2026-08-02
>
> 기준 commit: `029d3d2f9f9f144f68e9fe573f3b437e05d7ba70`
>
> 실행 순서: **구현 → 테스트 → 문서화 → 최종검증**
>
> 완료 목표: 이 repository가 소유하는 language-neutral canonical deposit prover HTTP contract를 정의하고 `clairveil-proverd`가 그 계약을 직접 제공한다.

이 문서는 다른 작업 세션이 추가 설계 탐색 없이 바로 구현할 수 있도록 canonical wire contract, 파일별 변경 범위, 테스트 matrix, 문서 갱신 범위와 exit gate를 고정한다. 구현 중 이 문서와 현재 코드가 충돌하면 consensus/circuit/keeper의 실행 가능한 계약을 우선하고, 이 문서의 상태와 결정 기록을 같은 변경에서 갱신한다.

## 1. 책임 경계와 목표

### 1.1 이 repository의 책임

이번 작업에서 `clairveil` repository는 다음을 소유한다.

- `POST /v1/prover/deposit` canonical HTTP contract
- request, payload, response, proof versioning
- canonical point/field/amount/proof encoding과 validation
- `clairveil-proverd` route, artifact provider, admission, readiness, auth와 error contract
- language-neutral JSON conformance fixture와 Go contract test
- downstream client가 구현할 수 있는 bilingual API/handoff 문서

특정 JS/TS package의 현재 내부 API, provider 구조, release version 또는 migration은 이 repository의 source of truth가 아니다. 현재 downstream 구현과 무변경 호환을 별도 요구하지 않는 한 canonical core contract를 downstream 구현에 맞춰 약화하거나 unversioned schema를 수용하지 않는다.

### 1.2 완료 상태

완료 시 다음 조건이 모두 성립해야 한다.

- `clairveil-proverd`가 `POST /v1/prover/deposit`을 제공한다.
- Deposit request/response는 outer envelope와 nested payload/proof version을 모두 가진다.
- Request는 회로가 실제 사용하는 witness만 포함하고 Go `types.Note` JSON 표현에 의존하지 않는다.
- Server는 request witness로 commitment를 재계산하고 mismatch를 proving 전에 거부한다.
- Server는 기존 `DepositCircuit` R1CS/PK를 사용해 proof를 만들고 canonical BN254 proof framing을 검증한다.
- `/healthz`, `/readyz`, `/debug/vars`가 deposit route/circuit/admission을 반영한다.
- Bearer auth, request limit, gzip limit, admission과 secret-free error 경계가 다른 bounded proof route와 동일하게 적용된다.
- Deposit/transfer/withdraw/batch proof route가 하나의 common HTTP policy를 사용한다. `Content-Type`, 405 `Allow`, `Cache-Control`, request/prover failure status가 route마다 갈라지지 않는다.
- Language-neutral fixture와 Go conformance test가 wire contract를 동결한다.
- 잘못된 “deposit HTTP endpoint가 없다”는 현재 문서가 수정된다.
- `examples/clairveil-dapp/**`는 변경하지 않는다.

## 2. 비수정 범위와 중단 조건

### 2.1 변경하지 않는 계약

다음은 이번 작업의 비수정 범위다.

- `x/privacy/circuit/deposit.go`의 constraint와 public input
- Deposit VK/R1CS/PK 형식, checksum과 artifact manifest descriptor
- `MsgDeposit` protobuf와 chain transaction wire format
- keeper deposit proof verification, reserve accounting, tree/index/state transition
- genesis, store schema와 migration
- CLI deposit의 local proving 동작
- 기존 transfer/withdraw/batch의 path, success request/response schema와 version
- 공통 `ErrorResponse` JSON의 `version`, code set, `retryable` 의미
- 별도 deposit 전용 Go binary
- `cmd/clairveil-proverload`와 public capacity benchmark 구현
- 특정 downstream SDK용 adapter, compatibility shim 또는 release migration
- `examples/clairveil-dapp/**` 전체

### 2.2 즉시 중단하고 범위를 재검토할 조건

다음 상황이 발생하면 임의로 범위를 넓히지 않고 작업을 중단해 별도 결정으로 올린다.

- endpoint 구현에 Deposit circuit constraint 변경이 필요해 보이는 경우
- public input order 또는 keeper verification input을 바꿔야 하는 경우
- VK/R1CS/PK 재생성이나 manifest checksum 변경이 필요한 경우
- `MsgDeposit` field를 추가하거나 변경해야 하는 경우
- canonical request에 creator, denom, encrypted note, memo 또는 chain ID를 넣어야 한다고 판단되는 경우
- 아래에서 승인한 common HTTP policy 정규화 외에 기존 transfer/withdraw/batch path, success request/response schema/version 또는 error body/code를 바꿔야 하는 경우
- unversioned legacy request를 canonical route에서 함께 받아야 하는 요구가 생기는 경우

이 중 어느 것도 현재 구현에는 필요하지 않아야 한다. 기존 `x/privacy/client/sdk/deposit/prove.go`의 deposit proof builder, `x/privacy/circuit/deposit.go`의 Deposit circuit, `x/privacy/zk/registry.go`의 artifact registry, `x/privacy/keeper/deposit.go`의 keeper verifier를 그대로 재사용하는 것이 기본 전제다.

## 3. 현재 상태

| 영역 | 현재 상태 | 이번 작업 |
| --- | --- | --- |
| Deposit circuit | `DepositCircuit` 구현 및 proof test 존재 | 변경 없음 |
| Artifact contract | `CircuitDeposit`, R1CS/PK/VK descriptor 존재 | service wiring/readiness에 사용 |
| Native SDK proving | `deposit.BuildDepositProof` 존재 | prepared payload에서 호출 |
| Chain verification | keeper가 commitment/amount/asset ID로 VK 검증 | 변경 없음 |
| Prover transport | transfer/withdraw/batch route만 존재하고 Content-Type/header/status 처리가 일부 불일치 | common HTTP policy 정규화 후 deposit contract/route/client 추가 |
| Prover service | deposit artifact/admission/readiness 누락 | deposit을 full service surface에 추가 |
| Conformance | deposit HTTP contract fixture 없음 | language-neutral fixture 추가 |
| 문서 | 일부 문서가 endpoint 부재를 현재 사실로 기술 | canonical API 기준으로 수정 |

## 4. 고정 설계 결정

| ID | 결정 | 근거 |
| --- | --- | --- |
| DEC-01 | Route는 `POST /v1/prover/deposit`이다. | 기존 deposit/transfer/withdraw namespace와 일관되고 public benchmark plan에도 같은 path가 제안되어 있다. |
| DEC-02 | Outer request/response와 nested payload/proof를 각각 versioning한다. | Durable payload/proof version과 HTTP envelope version은 독립적인 compatibility layer다. |
| DEC-03 | Request는 structured canonical witness field를 사용하고 `note_json`을 사용하지 않는다. | Go JSON representation, double encoding, 언어별 큰 정수 처리에 의존하지 않는다. |
| DEC-04 | Public key는 32-byte canonical compressed BN254 twisted Edwards point hex다. | 기존 `privacycrypto.DecodeCanonicalPoint` contract를 재사용한다. |
| DEC-05 | Amount는 canonical uint64 decimal string이며 zero를 허용한다. | 기존 zero-value deposit 의미와 shielded amount bound를 보존한다. |
| DEC-06 | Asset ID와 randomness는 정확한 32-byte canonical field hex다. | Circuit witness와 기존 field boundary를 그대로 표현한다. |
| DEC-07 | Response binding key는 note commitment다. v1에는 별도 `payload_hash`를 넣지 않는다. | Commitment가 두 public key, amount, asset ID, randomness를 이미 cryptographically binding한다. |
| DEC-08 | Memo, creator, denom, encrypted note, seed와 chain ID는 request에 넣지 않는다. | Circuit이 소비하지 않는 정보이며 remote prover disclosure를 최소화해야 한다. |
| DEC-09 | Response proof는 정확히 164-byte canonical BN254 Groth16 encoding이다. | 기존 `privacyzk.ValidateCanonicalProofBN254`를 재사용한다. |
| DEC-10 | Canonical route는 version auto-detection, unknown field, legacy fallback을 허용하지 않는다. | Ambiguous decode와 silent downgrade를 막는다. |
| DEC-11 | Bearer/auth/admission/body-limit뿐 아니라 route-independent HTTP policy를 모든 proof route에 공통 적용한다. | Deposit witness도 다른 proof payload와 동일하게 privacy-sensitive하며 route별 transport 차이를 만들 이유가 없다. |
| DEC-12 | CORS는 canonical proof protocol이 아니라 deployment gateway 책임으로 둔다. | Raw daemon에 permissive browser policy를 추가하지 않는다. |
| DEC-13 | 기존 public Go constructor signature는 유지한다. | Deposit 추가가 현재 downstream Go caller를 source-break하지 않아야 한다. |
| DEC-14 | Prover는 생성 직후 proof framing을 검증하지만 VK verification을 중복 수행하지 않는다. | Ultimate verification은 chain keeper 책임이며 server-side 재검증은 proving 비용을 중복한다. |
| DEC-15 | Common policy 정규화는 deposit 구현과 같은 branch/PR/release에 포함하되 선행 별도 commit으로 분리한다. | Deposit을 먼저 다른 정책으로 출시하거나 기존의 잘못된 분류를 복사한 뒤 다시 바꾸는 downstream churn을 피한다. |
| DEC-16 | 모든 proof route는 missing/unsupported JSON media type을 415로, request decode/version/semantic 실패를 400으로, 실제 prover invocation 이후 실패를 500으로 반환한다. | 4xx는 caller가 수정할 수 있는 입력 오류, 5xx는 validated request 이후 server/prover 실패라는 HTTP 의미를 고정한다. |
| DEC-17 | Existing transfer/withdraw/batch의 `400 proof_failed`를 `500 proof_failed`로 교정한다. | 현재 동작은 외부 관찰 가능하므로 단순 internal refactor가 아니라 compatibility-impacting contract correction으로 기록한다. |
| DEC-18 | Common 정책 변경으로 success envelope version과 `ErrorResponseVersion=v1`은 올리지 않는다. | Error JSON shape/code/retryability는 그대로이고 HTTP status/header enforcement만 교정한다. General prover fixture schema를 `v3`로 올리고 release note에 migration impact를 기록한다. |

`payload_hash`가 나중에 필요해지면 canonical serialization과 hash domain을 먼저 정의하고 payload/proof version을 올린다. v1에 optional field로 소급 추가하지 않는다.

## 5. Canonical HTTP contract

### 5.1 Request

```http
POST /v1/prover/deposit
Content-Type: application/json
Accept: application/json
```

```json
{
  "version": "v1",
  "payload": {
    "version": "v1",
    "receiver_spend_pubkey_hex": "32-byte-lowercase-hex",
    "receiver_view_pubkey_hex": "32-byte-lowercase-hex",
    "amount": "10",
    "asset_id_hex": "32-byte-lowercase-hex",
    "randomness_hex": "32-byte-lowercase-hex",
    "note_commitment_hex": "32-byte-lowercase-hex"
  }
}
```

Go contract shape:

```go
const (
    DepositProofRequestVersion          = "v1"
    PreparedDepositProverPayloadVersion = "v1"
)

type PreparedDepositProverPayload struct {
    Version                string `json:"version"`
    ReceiverSpendPubKeyHex string `json:"receiver_spend_pubkey_hex"`
    ReceiverViewPubKeyHex  string `json:"receiver_view_pubkey_hex"`
    Amount                 string `json:"amount"`
    AssetIDHex             string `json:"asset_id_hex"`
    RandomnessHex          string `json:"randomness_hex"`
    NoteCommitmentHex      string `json:"note_commitment_hex"`
}

type DepositProofRequest struct {
    Version string                       `json:"version"`
    Payload PreparedDepositProverPayload `json:"payload"`
}
```

### 5.2 Request field contract

| Field | Contract |
| --- | --- |
| outer `version` | 정확히 `v1` |
| payload `version` | 정확히 `v1` |
| `receiver_spend_pubkey_hex` | gnark-crypto BN254 twisted-Edwards `PointAffine.Bytes()`와 같은 정확히 32-byte compressed wire encoding의 64 lowercase hex; on-curve, non-identity, prime subgroup |
| `receiver_view_pubkey_hex` | spend key와 같은 canonical point 규칙 |
| `amount` | `0` 또는 leading zero 없는 canonical uint64 decimal string |
| `asset_id_hex` | 정확히 32-byte unsigned big-endian canonical BN254 scalar-field encoding의 64 lowercase hex |
| `randomness_hex` | 정확히 32-byte unsigned big-endian canonical BN254 scalar-field encoding의 64 lowercase hex |
| `note_commitment_hex` | 정확히 32-byte unsigned big-endian canonical non-zero BN254 field의 64 lowercase hex |

모든 hex는 `0x` prefix 없이 고정 폭으로 encode한다. Server는 두 compressed point를 X/Y coordinate로 복원하고 memo가 빈 `types.Note`를 구성한다. `Note.ValidateV1()`과 `ComputeCommitment()`를 실행하고 재계산 commitment가 request와 정확히 같아야 한다. 따라서 reconstructed note의 commitment뿐 아니라 nullifier도 non-zero여야 한다. Asset ID와 randomness 자체의 zero는 허용하되 이 최종 NoteV1 invariant를 통과해야 한다.

Amount/asset ID는 proof public input이지만 server는 denom registry나 chain RPC를 조회하지 않는다. Downstream client가 같은 amount와 asset ID에 대응하는 denom으로 `MsgDeposit`을 만들고, keeper가 message amount 및 registered denom-derived asset ID로 최종 proof를 검증한다.

### 5.3 Response

```json
{
  "version": "v1",
  "proof": {
    "version": "v1",
    "note_commitment_hex": "32-byte-lowercase-hex",
    "proof_hex": "164-byte-lowercase-hex"
  }
}
```

Go contract shape:

```go
const (
    DepositProofResponseVersion = "v1"
    PreparedDepositProofVersion = "v1"
)

type PreparedDepositProof struct {
    Version           string `json:"version"`
    NoteCommitmentHex string `json:"note_commitment_hex"`
    ProofHex          string `json:"proof_hex"`
}

type DepositProofResponse struct {
    Version string               `json:"version"`
    Proof   PreparedDepositProof `json:"proof"`
}
```

Response validation:

- outer response와 nested proof version이 모두 `v1`이어야 한다.
- response commitment는 request payload commitment와 같아야 한다.
- response commitment는 server가 witness에서 재계산한 canonical lowercase value여야 한다.
- proof hex는 정확히 328 lowercase hex character여야 한다.
- decoded proof는 `privacyzk.ValidateCanonicalProofBN254`를 통과해야 한다.
- response에는 위 field 외의 unknown field가 없어야 한다.

### 5.4 Versioning 규칙

- Endpoint path의 `/v1`은 HTTP route major version이다.
- Request envelope, prover payload, response envelope, prepared proof version은 각각 독립적으로 검증한다.
- Field 추가/삭제/rename, encoding 변경, validation 의미 변경은 해당 object의 version bump 대상이다.
- Strict decoder를 사용하므로 기존 `v1`에 optional field를 조용히 추가하지 않는다.
- Unsupported version은 `400 invalid_request`로 fail closed한다.
- Circuit witness/public-input 변경은 payload/proof version뿐 아니라 artifact identity 영향도 별도로 검토한다.
- Legacy request auto-detection과 “먼저 새 schema, 실패하면 옛 schema” fallback은 금지한다.

### 5.5 Common proof-route HTTP policy

이 subsection은 신규 deposit뿐 아니라 기존 transfer, withdraw, batch-transfer proof route에도 동일하게 적용한다.

- Request `Content-Type` header는 필수다. Body read와 admission 획득 전에 검사한다.
- `mime.ParseMediaType` 기준 media type이 정확히 `application/json`이어야 하고, parameter는 없거나 `charset=utf-8` 하나만 허용한다. Media type/parameter 비교는 HTTP 규칙에 맞게 case-insensitive하게 처리하되 다른 parameter와 charset은 거부한다.
- `Accept`는 필수가 아니며 v1에서 content negotiation을 하지 않는다. Server는 항상 JSON을 반환한다.
- `Content-Encoding`은 기존 bounded service가 지원하는 `identity` 또는 `gzip`만 허용한다.
- Raw wire body와 decompressed body 모두 configured positive limit를 적용한다.
- Default request limit는 현재 service default인 8 MiB를 유지한다.
- Success와 error response는 `Content-Type: application/json`을 사용한다.
- 모든 proof route의 success와 error response에 `Cache-Control: no-store`를 설정한다. Auth/content-encoding 단계에서 끝나는 response도 예외가 아니다.
- 모든 proof route의 method mismatch 405 response는 `Allow: POST`를 설정한다.
- Unknown route는 기존 common `404 not_found` contract를 유지한다.

### 5.6 Error contract

모든 proof route의 공통 body:

```json
{
  "version": "v1",
  "code": "invalid_request",
  "message": "proof request validation failed"
}
```

`message`는 route와 failure class에 맞는 고정된 secret-free 문구를 사용하며 witness 값을 포함하지 않는다. `retryable`은 optional boolean이다. 생략은 `false`를 뜻하며 `busy` response에서만 반드시 `true`로 존재한다. 다른 error code에서 `retryable=true`는 invalid error response다. Error decoder도 duplicate key, unknown field와 trailing JSON을 거부한다.

| HTTP | code | retryable | 조건 |
| ---: | --- | --- | --- |
| 400 | `invalid_request` | false | JSON/version/payload semantic validation 실패 |
| 400 | `invalid_request` | false | invalid/unsupported `Content-Encoding` |
| 401 | `unauthorized` | false | configured bearer token 불일치 |
| 404 | `not_found` | false | unknown path |
| 405 | `method_not_allowed` | false | POST가 아닌 method |
| 413 | `invalid_request` | false | raw 또는 decompressed body limit 초과 |
| 415 | `invalid_request` | false | missing/unsupported `Content-Type` |
| 429 | `busy` | true | circuit admission queue 포화 |
| 500 | `proof_failed` | false | proof runner 또는 response self-validation 실패 |
| 503 | `unavailable` | false | 해당 route의 prover가 미구성 |

Request strict decode/version/semantic validation까지 통과한 뒤 `Prove*` invocation이 error를 반환하거나 nil/invalid response를 반환하면 route에 관계없이 `500 proof_failed`다. Runtime artifact load와 proof runner 실패도 여기 포함한다. Artifact 부재는 `/readyz`에서 사전에 fail closed하며, runtime 503 구분을 위해 새 typed unavailable error hierarchy를 이번 범위에 추가하지 않는다.

Unsupported `Content-Encoding`은 현재 bounded service의 common `400 invalid_request` contract를 유지한다. Error JSON shape와 code set은 바꾸지 않지만 기존 transfer/withdraw/batch의 `400 proof_failed` status가 500으로 바뀌므로 changelog와 handoff에서 compatibility impact를 명시한다. `proof_failed`는 계속 `retryable=false`이며 status만 보고 automatic retry하지 않는다.

Error message, log, metric label에는 다음을 넣지 않는다.

- request/response body
- amount, asset ID, randomness
- public key 또는 note commitment
- proof bytes
- bearer token
- gnark witness/solver diagnostic

### 5.7 Auth, timeout과 privacy boundary

- `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`이 설정되면 deposit route에도 같은 bearer auth를 적용한다.
- Credential을 URL query/userinfo에 넣지 않는다.
- Non-loopback remote endpoint는 TLS를 사용한다.
- Client는 finite timeout을 설정해야 한다.
- Context cancellation은 caller wait cancellation이며 in-process gnark solver termination을 보장하지 않는다.
- Safe default retry는 같은 endpoint에만 허용한다.
- Automatic multi-prover failover는 witness disclosure boundary를 넓히므로 기본 금지한다.
- Browser cross-origin 사용은 allowlist CORS와 auth policy를 가진 downstream gateway가 담당한다.
- Remote prover는 receiver key, amount, asset ID, randomness를 받고 commitment와 nullifier를 파생할 수 있다. 따라서 이 endpoint 선택 자체를 trusted-prover privacy decision으로 문서화하고, 일반 public RPC와 같은 trust boundary로 취급하지 않는다.

### 5.8 Downstream assembly boundary

이 endpoint는 `MsgDeposit`이나 encrypted note를 만들거나 transaction을 broadcast하지 않는다. 언어와 SDK에 무관한 caller flow는 다음으로 고정한다.

1. Downstream client가 receiver keys, amount, denom-derived asset ID, randomness와 선택한 memo로 NoteV1을 구성한다.
2. 같은 circuit-relevant field로 commitment를 계산하고 memo를 포함한 note plaintext를 canonical deposit envelope로 암호화한다.
3. Memo를 제외한 5.1 payload로 deposit proof를 요청한다.
4. Response version, commitment equality와 proof framing을 검증한다.
5. `proof_hex`와 `note_commitment_hex`를 bytes로 바꾸고, 동일한 amount/denom 및 encrypted note와 함께 `MsgDeposit`을 만든다.
6. Transaction을 sign/broadcast한다. Keeper는 denom에서 asset ID를 다시 구하고 amount/commitment와 함께 proof를 최종 검증한다.

Proof는 memo, encrypted note, creator나 denom string 자체를 bind하지 않는다. 대신 amount, denom-derived asset ID와 commitment를 public input으로 bind하고, commitment가 receiver keys/amount/asset ID/randomness를 bind한다. Downstream client는 이 경계를 흐리거나 prover response만으로 encrypted note를 재구성했다고 가정하면 안 된다.

## 6. Phase 1 — 구현

Phase 1에서는 production code만 추가한다. 신규 기능 test와 fixture는 Phase 2에서 추가한다. 기존 test가 compile을 막는 경우 constructor compatibility 등 최소 수정만 먼저 수행하고 실제 assertion 확대는 Phase 2에 기록한다.

### I-00. Common proof-route HTTP policy 정규화

대상:

- `x/privacy/client/sdk/provertransport/http.go`
- `x/privacy/client/sdk/provertransport/contract.go`
- `x/privacy/client/sdk/proverservice/service.go`

작업:

1. Route별 handler에 복사하지 않도록 다음 common helper 경계를 만든다.
   - JSON media type parse/validation
   - proof-route response header 설정
   - method mismatch와 `Allow: POST`
   - request failure와 post-validation prover failure response 작성
2. 기존 transfer/withdraw/batch와 신규 deposit에 같은 helper를 적용한다.
   - Raw proof handler의 정상 순서는 method → media type → prover availability → body/framing → admission → semantic validation → prove → response validation이다.
   - Bounded service의 bearer/content-encoding 조기 거부가 먼저 발생할 수 있으므로 여러 invalid condition을 동시에 보낸 request의 precedence는 contract로 고정하지 않는다.
3. Proof route 진입 즉시 `Cache-Control: no-store`를 설정한다.
   - Raw `provertransport.HTTPHandler` 직접 사용
   - Bounded `proverservice.Handler`의 auth/content-encoding 조기 종료
   - 두 경로 모두 빠짐없이 적용한다.
4. Status 분류를 다음으로 통일한다.
   - strict JSON/version/semantic validation에서 발견한 caller-correctable request 실패: `400 invalid_request`
   - circuit admission 포화: `429 busy`, `retryable=true`
   - route prover 미구성: `503 unavailable`
   - validated request로 `Prove*`를 호출한 뒤 runner error, nil response 또는 response self-validation 실패: `500 proof_failed`
5. `ErrorResponseVersion`, error code set과 `retryable` 의미는 유지한다.
6. 기존 transfer/withdraw/batch success request/response struct, path와 version은 변경하지 않는다.
7. Handler 전체를 generic reflection/generics framework로 다시 쓰지 않는다. 이번 refactor는 공통 transport policy helper와 status 교정까지만 수행한다.

완료 조건:

- 네 proof route가 같은 media type/header/status policy를 사용한다.
- Existing success wire contract에는 diff가 없다.
- 기존 `400 proof_failed` 동작만 명시적으로 500으로 교정된다.

### I-01. Deposit prepared payload/proof layer

대상:

- 신규 `x/privacy/client/sdk/deposit/payload.go`
- 기존 `x/privacy/client/sdk/deposit/prove.go`

작업:

1. `PreparedDepositProverPayload`와 `PreparedDepositProof`를 추가한다.
2. `BuildPreparedDepositProverPayload(note types.Note)`를 추가한다.
   - `Note.ValidateV1()`을 먼저 실행한다.
   - Spend/view point를 canonical compressed hex로 encode한다.
   - Amount를 canonical decimal string으로 encode한다.
   - Asset ID, randomness, commitment를 exact 32-byte lowercase hex로 encode한다.
   - Memo를 payload에 포함하지 않는다.
3. `ValidatePreparedDepositProverPayload`를 추가한다.
   - exact version 및 lowercase/exact-width contract를 적용한다.
   - Field/commitment는 `^[0-9a-f]{64}$`, proof는 `^[0-9a-f]{328}$`를 먼저 검사한다. `field.DecodeCanonicalHex`가 short input을 left-pad할 수 있으므로 exact-width check를 생략하지 않는다.
   - Public key도 lowercase/exact 64 hex를 먼저 검사한 뒤 `privacycrypto.DecodeCanonicalPoint`를 호출한다.
   - point, amount, field를 decode한다.
   - 빈 memo의 Note를 재구성하고 `ValidateV1()`을 실행한다.
   - commitment를 재계산해 equality를 확인한다.
4. `BuildPreparedDepositProof`를 추가한다.
   - validated payload에서 Note를 복원한다.
   - 기존 `BuildDepositProof`를 호출한다.
   - 생성 proof를 canonical framing 검증한다.
   - recomputed commitment로 `PreparedDepositProof`를 만든다.
5. `ValidatePreparedDepositProof`를 추가한다.
   - payload를 다시 검증한다.
   - version, commitment binding, proof exact hex/canonical framing을 검증한다.
6. 기존 `BuildDepositAssignment`와 circuit assignment는 변경하지 않는다.

완료 조건:

- Go `types.Note` JSON을 사용하지 않고 payload에서 기존 Deposit proof builder까지 도달한다.
- Payload가 circuit witness보다 더 많은 정보를 포함하지 않는다.

### I-02. HTTP request/response contract

대상:

- `x/privacy/client/sdk/provertransport/contract.go`

작업:

1. Deposit request/response version constant와 struct를 추가한다.
2. `NewDepositProofRequest`를 추가해 validated payload만 envelope에 넣는다.
3. `ValidateDepositProofRequest`, `BuildDepositProofResponse`, `ValidateDepositProofResponse`를 추가한다.
4. `DecodeDepositProofRequestJSON`과 `DecodeDepositProofResponseJSON`을 기존 `decodeStrictJSON`으로 구현한다.
5. Common `DecodeErrorResponseJSON`도 `decodeStrictJSON`을 사용하게 하고 기존 error contract test를 유지한다.
6. Marshal helper는 HTTP/fixture에 필요한 범위만 추가한다.
7. 별도 resume/file CLI flow가 없으므로 v1 작업에서 deposit request/proof file command는 추가하지 않는다.

완료 조건:

- Duplicate key, unknown field, trailing JSON이 outer와 nested object 모두에서 fail closed한다.
- 기존 transfer/withdraw/batch success request/response contract를 변경하지 않는다.
- Common error decoder hardening과 I-00에서 승인한 status/header correction만 기존 route에 적용한다.

### I-03. Raw HTTP handler와 Go client

대상:

- `x/privacy/client/sdk/provertransport/http.go`

작업:

1. 다음 constant를 추가한다.

   ```go
   DepositProofPath      = "/v1/prover/deposit"
   DepositProofCircuitID = "deposit"
   ```

2. `DepositProver`, `ReferenceDepositProver`와 `HTTPHandler.DepositProver`를 추가한다.
   - Deposit에는 expiry/clock semantic이 없으므로 interface는 `ProveDeposit(request DepositProofRequest) (*DepositProofResponse, error)`로 고정하고 불필요한 `time.Time` parameter를 넣지 않는다.
3. `ServeHTTP` dispatch에 deposit route를 추가한다.
4. `serveDepositProof`를 구현한다.
   - I-00의 common POST/media type/header policy 재사용
   - bounded body/framing 검사
   - deposit admission 획득
   - strict decode와 semantic validation
   - `StartProve` 직후 prover 호출
   - 실제 prover return 뒤 permit release
   - response self-validation과 `no-store` JSON response
5. `HTTPProverClient.ProveDeposit`을 추가한다.
   - request를 보내기 전에 local validation
   - redirect 금지, URL/TLS, bearer, response size의 기존 client boundary 재사용
   - response strict decode와 request binding validation
6. Constructor explosion을 피하기 위해 다음 aggregate와 canonical constructor를 추가한다.

   ```go
   type ProverSet struct {
       Deposit       DepositProver
       Transfer      TransferProver
       Withdraw      WithdrawProver
       BatchTransfer BatchTransferProver
   }

   func NewHTTPHandlerWithProverSet(
       provers ProverSet,
       now func() time.Time,
       admission ProofAdmission,
   ) *HTTPHandler
   ```

   기존 `NewHTTPHandler*` 함수는 source-compatible wrapper로 유지한다.

완료 조건:

- Raw handler 수준에서 deposit route가 transfer/withdraw와 같은 body/admission/permit 안전성을 가진다.
- 기존 public constructor call site가 컴파일된다.

### I-04. Bounded `clairveil-proverd` service wiring

대상:

- `x/privacy/client/sdk/proverservice/service.go`
- `x/privacy/client/sdk/proverservice/admission.go`

작업:

1. `referenceDepositArtifactProvider`를 추가한다.
   - `registry.R1CS(privacyzk.CircuitDeposit)`
   - `registry.ProvingKey(privacyzk.CircuitDeposit)`
2. `referenceDepositProofRunner`를 추가하고 기존 process-wide gnark logger suppression을 재사용한다.
3. `NewReferenceHandler`에 `ReferenceDepositProver`를 연결한다.
4. `DefaultRuntimeInfo.Routes`에 deposit path를 추가한다.
5. `DefaultRuntimeInfo.Circuits`에 `deposit`을 canonical required order로 추가한다.
6. Reference handler readiness의 hand-maintained subset을 `privacyzk.RequiredCircuitIDs()`로 교체한다. `RunProverPreflight`가 이미 이 helper를 사용한다면 변경하지 않고 test로 두 경로의 일치를 고정한다.
7. `isProofRoute`에 deposit을 추가해 bearer, raw/decompressed limit가 적용되게 한다.
8. `DefaultAdmissionConfig`에 독립 deposit gate를 추가한다.
   - `max_in_flight=1`
   - `max_queued=4`
9. `/debug/vars`의 generic admission snapshot에 deposit이 자동 노출되는지 확인한다.
10. `NewHandlerWithProverSet(provers provertransport.ProverSet, ...)`를 canonical service constructor로 추가한다. 기존 `NewHandler*` API는 source-compatible하게 유지하고 이 constructor로 위임한다.
11. I-00의 common no-store helper가 deposit을 포함한 모든 proof route의 auth, encoding, admission과 proof handler response에 적용되는지 확인한다.

완료 조건:

- Reference daemon이 기존 Deposit R1CS/PK를 lazy load해 proof를 만든다.
- Deposit artifact가 없거나 invalid하면 readiness가 fail closed한다.
- Validator VK readiness와 prover R1CS/PK readiness의 기존 역할 구분이 유지된다.

### I-05. Entrypoint와 packaging 확인

대상:

- `cmd/clairveil-proverd/main.go`
- `build/clairveil-proverd/**`

예상 결정:

- `main.go`는 `RunPreflight`와 `NewReferenceHandler`를 이미 사용하므로 직접 변경하지 않는다.
- Docker/compose/env는 새 flag나 env가 없으므로 원칙적으로 변경하지 않는다.
- Runtime route/readiness가 service wiring을 통해 자동 반영되는지만 확인한다.

Phase 1 gate:

- Production code가 compile한다.
- Existing transfer/withdraw/batch와 신규 deposit이 같은 common HTTP policy helper를 사용한다.
- `git diff --name-only`에 circuit, proto, artifact 또는 DApp 파일이 없다.
- Canonical payload가 `deposit.BuildDepositProof`까지 연결된다.

## 7. Phase 2 — 테스트와 conformance fixture

### T-00. Common HTTP policy regression test

대상:

- `x/privacy/client/sdk/provertransport/http_test.go`
- `x/privacy/client/sdk/proverservice/service_test.go`

Deposit/transfer/withdraw/batch를 같은 table에 넣고 다음을 고정한다.

- Valid JSON `Content-Type`과 `application/json; charset=utf-8` 허용
- Missing/unsupported media type은 body read/admission/prover call 전에 415
- POST가 아닌 method는 405와 `Allow: POST`
- Success, request error, auth error, encoding error, busy, unavailable, proof failure 모두 `Cache-Control: no-store`
- Malformed/unsupported version/semantic invalid request는 `400 invalid_request`
- Admission 포화는 `429 busy`, `retryable=true`
- Nil route prover는 `503 unavailable`
- Prover invocation error, nil response와 invalid response는 모두 `500 proof_failed`
- `proof_failed` body는 `retryable`을 생략하고 client는 이를 false로 해석
- `HTTPProverClient`가 모든 route request에 `Content-Type: application/json`을 설정
- 기존 redaction canary가 status 교정 후에도 response/log에 포함되지 않음

기존 transfer/withdraw/batch의 400 assertion을 단순 치환하는 데 그치지 않고, 400과 500의 경계가 handler call 순서와 일치하는지 검증한다. Media type 자체를 검증하는 negative case 외에는 기존 valid/invalid body test helper가 모두 `Content-Type: application/json`을 설정하게 고쳐, 원래 검증하려던 body-limit, framing, semantic, admission assertion이 415에 가려지지 않게 한다.

### T-01. Deposit payload/proof unit test

대상:

- 신규 `x/privacy/client/sdk/deposit/payload_test.go`

Positive:

- Note → prepared payload → reconstructed Note의 circuit-relevant field equality
- zero amount preservation
- exact big-endian field encoding과 compressed point round-trip

Negative table:

- wrong payload version
- uppercase, `0x` prefix, odd-length 또는 wrong-width hex
- malformed/off-curve/identity/non-subgroup point
- leading-zero/non-decimal/out-of-range amount
- noncanonical asset ID/randomness
- zero/noncanonical commitment
- amount, key, asset ID 또는 randomness mutation 후 commitment mismatch
- wrong proof version
- response commitment mismatch
- malformed/noncanonical/wrong-size proof

`BuildPreparedDepositProof`의 실제 positive path와 canonical prepared proof validation은 T-05의 단 한 번인 real proving flow에서 검증한다. Unit test를 위해 두 번째 setup/prove를 만들지 않는다.

### T-02. Transport contract test

대상:

- `x/privacy/client/sdk/provertransport/contract_test.go`

검증:

- request/response strict JSON round-trip
- unknown field
- duplicate key
- trailing second JSON value
- missing payload/proof
- unsupported outer/nested version
- response binding validation
- sensitive canary가 error string에 포함되지 않음

기존 transfer/withdraw/batch test 이름이나 table이 proof contract 전체를 의미한다면 deposit case를 추가하고 이름을 현재 범위와 맞춘다.

### T-03. HTTP handler/client test

대상:

- `x/privacy/client/sdk/provertransport/http_test.go`

최소 matrix:

- handler POST success와 exact response schema/header
- `HTTPProverClient.ProveDeposit` round-trip
- Deposit이 T-00의 common method/media type/header/status table에 포함됨
- nil deposit prover 503
- raw body 초과
- malformed framing은 admission 획득 전 거부
- semantic invalid request는 prover를 호출하지 않고 permit release
- `DepositProofCircuitID`가 admission에 전달됨
- busy 429와 `retryable=true`
- proof runner 실패 500
- nil/invalid prover response가 panic하지 않음
- error body에 request canary가 포함되지 않음
- permit은 actual prove return 전까지 유지되고 정확히 한 번 release

기존 body-limit/redaction/nil-response table에 deposit row를 추가해 공통 boundary의 누락을 막는다.

### T-04. Service test

대상:

- `x/privacy/client/sdk/proverservice/service_test.go`
- `x/privacy/client/sdk/proverservice/admission_test.go`

검증:

- `DefaultRuntimeInfo`에 deposit route/circuit 포함
- required circuit order가 `privacyzk.RequiredCircuitIDs()`와 일치
- default admission/metrics에 독립 deposit entry 존재
- bearer auth가 deposit route에 적용
- outer raw/decompressed body limit 적용
- gzip success와 invalid/unsupported content encoding 400
- auth/content-encoding 조기 종료 response에도 common `no-store`가 적용
- reference handler의 deposit prover가 non-nil
- readiness가 Deposit R1CS/PK를 요구
- health/readiness JSON route/circuit advertisement

실제 artifact IO가 필요 없는 assertion을 우선하고, circuit list helper를 분리해 readiness/preflight wiring을 직접 검증한다.

### T-05. 실제 Groth16 integration test 한 개

대상 위치:

- `x/privacy/client/sdk/provertransport` 또는 `x/privacy/client/sdk/proverservice`의 `*_integration_test.go`

흐름:

```text
canonical PreparedDepositProverPayload
  -> DepositProofRequest
  -> httptest bounded handler
  -> real DepositCircuit compile/setup/prove
  -> HTTP response validation
  -> canonical proof decode
  -> matching public witness로 groth16.Verify
```

원칙:

- 실제 compile/setup/prove test는 한 개만 둔다.
- 이 test에서 `BuildPreparedDepositProof` positive path와 HTTP round-trip을 함께 검증한다.
- 나머지 handler/service test는 stub을 사용하되 success stub이 canonical proof를 필요로 하면 이 test package의 `sync.Once` real-proof helper가 만든 동일 request/response를 재사용한다. 별도의 prove call을 추가하지 않는다.
- Setup/proof fixture는 `sync.Once` 등으로 한 test process에서 재사용하고 repository artifact나 static proof file로 commit하지 않는다.
- 기존 keeper/circuit deposit e2e를 중복 복제하지 않는다.
- Mutated amount/asset/commitment public witness에서 verification이 실패하는 기존 circuit/keeper test를 유지한다.

### T-06. Language-neutral conformance fixture

신규:

- `x/privacy/client/sdk/conformance/testdata/privacy_deposit_prover_contract.json`
- `x/privacy/client/sdk/conformance/deposit_prover_contract_test.go`

Fixture가 고정할 내용:

- schema version `clairveil.proverd.deposit-api.contract.v1`
- method/path/content type
- request/payload/response/proof version
- field encoding, exact byte/hex length와 amount range
- field가 unsigned big-endian이고 public key가 canonical compressed encoding이라는 규칙과 concrete vector
- one canonical positive request와 expected commitment
- response commitment binding
- proof byte/hex length, compressed point offset `[0, 32, 96, 132]`, big-endian commitment-count framing과 canonical round-trip 규칙
- raw JSON strict-decoder negative vector
- field mutation/commitment mismatch negative vector
- common error code/status/retryability mapping
- Deposit/transfer/withdraw/batch의 request failure 400, prover failure 500 mapping
- timeout, same-endpoint retry, no automatic failover 정책

기존 수정:

- `privacy_prover_http_api_contract.json`을 `schema_version=v3`로 올리고, 현재 빠져 있는 `batch_transfer_route`와 신규 `deposit_route`를 모두 route inventory에 추가
- `prover_http_contract_test.go`에서 deposit/transfer/withdraw/batch의 Go path/version constant와 fixture를 비교

`privacy_prover_example_bundle.json`은 transfer/withdraw prepared-payload example이라는 기존 범위를 유지한다. Deposit-specific vector는 새 fixture가 담당하며 억지로 다른 proof model에 넣지 않는다.

### T-07. 기존 테스트의 현재 상태 불일치 수정

다음을 repository 전체에서 검색해 수정한다.

- proof route가 transfer/withdraw/batch 세 개뿐이라는 exact list/count
- prover circuit이 joinsplit/spend/batch 세 개뿐이라는 assertion
- default admission map에 deposit이 없는 상태를 기대하는 test
- `isProofRoute` auth/body-limit table에서 deposit 누락
- readiness/preflight가 deposit artifact 없이 성공한다고 가정하는 test
- “transfer and withdraw”라는 이름이 실제로 모든 non-batch proof route를 뜻하는 test
- transfer/withdraw/batch prover error 또는 invalid response를 `400 proof_failed`로 기대하는 test
- proof route가 missing/wrong `Content-Type`을 허용하거나 `Cache-Control`/`Allow`를 검사하지 않는 test
- Content-Type 없이 body-limit/framing/semantic/admission 결과를 기대해 새 415 policy에 가려지는 existing test/helper

Phase 2 gate:

```bash
go test ./x/privacy/client/sdk/deposit -count=1
go test ./x/privacy/client/sdk/provertransport -count=1
go test ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/conformance -count=1
go test ./cmd/clairveil-proverd -count=1
```

모든 test가 통과하기 전에는 문서에서 endpoint를 완료 상태로 표현하지 않는다.

## 8. Phase 3 — 문서화

### D-01. Canonical API 문서 신규 작성

신규 bilingual pair:

- `docs/clairveil-proverd-http-api.md`
- `docs/clairveil-proverd-http-api-kr.md`
- `docs/clairveil-proverd-deposit-api.md`
- `docs/clairveil-proverd-deposit-api-kr.md`

General prover HTTP 문서는 다음을 authoritative하게 설명한다.

```text
proof route inventory와 route별 envelope version
common Content-Type/Content-Encoding/body-limit 규칙
405 Allow와 Cache-Control
auth·timeout·privacy 공통 경계
400/401/404/405/413/415/429/500/503 error contract
versioning과 compatibility 규칙
```

Deposit 문서는 다음 route-specific 계약을 authoritative하게 설명한다.

```text
POST /v1/prover/deposit
request schema
response schema
versioning 규칙
commitment/proof validation
deposit witness disclosure와 downstream assembly 경계
common error contract 적용 방식
```

Deposit 문서에서 common HTTP policy를 다시 독자적으로 정의하지 않고 general prover HTTP 문서/schema로 연결한다. 특정 SDK 이름, provider API, release history 또는 migration 절차를 넣지 않는다. 예시는 “JS/TS SDK 또는 downstream client” 수준으로만 표현한다.

### D-02. 잘못된 현재 상태 수정

필수 수정:

| 문서 | 수정 내용 |
| --- | --- |
| `docs/clairveil-privacy-accounting-design-note.md` / `-kr.md` | “deposit HTTP endpoint 없음”과 downstream이 endpoint를 새로 만들어야 한다는 문장 제거 |
| `docs/clairveil-client-api-checklist.md` / `-kr.md` | canonical deposit route/version/payload/proof validation과 전 route 공통 status/header policy 추가 |
| `docs/clairveil-js-sdk-handoff.md` / `-kr.md` | Legacy filename은 discoverability를 위해 유지하되 prover/deposit section은 general/deposit canonical API 문서로 연결하는 thin handoff로 축소하고 특정 package version/provider/release 상태를 source of truth처럼 쓰지 않음 |
| `docs/clairveil-proverd-remote-production-profile.md` / `-kr.md` | deposit witness/route/artifact readiness와 전 route media type/status/no-store/admission/auth/privacy 추가 |
| `docs/clairveil-operations-guide.md` / `-kr.md` | 운영 route 목록, deposit disclosure, common HTTP policy, admission/readiness 경계 추가 |
| `docs/clairveil-architecture.md` / `-kr.md` | Deposit local/remote proving flow와 versioned contract 추가 |
| `docs/clairveil-client-ux-flows.md` / `-kr.md` | Deposit proof progress/timeout/retry/failure UX 추가 |
| `docs/clairveil-client-product-brief.md` / `-kr.md` | Deposit proof provider와 trust decision 추가 |
| `docs/clairveil-client-risk-decisions.md` / `-kr.md` | Remote deposit witness disclosure와 logging prohibition 추가 |
| `docs/clairveil-downstream-cosmos-integration-guide.md` / `-kr.md` | Official proof acquisition route와 chain/client responsibility 분리 |
| `docs/clairveil-security-best-practices-review.md` / `-kr.md` | Deposit route도 bounded raw handler/auth/redaction 대상임을 반영 |
| `docs/clairveil-threat-model.md` / `-kr.md` | Wallet→prover boundary와 sensitive asset을 deposit까지 확대 |
| `docs/clairveil-testing-guide.md` / `-kr.md` | Deposit contract/focused test/fixture와 common HTTP policy regression 명령 추가, external SDK 현재-version snapshot 제거 |
| `docs/clairveil-release-handoff-pack.md` / `-kr.md` | Deposit API와 common status correction acceptance item 및 language-neutral migration impact 추가, external SDK version snapshot/전용 migration 지시는 제거 |

문서를 수정할 때 canonical API 전체를 여러 문서에 복사하지 않는다. 전용 API 문서로 link하고 각 문서에는 해당 audience의 결정과 boundary만 둔다.

### D-03. Schema, fixture index와 release metadata

신규:

- `docs/schemas/clairveil-proverd-http-api.schema.json`
  - `$id`, Draft 2020-12, exact version/required field/pattern/length와 `additionalProperties: false`를 가진 language-neutral schema로 작성한다.
  - `$defs`에 canonical request, response, error, full route inventory, common status/header mapping과 deposit conformance fixture object를 각각 노출한다.
  - Root `oneOf`는 `privacy_prover_http_api_contract.json`과 `privacy_deposit_prover_contract.json`을 검증한다.
  - `clairveil-js-wallet-contract.schema.json`을 import하거나 특정 SDK package shape를 참조하지 않는다.

수정:

- `docs/schemas/clairveil-js-wallet-contract.schema.json`
  - Deposit API definition을 추가하지 않는다.
  - Generic `routeContract`/`proverHttpApiContract` `$defs`를 제거하고 wallet-facing fixture만 계속 소유한다.
- `examples/js-sdk-fixture-validator/src/index.ts`와 해당 README pair
  - `privacy_prover_http_api_contract.json`을 JS wallet schema로 검증하는 wiring과 generic route semantic assertion만 제거한다.
  - Deposit client/provider code나 JS-specific deposit test는 추가하지 않는다.
  - Canonical 검증은 새 schema와 Go conformance test가 소유한다고 안내한다.
- `docs/schemas/README.md` / `README-kr.md`
  - 새 dedicated schema/conformance fixture와 version layer를 설명한다.
  - External SDK의 현재 release/compatibility 상태는 제거하고 repository-owned contract와 validation command만 남긴다.
- `docs/README.md` / `README-kr.md`
  - 신규 general/deposit API 문서 pair를 index한다.
- `README.md` / `README-kr.md`
  - General `clairveil-proverd` API reference link만 추가하고 SDK-specific 사용법은 넣지 않는다.
- `scripts/release-pack-paths.txt`
- `scripts/release-pack-required-files.txt`
  - 신규 general/deposit API 문서, dedicated schema, deposit conformance fixture와 general prover HTTP fixture를 release input에 추가한다.
- `scripts/release-manifest-template.txt`
  - “JS/web wallet JSON schemas” 표현을 language-neutral API schema와 wallet-facing schema의 분리된 소유권에 맞게 교정한다.
- `CHANGELOG.md` / `CHANGELOG-kr.md`
  - `Unreleased`에 additive deposit prover API와 unchanged circuit/chain wire contract를 기록한다.
  - Existing proof route의 Content-Type enforcement와 `400 proof_failed` → `500 proof_failed` 교정을 compatibility/migration impact로 명시한다.
  - Success request/response version과 `ErrorResponseVersion=v1`은 unchanged임을 명시한다.

### D-04. Historical plan과 benchmark 문구

- `plans/clairveil-public-benchmark-plan-kr.md`의 “현재 endpoint 없음”은 과거 기록을 지우지 않고 dated current-status note로 교정한다.
- Deposit HTTP benchmark/load profile은 이번 구현 범위가 아님을 명시한다.
- 완료된 과거 changelog를 현재 상태였던 것처럼 소급 수정하지 않는다.
- CLI 문서는 remote deposit option을 실제 구현하지 않으므로 CLI가 remote prover를 지원한다고 쓰지 않는다.

### D-05. 특정 downstream 구현 비의존성 검토

다음 문자열과 의미를 검토한다.

```bash
rg -n 'ClairveilJS|clairveil-js|depositProofUrl|note_json' \
  docs plans x/privacy/client/sdk/conformance scripts
```

허용:

- 기존 historical 사실을 명확히 historical로 유지하는 문장
- 별도 repository 책임을 설명하기 위한 최소 경계 문장

금지:

- 특정 SDK의 현재 request shape를 canonical contract 근거로 삼는 문장
- 특정 SDK release/version migration을 이 repository의 API 문서에 기록
- canonical request example에 `note_json` 사용
- Deposit만 별도 Content-Type/status/header policy를 갖는다고 설명

Phase 3 gate:

- 신규 general/deposit API 문서가 각각 EN/KR pair로 존재한다.
- 모든 현재 handoff 문서가 endpoint 존재와 versioned schema를 정확히 설명한다.
- General prover 문서/fixture가 네 proof route의 common HTTP policy와 compatibility impact를 정확히 설명한다.
- New fixture/schema/docs가 index와 release manifest에 포함된다.
- `examples/clairveil-dapp/**`는 변경되지 않는다.

## 9. Phase 4 — 최종검증

### V-01. Formatting과 정적 diff 검증

```bash
gofmt -w <이번 작업에서 변경한 Go 파일>
git diff --check
git status --short
```

다음 경로에는 diff가 없어야 한다.

```bash
git diff --exit-code -- \
  proto/clairveil/privacy/v1 \
  x/privacy/circuit \
  x/privacy/zk \
  examples/clairveil-dapp
```

`x/privacy/zk`에 runtime service wiring을 넣지 않는다. Deposit artifact는 `proverservice` provider에서 registry를 통해 사용한다.

### V-02. Focused test와 race

```bash
go test ./x/privacy/client/sdk/deposit -count=1
go test ./x/privacy/client/sdk/provertransport -count=1
go test ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/conformance -count=1
go test ./cmd/clairveil-proverd -count=1

go test -race \
  ./x/privacy/client/sdk/provertransport \
  ./x/privacy/client/sdk/proverservice \
  -count=1
```

### V-03. Build와 repository-wide regression

```bash
go build ./cmd/clairveil-proverd
go test ./x/privacy/... -count=1
go test ./... -count=1
```

### V-04. Documentation/release/example validation

```bash
make docs-check
make examples
```

가능하면 최종 통합 gate도 실행한다.

```bash
make check
```

`make check`가 environment/resource 문제로 실행되지 못하면 실행한 하위 command와 미실행 이유를 completion ledger에 정확히 기록한다.

### V-05. Contract acceptance 확인

최종 test 또는 local smoke evidence에서 다음을 확인한다.

- Valid request가 HTTP 200과 versioned response를 반환한다.
- Deposit/transfer/withdraw/batch가 같은 Content-Type, 405 `Allow`, `Cache-Control: no-store` policy를 사용한다.
- 네 route 모두 invalid request는 `400 invalid_request`, post-validation prover/response failure는 `500 proof_failed`를 반환한다.
- Returned proof가 matching Deposit public witness로 검증된다.
- Amount/asset/key/randomness/commitment mutation은 proving 전에 거부된다.
- Unsupported version과 unknown field가 fail closed한다.
- `/readyz`는 Deposit R1CS/PK가 없으면 ready가 아니다.
- `/debug/vars`는 deposit admission gate를 표시한다.
- Bearer-enabled service에서 unauthenticated deposit request는 401이다.
- Busy response만 `retryable=true`다.
- Error/log/metric에 witness canary가 없다.
- Existing transfer/withdraw/batch test가 모두 통과한다.
- Circuit/artifact/proto/DApp diff가 없다.

## 10. 파일별 작업표

| 단계 | 파일 | 작업 |
| --- | --- | --- |
| 구현 | `x/privacy/client/sdk/provertransport/http.go`, `proverservice/service.go` | 전 proof route common media type/header/status policy 정규화 |
| 구현 | `x/privacy/client/sdk/deposit/payload.go` | Versioned prepared payload/proof, canonical build/validation |
| 구현 | `x/privacy/client/sdk/provertransport/contract.go` | Deposit request/response envelope와 strict decoder |
| 구현 | `x/privacy/client/sdk/provertransport/http.go` | Route, handler, client, constructor compatibility |
| 구현 | `x/privacy/client/sdk/proverservice/service.go` | Artifact/runner/readiness/preflight/runtime/auth wiring |
| 구현 | `x/privacy/client/sdk/proverservice/admission.go` | Deposit circuit admission |
| 테스트 | `x/privacy/client/sdk/deposit/payload_test.go` | Payload/proof positive/negative test |
| 테스트 | `x/privacy/client/sdk/provertransport/*_test.go` | 전 route common HTTP policy, deposit contract/HTTP/client/real proof test |
| 테스트 | `x/privacy/client/sdk/proverservice/*_test.go` | Runtime/auth/body/admission/readiness test |
| 테스트 | `x/privacy/client/sdk/conformance/deposit_prover_contract_test.go` | Fixture-to-Go contract binding |
| Fixture | `x/privacy/client/sdk/conformance/testdata/privacy_deposit_prover_contract.json` | Language-neutral canonical vectors |
| Fixture | `x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json` | Deposit과 기존 batch를 포함한 전체 route/version inventory |
| 문서 | `docs/clairveil-proverd-http-api*.md` | 전 proof route common HTTP policy reference |
| 문서 | `docs/clairveil-proverd-deposit-api*.md` | Deposit route-specific canonical API reference |
| 문서 | Client/downstream/proverd/security 문서 pair | Current-state correction와 audience boundary |
| Schema | `docs/schemas/clairveil-proverd-http-api.schema.json` | Language-neutral common HTTP/deposit contract schema |
| Schema audit | `docs/schemas/clairveil-js-wallet-contract.schema.json` | Generic prover HTTP definition을 제거하고 wallet fixture scope 유지 |
| Ownership cleanup | `examples/js-sdk-fixture-validator/src/index.ts`, README pair | Generic prover HTTP fixture validation을 Go/schema로 이관; deposit client code는 추가하지 않음 |
| Release | `CHANGELOG*`, docs/plan index, release-pack manifests | Handoff/release inclusion |

## 11. 권장 commit 경계

실제 repository policy가 squash를 요구하지 않는다면 다음 순서를 권장한다.

1. `refactor(provertransport): normalize proof route HTTP policy`
   - I-00과 T-00을 함께 포함해 전 route Content-Type/header와 400/500 status 분류를 교정
   - Existing success request/response contract는 변경하지 않음
2. `feat(proverd): add canonical versioned deposit proof endpoint`
   - Phase 1의 deposit production code 포함
3. `test(proverd): cover common policy deposit service and conformance`
   - Phase 2 test와 fixture 포함
4. `docs(proverd): publish language-neutral HTTP and deposit contracts`
   - Phase 3 general/deposit bilingual docs/schema/index/changelog와 compatibility impact 포함
5. `chore(plan): close deposit API implementation plan`
   - 모든 gate 통과 뒤 plan status/completion ledger 갱신

작업 자체는 구현 → 테스트 순서로 수행하되 commit은 해당 focused test가 통과한 뒤 자른다. 특히 I-00만 적용해 기존 400 assertion이 실패하는 commit을 만들지 않고 T-00까지 완료한 뒤 첫 commit을 만든다. 각 commit 사이에서 이전 단계 gate를 통과하며, test 실패 상태나 문서가 구현보다 앞선 완료 상태를 중간 commit에 남기지 않는다.

## 12. 작업 체크리스트

### 구현

- [x] I-00 전 proof route common HTTP policy 정규화
- [x] I-01 prepared deposit payload/proof 구현
- [x] I-02 HTTP request/response contract 구현
- [x] I-03 raw handler와 Go client 구현
- [x] I-04 bounded service/admission/readiness wiring
- [x] I-05 entrypoint/packaging 영향 확인
- [x] Phase 1 gate 통과

### 테스트

- [x] T-00 common HTTP policy regression test
- [x] T-01 deposit payload/proof unit test
- [x] T-02 transport strict contract test
- [x] T-03 handler/client boundary test
- [x] T-04 service/admission/readiness test
- [x] T-05 실제 Groth16 integration test 한 개
- [x] T-06 language-neutral conformance fixture/test
- [x] T-07 기존 세-route 가정 수정
- [x] Phase 2 gate 통과

### 문서화

- [x] D-01 canonical bilingual API 문서
- [x] D-02 잘못된 current-state 문서 수정
- [x] D-03 schema/index/release metadata 갱신
- [x] D-04 historical/benchmark 문구 교정
- [x] D-05 특정 downstream 구현 비의존성 검토
- [x] Phase 3 gate 통과

### 최종검증

- [x] V-01 format/diff/scope 검증
- [x] V-02 focused/race test
- [x] V-03 build/repository-wide test
- [x] V-04 docs/release/example validation
- [x] V-05 contract acceptance 확인
- [x] Plan 상태를 `Completed record`로 변경
- [x] `plans/README.md`와 `plans/README-kr.md` 상태 갱신

### Repository 재감사

- [x] R-01 상위 architecture/remote/handoff/client/security EN/KR contract inventory를 네 proof route와 route별 binding에 정렬
- [x] R-02 semantic contract inventory 누락을 `make docs-check`에서 fail closed하도록 검증 추가
- [x] R-03 `GO-2026-5158`, `GO-2026-6061` reachable vulnerability를 fixed dependency version으로 해소
- [x] R-04 JSON `govulncheck` scanner의 nonzero scan/usage status를 항상 거부해 policy-approved finding이 오류를 숨기지 못하게 fail closed
- [x] R-05 `make release-check`와 release package 검증을 clean commit state에서 재실행
- [x] R-06 금지 경로, language-neutral ownership, completion ledger를 독립 재검토
- [x] R-07 clean supported environment에서 드러난 미선언 Python `jsonschema` 의존성을 required Go toolchain 기반 Draft 2020-12 conformance gate로 교체하고 `make ci`/`make release-check`를 fresh venv에서 재실행
- [x] R-08 `/healthz`와 `/readyz`의 route/circuit inventory를 configured `ProverSet`에서 산출하여 partial compatibility constructor가 미구성 deposit/batch capability를 advertise하지 않도록 교정
- [x] R-09 모든 `Content-Encoding` field와 comma-separated coding을 검사하여 repeated, multiple, empty, unsupported encoding을 body read 전에 fail closed
- [x] R-10 EN/KR testing guide의 `make examples` command inventory를 Makefile의 8개 실제 command와 정렬하고 `make docs-check`에 drift 방지 gate 추가
- [x] R-11 capability-boundary remediation clean commit에서 focused/race/full test, fresh supported environment `make ci`/`make vulncheck`/`make release-check`, release package와 금지 경로를 재검증

## 13. Completion ledger

구현 세션은 완료 시 아래 표를 채운다.

| 항목 | 결과 |
| --- | --- |
| Common HTTP policy commit | `3bc69a82c5fa3a613e872fc2168d3301fa4fee22` |
| 구현 commit | `9b50f33b9d92f947c77dccdf30af19c2c2b9b063` |
| 테스트 commit | `75f8ce0d19a2c035855823a54c9a220988fb5d51` |
| 문서 commit | `c2362c7bd55658e43bdee7892f123ff851fba7b2` |
| Security dependency remediation commit | `4d9f013684ae68318bf1ce556376afe789c0c1c3` |
| Documentation consistency audit commit | `8120bd2a4f070af710f6e9d7bce259bfe9f735c1` |
| Security scanner fail-closed commit | `60e98ddd4ad71743570b8705960ba6ced1ffed18` |
| Clean-environment schema gate remediation commit | `4d7ecb1612ed4570f86ef7c107738cd80f8a472e` |
| Runtime capability/HTTP framing remediation commit | `522d2b6e82120787ff174044c02a2cc78e6d6e65` |
| 최종 재검증 HEAD | `522d2b6e82120787ff174044c02a2cc78e6d6e65` (실제 `ProverSet` inventory, strict `Content-Encoding`, `make examples` 문서/gate 정렬을 포함한 전체 gate 검증 대상 clean HEAD; 이 completion ledger는 후속 `chore(plan)` commit) |
| Focused test | PASS — provertransport, proverservice의 partial/full runtime inventory와 repeated/comma-separated/multiple/empty `Content-Encoding`, `cmd/clairveil-proverd` |
| Race test | PASS — provertransport, proverservice (`LC_DYSYMTAB` linker warning만 발생, test exit 0) |
| Build/privacy regression | PASS — `go build ./cmd/clairveil-proverd`, `go test ./x/privacy/... -count=1` |
| `go test ./...` | PASS — clean `522d2b6e82120787ff174044c02a2cc78e6d6e65` HEAD에서 `-count=1` fresh 실행 |
| Schema validation | PASS — `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2` 기반 Draft 2020-12 schema compile, canonical fixture 2건 accept, unknown top-level field와 uint64 초과 amount negative mutation 2건 reject |
| Clean supported environment regression | PASS — 기존 `629ddb34ee04ca287436a801c9b0ea0d418369e1`에서 fresh venv의 `make ci`/`make release-check`가 미선언 Python `jsonschema`로 exit 2임을 재현한 뒤, required Go toolchain gate로 교체하여 같은 조건에서 해소 |
| `make docs-check` | PASS — third-party package가 없는 fresh Python 3.12 venv에서 link/pair/index/changelog/release closure, 두 prover fixture schema, 상위 문서 current-contract inventory/binding semantic 검증, Makefile의 `make examples` 8-command inventory와 EN/KR guide 일치 검증 |
| `make examples` | PASS — audit key, JS fixture, prover client, DApp CI/check/test와 packaged ClairveilJS type/export/smoke 8개 command 전체 실행 |
| `make check` / `make ci` | PASS — clean `522d2b6e82120787ff174044c02a2cc78e6d6e65` HEAD, fresh venv, Go `1.25.12`, Node.js `22.16.0`, Python `3.12.8`을 명시한 clean PATH에서 docs, full Go test/build, example 전체 gate exit 0 |
| `make vulncheck` | PASS — 새 schema validator dependency를 포함해 scanner/policy gate 통과; `go.opentelemetry.io/otel v1.44.0`으로 `GO-2026-5158`, `google.golang.org/grpc v1.82.1`으로 `GO-2026-6061` 해소; JSON scanner nonzero status fail-closed test 통과; fixed version이 없는 policy 예외 3건만 유지 |
| `make release-check` | PASS — clean `522d2b6e82120787ff174044c02a2cc78e6d6e65` HEAD와 third-party package 없는 fresh Python 3.12 venv에서 `make ci`, `make vulncheck`, localnet smoke, privacy E2E, batch static contract, bulk transfer localnet required step 전체 통과; optional `grpcurl` 없이 `RPC_PORT=38657`, `P2P_PORT=38656`, `ABCI_PORT=38658`, `GRPC_PORT=29090`, `API_PORT=21317`, `PPROF_PORT=26060`, `PROVERD_PORT=28081` 격리 port 사용 |
| Release package | PASS — `make release-pack-verify`, checksum, 153 required files, manifest commit `522d2b6e82120787ff174044c02a2cc78e6d6e65` 검증 |
| Circuit/artifact/proto diff | 없음 — `proto/clairveil/privacy/v1`, `x/privacy/circuit`, `x/privacy/zk` 무변경 |
| `examples/clairveil-dapp/**` diff | 없음 |
| 미실행/known issue | 필수 gate 전부 실행. Bulk readiness의 external prover-pool scale는 `PROVERD_URLS`가 없어 `required=false`로 skip. `npm audit --omit=dev`는 0건; 금지 범위인 DApp의 dev-only `esbuild 0.28.0` Windows development-server low advisory는 pre-existing/out-of-scope로 무변경. Non-failing macOS linker warning은 위 Race test에 기록 |

모든 필수 gate가 통과하고 미해결 구현 작업이 없을 때만 상태를 `Completed record`로 바꾼다. 문서만 작성됐거나 일부 test가 생략된 상태는 완료가 아니다.
