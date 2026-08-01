# Clairveil Proverd HTTP API

> Korean version: [clairveil-proverd-http-api-kr.md](clairveil-proverd-http-api-kr.md)

This document is the authoritative common HTTP contract for `clairveil-proverd` proof routes. Route-specific deposit request and response semantics are defined separately in the [Deposit Prover API](clairveil-proverd-deposit-api.md). The machine-readable contract is [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json), validated against the conformance fixtures under `x/privacy/client/sdk/conformance/testdata`.

## Route inventory

Every route accepts only `POST` and returns JSON.

| Route | Request envelope version | Response envelope version |
| --- | --- | --- |
| `/v1/prover/transfer` | `v2` | `v2` |
| `/v1/prover/withdraw` | `v2` | `v2` |
| `/v1/proofs/batch-transfer` | `v1` | `v1` |
| `/v1/prover/deposit` | `v1` | `v1` |

`/v1` is the route major version. Object versions inside an envelope are independently validated by the relevant route contract.

## Common transport policy

- `Content-Type` is required and must be `application/json`. Its only permitted parameter is `charset=utf-8`; media type and parameter comparisons are case-insensitive. The server checks it before reading the body or taking admission.
- `Accept` is optional. Version 1 does not negotiate content types; the server always returns JSON.
- Only `identity` and `gzip` `Content-Encoding` values are accepted.
- Both the raw wire body and decompressed body are subject to a configured positive limit. The service default is 8 MiB.
- Every proof-route success and error response, including early authentication and content-encoding failures, has `Content-Type: application/json` and `Cache-Control: no-store`.
- A method mismatch returns `405` with `Allow: POST`.
- An unknown path retains the common `404 not_found` response contract.

## Error contract

All errors use this strict JSON envelope. `message` is a fixed, failure-class-specific, secret-free message.

```json
{
  "version": "v1",
  "code": "invalid_request",
  "message": "proof request validation failed"
}
```

`retryable` is optional and defaults to `false` when omitted. It is required and `true` only for `busy`; it must not be `true` for another code. Decoders reject duplicate keys, unknown fields, and trailing JSON.

| HTTP status | Code | `retryable` | Meaning |
| ---: | --- | --- | --- |
| 400 | `invalid_request` | false | JSON, version, or semantic request validation failure; also unsupported `Content-Encoding` |
| 401 | `unauthorized` | false | Configured bearer token did not match |
| 404 | `not_found` | false | Unknown path |
| 405 | `method_not_allowed` | false | Method is not `POST` |
| 413 | `invalid_request` | false | Raw or decompressed body exceeds its limit |
| 415 | `invalid_request` | false | Missing or unsupported `Content-Type` |
| 429 | `busy` | true | Circuit admission queue is full |
| 500 | `proof_failed` | false | Proof runner or response self-validation failed after a valid request invoked proving |
| 503 | `unavailable` | false | Route prover is not configured |

The request-failure status is `400` and the post-validation prover-failure status is `500` for deposit, transfer, withdraw, and batch transfer. A proof runner error, nil response, or invalid response after `Prove*` is invoked is therefore never classified as a caller request error.

## Authentication, timeout, and privacy boundary

When `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN` is set, every proof route requires the matching Bearer credential. Do not put credentials in a URL query or userinfo. Non-loopback remote deployments use TLS.

Clients set a finite timeout. Context cancellation ends the caller wait but does not guarantee termination of an in-process proving solver. A caller may retry the same endpoint under its own policy; automatic multi-prover failover is disabled by default because it expands the witness-disclosure boundary. Browser cross-origin use belongs at a downstream gateway with an explicit CORS allowlist and authentication policy.

Proof-route logs, error messages, and metric labels must not contain request or response bodies, amounts, asset IDs, randomness, public keys, note commitments, proof bytes, bearer tokens, or witness/solver diagnostics.

## Versioning and compatibility

The route major version and each request, payload, response, and proof object version are separate compatibility layers. Adding, removing, or renaming a field, changing an encoding, or changing validation meaning requires a version bump for the affected object. Existing `v1` objects do not gain silent optional fields because decoding is strict. Unsupported versions fail closed as `400 invalid_request`; legacy auto-detection and fallback decoding are prohibited.

The common policy deliberately preserves existing success request/response envelope versions and `ErrorResponseVersion=v1`. It corrects the transport classification of existing transfer, withdraw, and batch-transfer post-validation failures from `400 proof_failed` to `500 proof_failed`. Clients must not infer retry safety from `proof_failed`; it remains non-retryable.

## Conformance validation

The dedicated schema owns the general HTTP fixture and the deposit fixture; it does not import a wallet or SDK schema. Validate both from the repository root with a Draft 2020-12 validator:

```bash
python3 -m jsonschema -i x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json docs/schemas/clairveil-proverd-http-api.schema.json
python3 -m jsonschema -i x/privacy/client/sdk/conformance/testdata/privacy_deposit_prover_contract.json docs/schemas/clairveil-proverd-http-api.schema.json
```
