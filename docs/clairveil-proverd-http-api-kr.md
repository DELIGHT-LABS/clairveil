# Clairveil Proverd HTTP API

> English version: [clairveil-proverd-http-api.md](clairveil-proverd-http-api.md)

이 문서는 `clairveil-proverd` proof route의 authoritative한 공통 HTTP 계약입니다. Deposit의 route별 요청·응답 의미는 [Deposit Prover API](clairveil-proverd-deposit-api-kr.md)에서 정의합니다. 기계 판독 계약은 [clairveil-proverd-http-api.schema.json](schemas/clairveil-proverd-http-api.schema.json)이며, `x/privacy/client/sdk/conformance/testdata`의 conformance fixture로 검증합니다.

## Route inventory

모든 route는 `POST`만 받고 JSON을 반환합니다.

| Route | Request envelope version | Response envelope version |
| --- | --- | --- |
| `/v1/prover/transfer` | `v2` | `v2` |
| `/v1/prover/withdraw` | `v2` | `v2` |
| `/v1/proofs/batch-transfer` | `v1` | `v1` |
| `/v1/prover/deposit` | `v1` | `v1` |

`/v1`은 route major version입니다. Envelope 안 object의 version은 각 route 계약에서 독립적으로 검증합니다.

## 공통 전송 정책

- `Content-Type`은 필수이고 `application/json`이어야 합니다. 허용하는 유일한 parameter는 `charset=utf-8`이며, media type과 parameter 비교는 case-insensitive입니다. Server는 body를 읽거나 admission을 얻기 전에 이를 검사합니다.
- `Accept`는 선택 사항입니다. Version 1은 content negotiation을 하지 않으며 server는 항상 JSON을 반환합니다.
- `Content-Encoding`은 `identity`와 `gzip`만 허용합니다.
- Raw wire body와 decompressed body 모두 configured positive limit를 적용합니다. Service default는 8 MiB입니다.
- Auth/content-encoding의 조기 실패를 포함한 모든 proof-route success/error response는 `Content-Type: application/json`과 `Cache-Control: no-store`를 가집니다.
- Method mismatch는 `Allow: POST`를 포함한 `405`를 반환합니다.
- Unknown path는 기존 공통 `404 not_found` response 계약을 유지합니다.

## Error 계약

모든 error는 다음 strict JSON envelope를 사용합니다. `message`는 failure class별 고정된 secret-free 문구입니다.

```json
{
  "version": "v1",
  "code": "invalid_request",
  "message": "proof request validation failed"
}
```

`retryable`은 선택 사항이며 생략하면 `false`입니다. `busy`에서만 필수로 `true`여야 하고, 다른 code에서 `true`이면 안 됩니다. Decoder는 duplicate key, unknown field, trailing JSON을 거부합니다.

| HTTP status | Code | `retryable` | 의미 |
| ---: | --- | --- | --- |
| 400 | `invalid_request` | false | JSON, version, semantic request validation 실패 및 지원하지 않는 `Content-Encoding` |
| 401 | `unauthorized` | false | Configured bearer token 불일치 |
| 404 | `not_found` | false | Unknown path |
| 405 | `method_not_allowed` | false | `POST` 이외 method |
| 413 | `invalid_request` | false | Raw 또는 decompressed body가 limit 초과 |
| 415 | `invalid_request` | false | `Content-Type` 누락 또는 미지원 |
| 429 | `busy` | true | Circuit admission queue 포화 |
| 500 | `proof_failed` | false | Valid request가 proving을 호출한 뒤 proof runner 또는 response self-validation 실패 |
| 503 | `unavailable` | false | Route prover 미구성 |

Deposit, transfer, withdraw, batch transfer 모두 request failure status는 `400`, post-validation prover failure status는 `500`입니다. 따라서 `Prove*` 호출 뒤의 proof runner error, nil response, invalid response는 caller request error가 아닙니다.

## Authentication, timeout, privacy 경계

`CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`이 설정되면 모든 proof route는 일치하는 Bearer credential을 요구합니다. Credential을 URL query나 userinfo에 넣지 마십시오. Non-loopback remote deployment는 TLS를 사용합니다.

Client는 finite timeout을 설정합니다. Context cancellation은 caller wait를 끝내지만 in-process proving solver의 종료를 보장하지 않습니다. Caller는 같은 endpoint를 자체 정책으로 재시도할 수 있지만, witness disclosure 경계를 넓히므로 automatic multi-prover failover는 기본적으로 사용하지 않습니다. Browser cross-origin 사용은 명시적 CORS allowlist와 auth policy가 있는 downstream gateway의 책임입니다.

Proof-route log, error message, metric label에는 request/response body, amount, asset ID, randomness, public key, note commitment, proof bytes, bearer token, witness/solver diagnostic을 넣으면 안 됩니다.

## Versioning과 compatibility

Route major version과 각 request, payload, response, proof object version은 분리된 compatibility layer입니다. Field 추가·삭제·rename, encoding 변경, validation 의미 변경은 영향받는 object의 version bump가 필요합니다. Strict decoding 때문에 기존 `v1` object에 silent optional field를 추가하지 않습니다. Unsupported version은 `400 invalid_request`로 fail closed하며 legacy auto-detection과 fallback decoding은 금지합니다.

공통 정책은 기존 success request/response envelope version과 `ErrorResponseVersion=v1`을 유지합니다. 다만 기존 transfer, withdraw, batch-transfer의 post-validation failure는 `400 proof_failed`에서 `500 proof_failed`로 전송 분류가 교정됩니다. Client는 `proof_failed`에서 재시도 안전성을 추론하면 안 되며, 이는 계속 non-retryable입니다.

## Conformance 검증

전용 schema가 general HTTP fixture와 deposit fixture를 소유하며 wallet 또는 SDK schema를 import하지 않습니다. Repository root에서 repository의 focused Draft 2020-12 conformance gate로 둘 다 검증합니다.

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -run '^TestProverHTTPSchemaContract$' -count=1
```

이 gate는 canonical schema를 compile하고 두 canonical fixture를 accept하며 대표적인 unknown-field 및 범위 초과 amount mutation을 reject합니다. 필수 Go toolchain을 사용하므로 third-party Python package가 필요하지 않습니다. Downstream 구현은 같은 language-neutral schema에 대해 호환되는 어떤 Draft 2020-12 validator든 사용할 수 있습니다.
