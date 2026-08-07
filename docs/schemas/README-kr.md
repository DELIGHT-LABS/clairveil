# Clairveil JSON Schema

영문: [README.md](README.md)

이 machine-readable contract는 wallet-facing fixture와 canonical prover HTTP API를 설명하며 protocol verification을 대체하지 않습니다.

| Schema | 목적 |
| --- | --- |
| `clairveil-js-wallet-contract.schema.json` | `x/privacy/client/sdk/conformance/testdata`의 wallet conformance fixture shape |
| `clairveil-proverd-http-api.schema.json` | Prover route, envelope, error, policy, general/deposit fixture의 Draft 2020-12 shape |

Wallet fixture 검증:

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

Canonical HTTP와 deposit fixture 검증:

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -count=1
```

`make docs-check`는 third-party Python package 없이 repository-owned Draft 2020-12
validator로 positive fixture와 negative mutation을 검사합니다. Go conformance test는
fixture 값을 production constant와 semantic rule에 bind합니다.

Schema는 field, type, version, address/hash format, fixed array, cursor/version
field, amount/coin string, route/envelope/error 구조를 검사합니다. Payload/disclosure
digest 재계산, 복호화, Merkle path, cursor 전진, reservation 동작, proof는 검증하지
않으며 SDK와 Go test가 이를 강제합니다.

Route와 deposit semantic: [proverd HTTP API](../clairveil-proverd-http-api-kr.md#deposit).
NoteV1, batch, disclosure-blinding, fixed-payload normative vector는 `x/privacy/client/sdk/conformance/testdata`에 있으며, gate는 [테스트 가이드](../clairveil-testing-guide-kr.md)에서 선택합니다.
