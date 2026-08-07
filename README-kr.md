# Clairveil

Clairveil은 Cosmos SDK 체인에 붙일 수 있는 auditable shielded privacy core입니다.

투명 계정에서 파생되는 shielded identity, shielded deposit, ZK 기반 transfer/withdraw, 사용자 선택 공개, 그리고 모든 transfer에 붙는 mandatory audit disclosure를 하나의 재사용 가능한 `x/privacy` 모듈로 묶습니다. 이 저장소는 production chain 전체가 아니라, privacy core를 독립적으로 개발하고 검증하기 위한 standalone reference host입니다.

English version: [README.md](README.md)

## 무엇을 제공하나

- `x/privacy`: Cosmos SDK privacy module
- `clairveild`: privacy module을 실제 체인 위에서 검증하는 reference daemon
- `clairveil-setup`: Groth16 circuit artifact 생성 도구
- `clairveil-proverd`: local/remote companion prover reference service
- `clairveil-payroll`, `clairveil-payrolld`: reference payroll control-plane CLI와 daemon
- CLI, Go SDK helper, JS/web wallet conformance fixture
- local walkthrough, e2e smoke, reference payroll rehearsal, release handoff pack

> Clairveil은 downstream production app을 대신하지 않습니다. 관련 module, validator 운영, audit key custody, wallet storage encryption, artifact signing 등은 Clairveil을 가져다 쓰는 프로젝트가 자기 환경에 맞게 결정해야 합니다.

## 현재 reference chain

| 항목                   | 값                                  |
| ---------------------- | ----------------------------------- |
| Go module              | `github.com/DELIGHT-LABS/clairveil` |
| Daemon                 | `clairveild`                        |
| Transparent prefix     | `clair`                             |
| Shielded prefix        | `clairs`                            |
| Reference denom        | `uclair`                            |
| Proto package          | `clairveil.privacy.v1`              |
| Default local chain-id | `clairveil-local-1`                 |

## 현재 상태와 호환성

| 항목 | 현재 기준 |
| --- | --- |
| 공개 상태 | `PUBLICATION_READY_EXPERIMENTAL`; source/reference 공개 가능 상태이며 production 배포 승인이 아님 |
| Consensus circuit set | state version 2의 `privacy-note-v1` |
| Fixed client contract | `privacy-fixed-v1`; transfer payload `v5`, proof/prover contract `v2` |
| Batch surface | `BatchJoinSplit16x32`, `MsgBatchTransfer`; batch integration용 Go SDK/prover/scanner/payroll/CLI reference 구현 |
| Upgrade 경계 | 이전 artifact, proof job, note/scan cache, three-circuit genesis와 호환되지 않음. fresh genesis/reset 및 rescan 필요 |
| 남은 production gate | formal trusted setup, 외부 security/circuit audit, signed production artifact, downstream chain/product 검증 |

문서는 같은 checkout의 코드를 설명합니다. Tag 또는 commit을 통합할 때는 반드시 그 exact ref의 문서를 읽고 release manifest를 검증해야 하며, 이전 binary/tag와 `HEAD` 문서를 섞으면 안 됩니다.

## 빠른 시작

Git, Make, Go `1.25.12`, Python `3.9+`, Bash가 필요하며 repository CI/example 검증에는 Node.js `22+`와 npm도 필요합니다. 전체 circuit set을 생성하기 전에 [시작 가이드](docs/clairveil-getting-started-kr.md)의 리소스 요구량을 확인합니다.

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
make init
source ~/.clairveil/clairveil.env
clairveild start
```

[시작 가이드](docs/clairveil-getting-started-kr.md)에서 설정, 수동 deposit/transfer/disclosure/withdraw, one-proof batch 실행, 정리까지 이어서 진행합니다.

## 검증

```bash
make ci
```

실행 중인 node 없이 문서 검사, Go test, binary build, JS example 검증을 수행합니다. `make privacy-e2e-smoke`는 별도 임시 local chain을 시작해 전체 privacy flow를 검증합니다. 포트 변경, live batch gate, release/capacity 증거는 [테스트 가이드](docs/clairveil-testing-guide-kr.md)를 확인합니다.

## 통합과 문서

Architecture, protocol, CLI, SDK/prover 계약, 운영·보안 참조는 [문서 index](docs/README-kr.md)에서 찾습니다.

- Cosmos app 통합: [downstream 가이드](docs/clairveil-downstream-cosmos-integration-guide-kr.md).
- Reference payroll 계약과 demo: [payroll 예제](examples/reference-payroll/README-kr.md).
- 기여·릴리스 규칙: [CONTRIBUTING-kr.md](CONTRIBUTING-kr.md).

## 보안

취약점이 의심되면 public issue에 세부 내용을 올리지 말고 [SECURITY.md](SECURITY.md)를 따라 private vulnerability report를 보내주세요.

Clairveil은 privacy-sensitive software입니다. production deployment 전에는 최소한 audit key custody, wallet storage encryption, remote prover policy, ZK artifact provenance, chain-specific threat model을 downstream project가 별도로 완료해야 합니다.

## 라이선스

Clairveil은 Apache License 2.0으로 배포됩니다. 자세한 내용은 [LICENSE](LICENSE)와 [NOTICE](NOTICE)를 확인하세요.
