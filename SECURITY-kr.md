# 보안 정책

Clairveil은 reusable privacy-core 저장소입니다. 의심되는 취약점은 public issue로 보고하지 마세요.

## 지원 버전

첫 public release tag 전까지 보안 수정은 `main` branch를 대상으로 합니다.

Versioned release가 시작되면 지원 release line을 이 문서에 업데이트합니다.

## 취약점 보고

GitHub private vulnerability reporting으로 private report를 보내주세요.

```text
https://github.com/DELIGHT-LABS/clairveil/security/advisories/new
```

Private advisory form을 사용할 수 없다면 exploit detail을 public issue에 올리지 마세요. 먼저 private channel로 maintainer에게 연락하고 coordinated disclosure 절차를 기다려주세요.

가능하면 아래 정보를 포함합니다.

- 영향받는 commit, tag, branch
- 영향받는 component: 예를 들어 `x/privacy`, `clairveil-proverd`, ZK artifact tooling, CLI, fixture/schema, Docker packaging
- 재현 절차 또는 proof of concept
- 예상 impact
- 문제가 downstream production deployment에만 영향을 주는지, standalone reference repo에만 영향을 주는지, 둘 다인지

## 범위

In scope:

- `x/privacy`의 consensus 또는 state-machine safety 문제
- proof verification, payload binding, disclosure, nullifier correctness 문제
- Clairveil code로 인한 private key, viewing key, disclosure key, wallet note, prepared payload, prover metadata 노출
- remote prover authentication, timeout, body-limit, response-binding 문제
- release artifact, Docker packaging, schema, conformance fixture의 supply-chain 문제

이 standalone repository의 out of scope:

- downstream validator 운영
- downstream EVM, policy module, precompile, wasm, IBC integration
- downstream audit private key custody
- production web wallet encrypted storage 선택
- 이 repo에 구현되지 않은 production artifact signing 또는 ceremony policy

Out of scope 영역도 production에서는 중요한 위험입니다. Downstream project의 threat model과 release checklist에서 별도로 다뤄야 합니다.

## 기본 보안 검증

Maintainer는 아래를 실행해야 합니다.

```bash
make vulncheck
make release-check
```

`make vulncheck`는 reference app의 upstream dependency path에 fixed version이 없는 동안 `GO-2024-2584`와 `GO-2026-4479`의 `pion/dtls` v2 경로에 대한 documented policy exception을 포함합니다. Downstream production project는 이 예외들을 자기 risk register에서 다시 평가해야 합니다.
