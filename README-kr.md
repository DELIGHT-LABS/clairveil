# Clairveil

Clairveil은 Cosmos SDK 체인에 붙일 수 있는 auditable shielded privacy core입니다.

투명 계정에서 파생되는 shielded identity, shielded deposit, ZK 기반 transfer/withdraw, 사용자 선택 공개, 그리고 모든 transfer에 붙는 mandatory audit disclosure를 하나의 재사용 가능한 `x/privacy` 모듈로 묶습니다. 이 저장소는 production chain 전체가 아니라, privacy core를 독립적으로 개발하고 검증하기 위한 standalone reference host입니다.

## 무엇을 제공하나

- `x/privacy`: Cosmos SDK privacy module
- `clairveild`: privacy module을 실제 체인 위에서 검증하는 reference daemon
- `clairveil-setup`: Groth16 circuit artifact 생성 도구
- `clairveil-proverd`: local/remote companion prover reference service
- CLI, Go SDK helper, JS/web wallet conformance fixture
- local walkthrough, e2e smoke, release handoff pack

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

## 빠른 시작

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
make init
```

노드를 시작하려면 아래를 실행합니다.

```bash
source ~/.clairveil/clairveil.env
clairveild start
```

## 검증

코드와 예제 검증은 노드를 띄우지 않은 상태에서도 실행할 수 있습니다.

```bash
make ci
```

`make ci`는 `go test`, binary build, JS 예제 검증만 수행하며 `clairveild start` 노드에 붙지 않습니다.

로컬 노드에서 전체 privacy flow를 직접 따라가려면 [Local walkthrough](docs/clairveil-local-privacy-walkthrough-kr.md) 문서를 사용합니다.

자동 e2e smoke로 같은 흐름을 검증하려면 아래를 실행합니다. 이 target은 별도 임시 home을 만들고 local node를 직접 start합니다.

```bash
make privacy-e2e-smoke
```

> 이미 `clairveild start`로 기본 포트 `26657`, `26656`, `9090`, `1317`을 사용 중이라면 먼저 그 노드를 멈추거나 e2e port override를 사용하세요.

## 빌드

```bash
make build
```

생성되는 주요 binary는 아래입니다.

| Binary              | 역할                                  |
| ------------------- | ------------------------------------- |
| `clairveild`        | reference chain daemon                |
| `clairveil-setup`   | ZK artifact 생성                      |
| `clairveil-verify`  | legacy/debug note verification helper |
| `clairveil-proverd` | companion prover HTTP service         |

개별 binary를 직접 빌드할 수도 있습니다.

```bash
go build ./cmd/clairveild
go build ./cmd/clairveil-setup
go build ./cmd/clairveil-verify
go build ./cmd/clairveil-proverd
```

빌드한 binary를 Go install 경로로 복사하려면 아래를 실행합니다.

```bash
make install
```

`make install`은 `go env GOBIN`이 있으면 그 값을 사용하고, 비어 있으면 `$(go env GOPATH)/bin`을 사용합니다.

## 로컬 체인 초기화

기본 홈 `~/.clairveil`을 새 local chain으로 초기화하려면 아래를 실행합니다.

```bash
make init
```

동작:

- `make install`을 먼저 실행합니다.
- 기존 `~/.clairveil`이 있으면 `~/.clairveil.backup-YYYYMMDD-HHMMSS` 형식으로 백업합니다.
- `clairveild init`, `keys add`, `add-genesis-account`, `gentx`, `collect-gentxs`, `validate`를 수행합니다.
- `alice`, `bob`, `relayer`, `auditor` test key를 만들고, `auditor` disclosure pubkey를 genesis audit master key로 설정합니다.
- ZK artifact를 `~/.clairveil/artifacts/privacy`에 생성하고 `~/.clairveil/clairveil.env`를 만듭니다.

시작:

```bash
source ~/.clairveil/clairveil.env
clairveild start
```

주요 override:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home make init
CHAIN_ID=my-local-chain make init
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" make init
```

## 테스트

일반적인 전체 검증은 아래 하나로 충분합니다.

```bash
make ci
```

`make ci`는 실행 중인 local node가 필요하지 않습니다.

필요한 경우 개별 검증을 따로 실행할 수 있습니다.

```bash
make test
make localnet-smoke
make privacy-e2e-smoke
```

`make localnet-smoke`와 `make privacy-e2e-smoke`는 검증용 local node를 직접 띄웁니다. 이미 기본 포트를 쓰는 노드가 있으면 smoke test와 충돌할 수 있습니다.

