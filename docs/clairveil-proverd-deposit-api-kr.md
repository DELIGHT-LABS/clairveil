# Clairveil Proverd Deposit API

> English version: [clairveil-proverd-deposit-api.md](clairveil-proverd-deposit-api.md)

이 문서는 `POST /v1/prover/deposit`의 authoritative한 route-specific 계약입니다. [공통 Proverd HTTP API 정책](clairveil-proverd-http-api-kr.md)을 보완하며 독자적으로 다시 정의하지 않습니다. 기계 판독 계약은 [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json)입니다.

## Request

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

| Field | Canonical validation |
| --- | --- |
| Request/payload `version` | 정확히 `v1` |
| `receiver_spend_pubkey_hex`, `receiver_view_pubkey_hex` | 정확히 64 lowercase hex; canonical 32-byte compressed BN254 twisted-Edwards point, on-curve, non-identity, prime subgroup |
| `amount` | Canonical uint64 decimal string: `0` 또는 leading zero 없는 `18446744073709551615` 이하 십진수 |
| `asset_id_hex`, `randomness_hex` | 정확히 64 lowercase hex; canonical 32-byte unsigned big-endian BN254 scalar-field encoding |
| `note_commitment_hex` | 정확히 64 lowercase hex; canonical non-zero 32-byte BN254 field encoding |

Hex에 `0x` prefix를 붙이지 않습니다. Unknown field, duplicate key, trailing JSON, unsupported version, legacy request shape은 `400 invalid_request`로 fail closed합니다.

Service는 두 compressed public key를 복원하고 memo가 빈 note를 구성한 뒤 NoteV1 validation과 commitment 재계산을 수행합니다. 재계산한 commitment는 `note_commitment_hex`와 같아야 하며, 복원된 note의 commitment와 nullifier는 non-zero여야 합니다. Asset ID와 randomness 각각의 zero는 이 최종 invariant를 통과하는 경우에만 허용됩니다.

## Response

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

두 response version은 정확히 `v1`입니다. `proof.note_commitment_hex`는 제출한 commitment 및 service가 재계산한 commitment 모두와 같아야 합니다. `proof_hex`는 정확히 328 lowercase hex(164 bytes)이고 canonical BN254 Groth16 frame validation을 통과해야 합니다. Service는 생성한 frame을 반환 전에 검증하고, caller는 사용 전에 version, commitment binding, frame을 검증합니다.

## Versioning

`/v1` path, request envelope, payload, response envelope, proof object는 독립적으로 versioning합니다. Field shape, encoding, validation 의미의 변경은 영향을 받은 object의 version bump가 필요합니다. 이 `v1` shape에 silent optional field를 추가하거나 legacy input을 auto-detect하거나 다른 schema로 재시도 decoding하면 안 됩니다.

## Disclosure와 downstream assembly 경계

Deposit prover는 receiver public key, amount, asset ID, randomness를 받으며 note commitment와 nullifier를 도출할 수 있습니다. 따라서 remote prover 선택은 일반 public RPC 선택이 아니라 trusted-prover privacy decision입니다. Request에는 memo, creator, denom string, encrypted note, seed, chain ID를 넣지 않습니다.

Endpoint는 `MsgDeposit` 생성, note encryption, transaction signing/broadcast를 수행하지 않습니다. 언어와 SDK에 무관한 downstream flow는 다음과 같습니다.

1. Receiver key, amount, denom-derived asset ID, randomness, 선택한 memo로 NoteV1을 구성합니다.
2. Commitment를 계산하고 memo를 포함한 전체 note plaintext를 canonical deposit envelope로 암호화합니다.
3. 위의 memo-free payload로 proof를 요청합니다.
4. Response version, commitment equality, proof framing을 검증합니다.
5. `proof_hex`, `note_commitment_hex`를 bytes로 바꾸고 같은 amount/denom 및 encrypted note와 함께 `MsgDeposit`을 구성합니다.
6. Sign/broadcast합니다. Keeper는 denom에서 asset ID를 도출하고 amount, commitment, proof를 최종 검증합니다.

Proof는 memo, encrypted note, creator, denom string 자체를 bind하지 않습니다. Amount, denom-derived asset ID, commitment를 bind하고, commitment는 두 receiver key, amount, asset ID, randomness를 bind합니다. Downstream client는 이 경계를 보존해야 하며 prover response만으로 encrypted note를 재구성했다고 가정하면 안 됩니다.

공통 media type, limit, status/error, authentication, timeout, cache, compatibility 규칙은 [Proverd HTTP API](clairveil-proverd-http-api-kr.md)를 따릅니다.
