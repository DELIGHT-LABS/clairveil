# JS SDK Prover HTTP Client 예제

이 작은 JS/TS 예제는 유한 timeout과 bearer token으로 현재 Clairveil audit-field V2 prover 경계를 호출합니다. in-process mock을 사용하므로 live clairveil-proverd가 필요 없습니다.

이는 V2 wire/binding mock입니다. POST /v2/prover/audit-field, envelope v1, circuit set privacy-note-v1-audit-field-v1, base64 byte field, 순서가 고정된 PI23 binding을 사용합니다. Complete gnark witness를 만들거나 Groth16 proof를 검증하지 않습니다. Live response를 소비하기 전 Go ValidateAuditFieldProofResponse 같은 기존 exact-artifact verifier를 사용해야 합니다.

Client는 전송 전 framing을 검증하고 loopback에서만 HTTP를 허용하며 redirect를 거절합니다. AbortController로 요청 시간을 제한하고 오류에 서버 response body 전체를 포함하지 않습니다. 정상 응답과 반복 binding field 위조 거절을 모두 보여줍니다.

## 실행

~~~
npm --prefix examples/js-sdk-prover-http-client run demo
~~~

실제 wallet에서는 transport boundary만 가져가고 witness는 local에서 만들며, witness와 bearer credential을 log에 남기지 마세요. Transaction을 만들기 전 local PI23으로 Go/WASM exact-artifact verifier를 호출해야 합니다.