릴리즈 후보 수준의 검증은 아래 명령을 사용합니다.

```bash
make release-check
make release-pack
make release-pack-verify
```

테스트 레이어와 각 명령의 의미는 [테스트 가이드](docs/clairveil-testing-guide-kr.md)에 정리되어 있습니다.

## 가져다 쓰는 방법

초기 통합 중에는 downstream app에서 로컬 `replace`를 쓰는 방식이 가장 빠릅니다.

```go
require github.com/DELIGHT-LABS/clairveil v0.0.0

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

릴리즈 태그를 사용하기 시작하면 특정 tag 또는 commit으로 고정합니다.

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

downstream Cosmos SDK app은 `x/privacy`, proto, keeper wiring, module account, genesis audit key, CLI/API route를 자기 app에 연결해야 합니다. 자세한 절차는 [Downstream 통합 가이드](docs/clairveil-downstream-cosmos-integration-guide-kr.md)를 기준으로 보면 됩니다.

## CLI 개요

대표적인 privacy CLI는 아래입니다.

```bash
clairveild tx privacy show-address --from alice --keyring-backend test --output json
clairveild tx privacy deposit 10uclair --from alice --keyring-backend test
clairveild tx privacy transfer <clairs1...> 7uclair --from alice --keyring-backend test
clairveild tx privacy list-notes --from alice --keyring-backend test --json
clairveild tx privacy withdraw 7uclair --from alice --keyring-backend test
```

명령별 목적, 주요 flag, 출력 형태는 [CLI 기능 문서](docs/clairveil-cli-reference-kr.md)에 정리되어 있습니다.

## 문서 지도

| 문서                                                                               | 역할                                                              |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| [Reference app](docs/clairveild-reference-app-plan-kr.md)                          | `clairveild` reference host의 설계 의도와 현재 상태               |
| [Local walkthrough](docs/clairveil-local-privacy-walkthrough-kr.md)                | 로컬 노드에서 deposit, transfer, disclosure, withdraw를 직접 실행 |
| [Circuit guide](docs/clairveil-circuits-kr.md)                                     | Spend/JoinSplit 회로가 증명하는 것과 증명하지 않는 것             |
| [CLI reference](docs/clairveil-cli-reference-kr.md)                                | `clairveild tx/query privacy` 명령별 사용법                       |
| [Testing guide](docs/clairveil-testing-guide-kr.md)                                | unit, e2e, conformance, release 검증 방법                         |
| [Operations guide](docs/clairveil-operations-guide-kr.md)                          | node/prover/artifact/Merkle/audit 운영 기준                       |
| [Maintainer instructions](docs/clairveil-maintainer-instructions-kr.md)            | 문서, 회로, proto, fixture, release 변경 시 유지보수 규칙         |
| [Downstream integration](docs/clairveil-downstream-cosmos-integration-guide-kr.md) | Cosmos SDK app에 `x/privacy`를 붙이는 방법                        |
| [JS SDK handoff](docs/clairveil-js-sdk-handoff-kr.md)                              | JS/TS SDK와 웹월렛 구현 계약                                      |
| [Prover profile](docs/clairveil-proverd-remote-production-profile-kr.md)           | `clairveil-proverd` remote 운영 profile                           |
| [Merkle restore SOP](docs/clairveil-merkle-restore-sop-kr.md)                      | snapshot/restore/migration 후 tree 검증 절차                      |
| [Threat model](docs/clairveil-threat-model-kr.md)                                  | trust boundary, assets, residual risk                             |
| [Security review](docs/clairveil-security-best-practices-review-kr.md)             | production 전 보안 체크포인트                                     |
| [Release handoff](docs/clairveil-release-handoff-pack-kr.md)                       | downstream 팀에게 넘길 산출물과 검증 절차                         |

## 보안

취약점이 의심되면 public issue에 세부 내용을 올리지 말고 [SECURITY.md](SECURITY.md)를 따라 private vulnerability report를 보내주세요.

Clairveil은 privacy-sensitive software입니다. production deployment 전에는 최소한 audit key custody, wallet storage encryption, remote prover policy, ZK artifact provenance, chain-specific threat model을 downstream project가 별도로 완료해야 합니다.

## 라이선스

Clairveil은 Apache License 2.0으로 배포됩니다. 자세한 내용은 [LICENSE](LICENSE)와 [NOTICE](NOTICE)를 확인하세요.
