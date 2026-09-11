# Clairveil Proverd HTTP API

> English version: [clairveil-proverd-http-api.md](clairveil-proverd-http-api.md)

이 문서는 이 checkout에서 실행 중인 `clairveil-proverd`의 현재 공통 HTTP 계약입니다. 기계 판독 계약은 [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json)이고, `privacy_audit_field_prover_contract.json`은 wire-shape-only mock fixture입니다. semantic proving vector가 아니며 `ValidateAuditFieldProofRequest`나 response validation을 통과한다고 주장하지 않습니다.

## 현재 route

| Route | 요청 envelope `version` | 응답 envelope `version` |
| --- | --- | --- |
| `POST /v2/prover/audit-field` | `v1` | `v1` |

`/v2`는 HTTP route major version입니다. JSON envelope version과 독립적이므로 현재 request/response struct는 모두 문자열 `v1`을 요구합니다.

V2 route는 development-only입니다. `clairveil-proverd`는 일치하는 `privacy-note-v1-audit-field-v1` artifact directory와 `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR` 없이는 시작하지 않습니다. 전체 witness에 transaction secret이 있으므로 명시적으로 신뢰한 local prover만 사용하세요.

## 요청·응답 binding

모든 요청은 아래 field만 가진 strict JSON object입니다.

```json
{
  "version": "v1",
  "circuit_set_id": "privacy-note-v1-audit-field-v1",
  "circuit_id": "deposit-audit-field-v1",
  "artifact_hash": "base64-encoded 32 bytes",
  "public_inputs": ["base64-encoded canonical field element", "... exactly 23 entries"],
  "witness": "base64-encoded complete gnark witness"
}
```

`circuit_id`는 `deposit-audit-field-v1`, `spend-audit-field-v1`, `joinsplit-2x2-audit-field-v1`, `batch-joinsplit-16x32-audit-field-v1` 중 하나여야 합니다. Go 표준 JSON encoding에서 모든 `[]byte`는 base64 JSON string입니다. 따라서 `public_inputs`는 hexadecimal이 아니라 base64 string 배열입니다. 각 public input은 canonical BN254 field element로 decode되어야 하고 final PI23은 정확히 23개여야 합니다. Full witness도 완전히 decode되어야 하며 public part가 같은 순서의 23개 input과 일치해야 합니다.

성공 응답은 전체 public binding을 반복합니다.

```json
{
  "version": "v1",
  "circuit_set_id": "privacy-note-v1-audit-field-v1",
  "circuit_id": "deposit-audit-field-v1",
  "artifact_hash": "base64-encoded 32 bytes",
  "public_inputs": ["same 23 base64 values, in order"],
  "proof": "base64-encoded canonical BN254 Groth16 proof"
}
```

Client는 반복된 field가 prepared request와 모두 같은지 거부-우선으로 확인하고, V2 message를 만들기 전에 exact local artifact identity와 final PI23으로 local verification을 수행해야 합니다. Framing equality만으로 proof verification이 되지는 않습니다. Prepared object와 witness byte는 시도 직후 지우며 저장하거나 log에 남기지 않습니다.

## 전송과 error

Route는 `POST` JSON만 받고 `identity`/`gzip` body encoding을 지원하며 raw/decompressed body limit(기본 8 MiB)을 적용합니다. Bearer token이 설정되면 모든 proof route에 필요합니다. 응답에는 `Content-Type: application/json`, `Cache-Control: no-store`가 붙고 caller는 finite timeout을 설정하며 다른 prover로 자동 failover하면 안 됩니다.

Error는 아래 strict `v1` envelope입니다. Unknown/duplicate/trailing JSON field, unsupported version, invalid base64/framing, non-canonical field element, witness/PI23 mismatch는 `400 invalid_request`입니다. Valid request 뒤 prover failure는 `500 proof_failed`입니다.

```json
{"version":"v1","code":"invalid_request","message":"audit-field proof request validation failed"}
```

## Legacy archive 경계

기존 `/v1/prover/deposit`, `/v1/prover/transfer`, `/v1/prover/withdraw`, `/v1/proofs/batch-transfer` 계약은 live V2 route가 아닙니다. 해당 NoteV1/conformance fixture는 `x/privacy/client/sdk/conformance/testdata`에 보존된 역사 자료이며 fallback, auto-detection input, 현재 integration target이 아닙니다. Schema도 fixture 값을 V2로 바꾸지 않고 명시적인 legacy branch로 보존합니다.

## Deposit

이전 deposit route는 legacy-only입니다. Archive link 호환을 위해 이 anchor를 유지하며 `/v1/prover/deposit`을 V2 fallback으로 구현하지 말고 위의 현재 audit-field route를 사용하세요.

현재 및 보존 legacy fixture shape 검증:

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -run '^TestProverHTTPSchemaContract$' -count=1
```
