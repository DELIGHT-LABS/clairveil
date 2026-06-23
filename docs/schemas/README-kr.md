# Clairveil JSON Schema

이 디렉터리는 Clairveil JS/TS SDK와 웹월렛 연동을 위한 machine-readable contract를 담습니다.

## Schema

- `clairveil-js-wallet-contract.schema.json`: `x/privacy/client/sdk/conformance/testdata` 아래 wallet-facing conformance fixture의 JSON Schema입니다.

## 사용 방법

외부 SDK는 live network integration을 시작하기 전에 CI에서 fixture를 검증하는 편이 좋습니다.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

repo의 예제 validator는 실행 부담을 줄이기 위해 dependency-free subset validator를 사용합니다. Production JS/TS SDK는 같은 schema 파일을 AJV 같은 full JSON Schema validator로 검증해도 됩니다.

## Schema가 다루는 것

- browser signer/root seed derivation fixture shape
- wallet readonly address, view key, disclosure, scan fixture
- prepared transfer prover payload shape와 sender self-view disclosure field
- prepared withdraw prover payload shape
- final prepared withdraw payload shape
- relay withdraw handoff request와 relayer `MsgWithdraw` mapping shape
- prover HTTP route, request, response, error contract shape
- send-capable reference flow fixture shape

이 schema는 field presence, basic type, version constant, address prefix, fixed-size hash, 현재 transfer payload array size, Merkle path helper bit, canonical non-negative uint64 amount string, Cosmos SDK coin string을 확인합니다.

단, semantic verification을 대신하지는 않습니다. payload hash 재계산, disclosure digest 검증, sender self-view payload 복호화/검증, Merkle path 재계산, proof verification은 SDK/test가 별도로 수행해야 합니다.
