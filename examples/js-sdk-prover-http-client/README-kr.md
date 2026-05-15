# JS SDK Prover HTTP Client 예제

이 예제는 JS/TS wallet SDK가 명시적인 timeout과 bearer token으로 Clairveil companion prover contract를 호출하는 방식을 보여줍니다. repository fixture를 사용해 in-process mock prover를 띄우므로 live `clairveil-proverd` 없이 실행할 수 있습니다.

목표는 full wallet SDK를 구현하는 것이 아닙니다. 안전한 client shape를 명확하게 보여주는 것이 목표입니다.

- `AbortController` timeout과 함께 `fetch`를 사용합니다.
- prover가 auth를 요구하면 `Authorization: Bearer ...`를 보냅니다.
- request/response version을 검증합니다.
- proof `payload_hash`를 prepared payload와 비교해 검증합니다.
- bearer token 또는 full prepared payload를 log에 남기지 않습니다.

## 실행

이 디렉터리에서 실행합니다.

```bash
npm run demo
```

repository root에서 실행합니다.

```bash
npm --prefix examples/js-sdk-prover-http-client run demo
```

이 예제는 Node의 내장 TypeScript type stripping을 사용하므로 Node 22 이상이 필요하며 npm dependency는 없습니다.

## 실제 JS SDK로 가져갈 것

production JS SDK는 mock server가 아니라 client boundary를 자기 구조에 맞게 적용해야 합니다.

- local, remote, browser/WASM prover adapter 뒤에 하나의 `ProverClient` interface를 둡니다.
- remote proof request에는 유한한 timeout을 반드시 요구합니다.
- transaction을 만들기 전에 response version과 proof payload hash를 검증합니다.
- remote prover payload를 privacy-sensitive data로 취급합니다.
- remote prover token을 log, source code, browser bundle constant, public telemetry에 남기지 않습니다.
