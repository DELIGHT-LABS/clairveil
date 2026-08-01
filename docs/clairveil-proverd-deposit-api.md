# Clairveil Proverd Deposit API

> Korean version: [clairveil-proverd-deposit-api-kr.md](clairveil-proverd-deposit-api-kr.md)

This document is the authoritative route-specific contract for `POST /v1/prover/deposit`. It supplements, and does not redefine, the [common Proverd HTTP API policy](clairveil-proverd-http-api.md). The machine-readable contract is [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json).

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
| Request and payload `version` | Exactly `v1` |
| `receiver_spend_pubkey_hex`, `receiver_view_pubkey_hex` | Exactly 64 lowercase hex characters; canonical 32-byte compressed BN254 twisted-Edwards point, on-curve, non-identity, prime subgroup |
| `amount` | Canonical uint64 decimal string: `0` or no-leading-zero decimal through `18446744073709551615` |
| `asset_id_hex`, `randomness_hex` | Exactly 64 lowercase hex characters; canonical 32-byte unsigned big-endian BN254 scalar-field encoding |
| `note_commitment_hex` | Exactly 64 lowercase hex characters; canonical non-zero 32-byte BN254 field encoding |

Hex values have no `0x` prefix. Unknown fields, duplicate keys, trailing JSON, unsupported versions, and legacy request shapes fail closed as `400 invalid_request`.

The service restores both compressed public keys, constructs a memo-empty note, applies the NoteV1 validation, and recomputes its commitment. The recomputed commitment must equal `note_commitment_hex`; the reconstructed note's commitment and nullifier must be non-zero. Zero asset ID and randomness are individually permitted only when that final invariant holds.

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

Both response versions are exactly `v1`. `proof.note_commitment_hex` must equal both the submitted commitment and the commitment recomputed by the service. `proof_hex` is exactly 328 lowercase hex characters (164 bytes) and must pass canonical BN254 Groth16 frame validation. The service validates the generated frame before returning it; callers validate the versions, commitment binding, and frame before using it.

## Versioning

The `/v1` path, request envelope, payload, response envelope, and proof object are independently versioned. A field-shape, encoding, or validation-meaning change requires a version bump for its affected object. Do not add a silent optional field to this `v1` shape, auto-detect legacy input, or retry decoding against another schema.

## Disclosure and downstream assembly boundary

The deposit prover receives receiver public keys, amount, asset ID, and randomness. It can derive the note commitment and nullifier. Selecting a remote prover is therefore a trusted-prover privacy decision, not an ordinary public RPC choice. The request excludes memo, creator, denom string, encrypted note, seed, and chain ID.

The endpoint neither creates `MsgDeposit`, encrypts a note, nor signs or broadcasts a transaction. A language-neutral downstream flow is:

1. Construct NoteV1 from receiver keys, amount, denom-derived asset ID, randomness, and the selected memo.
2. Compute the commitment and encrypt the complete note plaintext, including memo, in the canonical deposit envelope.
3. Request this proof with the memo-free payload above.
4. Validate response versions, commitment equality, and proof framing.
5. Convert `proof_hex` and `note_commitment_hex` to bytes; build `MsgDeposit` with the same amount/denom and encrypted note.
6. Sign and broadcast. The keeper derives asset ID from the denom and finally verifies amount, commitment, and proof.

The proof does not bind memo, encrypted note, creator, or the denom string itself. It binds amount, the denom-derived asset ID, and the commitment; the commitment binds both receiver keys, amount, asset ID, and randomness. A downstream client must preserve this boundary and must not treat a prover response as an encrypted-note reconstruction.

For common media-type, limits, status/error, authentication, timeout, cache, and compatibility rules, use the [Proverd HTTP API](clairveil-proverd-http-api.md).
