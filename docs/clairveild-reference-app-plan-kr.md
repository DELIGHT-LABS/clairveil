# Clairveil Reference App 설계

## 목표

`clairveild`는 Clairveil standalone repo 안에서 `x/privacy`를 실제 체인에 얹어 검증하기 위한 최소 Cosmos SDK reference daemon이다.

목표는 downstream 프로젝트의 전체 앱을 대체하는 것이 아니라, 아래를 독립적으로 검증할 수 있게 하는 것이다.

- `clairveild init`
- `clairveild add-genesis-account`
- `clairveild gentx`
- `clairveild collect-gentxs`
- `clairveild start`
- `clairveild tx privacy ...`
- `clairveild query privacy ...`
- `make init`
- local shell walkthrough
- e2e fixture / tutorial 검증

## Reference App 범위

reference app은 아래 모듈만 포함한다.

| Module           | 이유                                                  |
| ---------------- | ----------------------------------------------------- |
| `auth`           | 계정, 서명, sequence, account number 처리             |
| `bank`           | deposit/withdraw의 transparent coin lock/release 처리 |
| `staking`        | local validator/gentx/start 기본 흐름                 |
| `slashing`       | validator lifecycle 기본 의존성                       |
| `distribution`   | staking app 기본 의존성                               |
| `gov`            | Cosmos SDK 기본 app 구성 호환성                       |
| `mint`           | staking app 기본 의존성                               |
| `params`         | legacy params subspace가 필요한 module 호환성         |
| `consensus`      | consensus params genesis/query                        |
| `genutil`        | init/gentx/collect-gentxs                             |
| `tx` / `auth tx` | sign/broadcast/encode/decode CLI                      |
| `x/privacy`      | Clairveil privacy module                              |

## 구현 상태

2026-05-04 기준 standalone repo에는 아래가 구현되어 있다.

| 항목                          | 상태                                                                                                                                       |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `app` package                 | `auth`, `bank`, `staking`, `slashing`, `distribution`, `gov`, `mint`, `params`, `consensus`, `genutil`, `vesting`, `x/privacy`를 포함한다. |
| `cmd/clairveild` root command | Cosmos SDK daemon command를 실행한다.                                                                                                      |
| Local genesis flow            | `init`, `keys add`, `add-genesis-account`, `gentx`, `collect-gentxs`, `validate`가 `uclair` 기준으로 통과한다.                             |
| Short start smoke             | 임시 home에서 `clairveild start`가 block 1 commit까지 진행된다.                                                                            |
| Smoke script                  | `make localnet-smoke` 또는 `./scripts/localnet-smoke.sh`로 local genesis/start smoke를 재현한다.                                           |
| Local init helper             | `make init`이 build/install, 기존 home 백업, local genesis, test key, audit master pubkey, ZK artifact를 준비한다.                         |
| Privacy walkthrough           | `docs/clairveil-local-privacy-walkthrough-kr.md`는 standalone `clairveild` 기준 full privacy flow를 다룬다.                                |
| Full privacy e2e smoke        | `make privacy-e2e-smoke`가 deposit, transfer, disclosure, direct withdraw, relayed withdraw를 로컬 노드에서 검증한다.                      |
| Release handoff               | `make release-pack`과 `make release-pack-verify`가 downstream handoff pack 생성과 검증을 담당한다.                                         |

주의: Cosmos SDK v0.54 계열 명령은 genesis 하위 command가 아니라 root-level `add-genesis-account`, `gentx`, `collect-gentxs`, `validate`를 사용한다.

## 구성 요소

### 1. app package

`app` 아래의 최소 Cosmos SDK app은 reference chain host 역할을 한다.

주요 파일:

- `app/app.go`
- `app/encoding.go`
- `app/genesis.go`
- `app/export.go`
- `app/modules.go`

완료 기준:

- `go test ./app/...` 통과
- `x/privacy` keeper가 bank keeper와 연결됨
- module account `privacy`가 bank module account로 등록됨
- `DefaultNodeHome`은 `~/.clairveil`

### 2. clairveild root command

`cmd/clairveild`는 Cosmos SDK root command를 실행한다.

주요 파일:

- `cmd/clairveild/main.go`
- `cmd/clairveild/cmd/root.go`

완료 기준:

- `clairveild --help` 출력
- `clairveild init --chain-id clairveil-local-1 test`
- `clairveild keys add alice --keyring-backend test`
- `clairveild tx privacy --help`
- `clairveild query privacy --help`

### 3. local node smoke test

`scripts/localnet-smoke.sh`는 임시 home 기준으로 local node를 띄운다.

완료 기준:

- `clairveild init`
- genesis account 추가
- gentx 생성
- collect-gentxs
- `clairveild start`
- `/status` 또는 `clairveild status` 확인

### 4. privacy walkthrough와 e2e

`docs/clairveil-local-privacy-walkthrough-kr.md`와 `scripts/privacy-e2e-smoke.sh`는 Clairveil standalone repo 기준 privacy flow를 검증한다.

완료 기준:

- `uclair` funding
- `clair1...` transparent address
- `clairs1...` shielded address
- deposit
- scan
- transfer
- decode disclosure
- direct withdraw
- prepare / relay withdraw
- public, recipient-encrypted, sender self-view, audit disclosure
