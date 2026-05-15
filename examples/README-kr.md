# Clairveil Examples

이 디렉터리는 Go가 아닌 stack에서 Clairveil을 연동하는 팀을 위한 작은 reference consumer를 담습니다.

## 예제 목록

- `js-sdk-fixture-validator`: Clairveil conformance fixture를 읽고 address prefix, payload hash, prover contract 기대값을 검증하는 dependency-free Node/TypeScript 예제입니다.
- `js-sdk-prover-http-client`: fixture-backed mock prover에 timeout-bound bearer-auth client로 prover HTTP contract를 호출하는 dependency-free Node/TypeScript 예제입니다.

이 예제들은 production SDK 자체가 아니라, JS/TS SDK나 웹월렛 팀이 Clairveil contract를 어떤 순서로 검증해야 하는지 보여주는 기준점입니다.
