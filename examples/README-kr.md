# Clairveil Examples

이 디렉터리는 Go가 아닌 stack에서 Clairveil을 연동하는 팀을 위한 작은 reference consumer를 담습니다.

## 예제 목록

- `audit-disclosure-keys`: audit disclosure keypair를 파생하고 genesis에 넣을 수 있는 public key encoding을 출력하는 dependency-free Node 예제입니다.
- `js-sdk-fixture-validator`: Clairveil conformance fixture를 읽고 address prefix, payload hash, prover contract 기대값을 검증하는 dependency-free Node/TypeScript 예제입니다.
- `js-sdk-prover-http-client`: fixture-backed mock prover에 timeout-bound bearer-auth client로 prover HTTP contract를 호출하는 dependency-free Node/TypeScript 예제입니다.
- `clairveil-dapp`: MetaMask/Keplr 연결과 로컬 `clairveild` dev relay를 함께 써서 localnet query, faucet funding, Keplr direct-sign bank send, Keplr direct-sign privacy deposit, note scan을 브라우저에서 확인하는 DApp 예제입니다.

이 예제들은 production SDK 자체가 아니라, JS/TS SDK나 웹월렛 팀이 Clairveil contract를 어떤 순서로 검증해야 하는지 보여주는 기준점입니다.
