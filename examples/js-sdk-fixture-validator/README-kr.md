# JS SDK Fixture Validator 예제

이 예제는 JS/TS SDK 개발자를 위한 작은 reference consumer입니다. live node에 연결하지 않고 proof도 만들지 않습니다. 대신 Clairveil conformance fixture를 읽고, wallet SDK가 먼저 맞춰야 하는 계약을 검증합니다.

- Clairveil account address는 `clair1...` prefix를 사용합니다.
- Clairveil shielded address는 `clairs1...` prefix를 사용합니다.
- transfer prepared payload hash가 Go SDK hash contract와 일치해야 합니다.
- withdraw prover payload hash가 Go SDK hash contract와 일치해야 합니다.
- 최종 relayed withdraw payload hash가 Go SDK hash contract와 일치해야 합니다.
- relay withdraw handoff에서 relayer address가 `MsgWithdraw.creator`가 되고 user recipient는 payload의 `recipient`로 유지되어야 합니다.
- companion prover request/response version과 path가 안정적으로 유지되어야 합니다.
- wallet-facing fixture가 `docs/schemas/clairveil-js-wallet-contract.schema.json`와 일치해야 합니다.

## 실행

이 디렉터리에서 실행합니다.

```bash
npm run validate
```

repository root에서 실행합니다.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

이 예제는 Node의 내장 TypeScript type stripping을 사용하므로 Node 22 이상이 필요하며 npm dependency는 없습니다.
Clairveil local fixture contract를 위한 작은 JSON Schema subset validator가 포함되어 있습니다. production JS/TS SDK는 같은 schema 파일에 대해 AJV 같은 full JSON Schema validator를 사용할 수 있습니다.

## 실제 JS SDK로 가져갈 것

이 예제는 의도적으로 범위가 좁습니다. production JS SDK는 파일 구조를 그대로 복사하기보다 같은 구현 검증 항목을 재사용하는 편이 좋습니다.

- 동일한 prepared payload hash function을 구현합니다.
- 동일한 fixture 파일을 CI에서 로드합니다.
- fixture shape를 `docs/schemas/clairveil-js-wallet-contract.schema.json`로 검증합니다.
- relay withdraw handoff fixture로 final payload에서 relayer 제출 메시지로 변환되는 필드를 검증합니다.
- generated proto/type이 `clairveil.privacy.v1`과 일치하는지 검증합니다.
- prover transport request/response version을 fixture 값에 고정합니다.
- wallet-facing fixture에 `clair1...` 또는 `clairs1...`가 아닌 address prefix가 있으면 빠르게 실패합니다.
