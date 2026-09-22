# Clairveil Proverd HTTP API

> Korean version: [clairveil-proverd-http-api-kr.md](clairveil-proverd-http-api-kr.md)

This is the current shared HTTP contract for `clairveil-proverd`. It describes the only live proof route in this checkout. The machine-readable contract is [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json); `privacy_audit_field_prover_contract.json` is a wire-shape-only mock fixture, not a semantic proving vector and not accepted by `ValidateAuditFieldProofRequest` or response validation.

## Current route

| Route | Request envelope `version` | Response envelope `version` |
| --- | --- | --- |
| `POST /v2/prover/audit-field` | `v1` | `v1` |

`/v2` is the HTTP route major version. It is deliberately independent of the JSON envelope version: both request and response structs currently require the literal string `v1`.

The V2 route is development-only. `clairveil-proverd` requires a matching `privacy-note-v1-u128-audit-field-v1` artifact directory and refuses to start without `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR`. Use an explicitly trusted, locally controlled prover: the complete witness includes transaction secrets.

## uint128 amount contract

Each note/disclosure amount and each operation's input/output totals are in `0..M`; public deposit/withdraw amounts are in `1..M`, where `M = 340282366920938463463374607431768211455`. PI23 keeps its order and count; `PublicAmount` is encoded in a 32-byte field with the upper 16 bytes zero. Conservation alone is insufficient: `[M,1] -> [M,1]` is rejected, while `[M-1,1] -> [M,0]` is accepted. Epoch, expiry, sequence, and index widths are unchanged.

JSON amounts are canonical decimal strings; JS calculations use `bigint`. Per-asset wallet balances, payroll/audit totals across operations, and cumulative deposits/withdrawals have no uint128 cap. Reserve cumulative deposits `D` and withdrawals `W` are arbitrary-precision non-negative strings; only their difference `D-W` and current bank amounts retain the existing 256-bit bound. Wallet and pool balances of `2M` are therefore valid.

The current codec is `privacy-fixed-v2`/binary version `2`, and wallet files use version `3`. Note/disclosure plaintext amounts use 16-byte unsigned BE; their domains are `clairveil.note-plaintext.v2`, `clairveil.disclosure-plaintext.v2`, and `clairveil.encrypted-envelope.v2`. Plaintexts are respectively `358` and `400` bytes; deposit/transfer recovery envelopes are `406` and `438` bytes; encrypted disclosure envelopes are `480` bytes. Use fresh genesis and new artifacts; old payload/wallet decoders are unsupported.

## Request and response binding

All requests are strict JSON objects with exactly these fields:

```json
{
  "version": "v1",
  "circuit_set_id": "privacy-note-v1-u128-audit-field-v1",
  "circuit_id": "deposit-audit-field-u128-v1",
  "artifact_hash": "base64-encoded 32 bytes",
  "public_inputs": ["base64-encoded canonical field element", "... exactly 23 entries"],
  "witness": "base64-encoded complete gnark witness"
}
```

`circuit_id` must be one of `deposit-audit-field-u128-v1`, `spend-audit-field-u128-v1`, `joinsplit-2x2-audit-field-u128-v1`, or `batch-joinsplit-16x32-audit-field-u128-v1`. Go's standard JSON encoding represents every `[]byte` field as a base64 JSON string; `public_inputs` is therefore an array of base64 strings, not hexadecimal. Each supplied public input must decode to one canonical BN254 field element, and there must be exactly final PI23 inputs. The full witness must decode exactly and its public part must equal those 23 inputs in order.

The successful response repeats the complete public binding:

```json
{
  "version": "v1",
  "circuit_set_id": "privacy-note-v1-u128-audit-field-v1",
  "circuit_id": "deposit-audit-field-u128-v1",
  "artifact_hash": "base64-encoded 32 bytes",
  "public_inputs": ["same 23 base64 values, in order"],
  "proof": "base64-encoded canonical BN254 Groth16 proof"
}
```

Clients must reject a response unless all repeated fields equal their prepared request and then perform local verification with the exact local artifact identity and final PI23 before constructing a V2 message. Framing equality alone is not proof verification. Prepared objects and witness bytes must be cleared after the attempt; do not persist or log either.

## Transport and errors

The route accepts `POST` JSON, supports `identity` or `gzip` body encoding, and applies the configured raw and decompressed body limit (8 MiB by default). When a bearer token is configured, every proof request must present it; an unset token disables this authentication check. Responses use `Content-Type: application/json` and `Cache-Control: no-store`; callers set a finite timeout and must not automatically fail over to another prover.

Errors use the strict `v1` envelope below. Unknown fields, duplicate fields, trailing JSON, an unsupported version, invalid base64/framing, a non-canonical field element, or witness/PI23 mismatch return `400 invalid_request`. A prover failure after a valid request returns `500 proof_failed`.

```json
{"version":"v1","code":"invalid_request","message":"audit-field proof request validation failed"}
```

## Legacy archive boundary

The former `/v1/prover/deposit`, `/v1/prover/transfer`, `/v1/prover/withdraw`, and `/v1/proofs/batch-transfer` contracts are not live V2 routes. Their NoteV1 and conformance fixtures remain retained historical evidence in `x/privacy/client/sdk/conformance/testdata`; they are not a fallback, auto-detection input, or a current integration target. The schema preserves those fixtures under an explicit legacy branch instead of rewriting them as V2 data.

## Deposit

The former deposit route is legacy-only. This compatibility anchor is retained for archived links; use the current audit-field route above and do not implement `/v1/prover/deposit` as a V2 fallback.

Validate the current and retained legacy fixture shapes with:

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -run '^TestProverHTTPSchemaContract$' -count=1
```
