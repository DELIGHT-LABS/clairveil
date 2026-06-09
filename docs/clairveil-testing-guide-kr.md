# Clairveil 테스트 가이드

이 문서는 Clairveil의 테스트 레이어와 각 명령이 보장하는 범위를 정리합니다.

## 1. 빠른 검증

일반 PR에서 기본으로 보는 검증은 아래입니다.

```bash
make ci
make vulncheck
```

`make ci`와 `make vulncheck`는 실행 중인 `clairveild` 노드를 필요로 하지 않습니다. `make ci`는 Go test, Go binary build, JS 예제 검증만 수행합니다.

release 후보나 큰 변경은 아래까지 실행합니다.

```bash
make release-check
make release-pack
make release-pack-verify
```

## 2. Make target

| 명령 | 의미 |
| --- | --- |
| `make test` | `go test ./...` 실행 |
| `make build` | `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd` build |
| `make install` | `make build` 후 Clairveil binary를 `GOBIN` 또는 `GOPATH/bin`으로 복사 |
| `make init` | `make install` 후 기본 local chain home을 초기화해 `clairveild start` 준비 |
| `make proto` | privacy protobuf/gateway Go file 재생성 |
| `make examples` | JS audit key, fixture validator, prover HTTP client 예제 실행 |
| `make ci` | `test`, `build`, `examples` 묶음 |
| `make vulncheck` | govulncheck policy gate 실행 |
| `make localnet-smoke` | reference daemon이 genesis부터 start 가능한지 짧게 검증 |
| `make privacy-e2e-smoke` | deposit, transfer, disclosure, withdraw 전체 flow 검증 |
| `make release-check` | `ci`, `vulncheck`, `localnet-smoke`, `privacy-e2e-smoke` 묶음 |
| `make release-pack` | downstream handoff archive와 sha256 생성 |
| `make release-pack-verify` | handoff archive checksum, 내부 checksum, 필수 파일, manifest commit 검증 |
| `make docker-proverd-build` | prover Dockerfile/compose build 검증 |

## 3. Go unit/integration test

```bash
make test
```

주요 범위:

| Package | 검증 내용 |
| --- | --- |
| `x/privacy/circuit` | Deposit/Spend/JoinSplit circuit constraint |
| `x/privacy/keeper` | deposit/transfer/withdraw state transition, Merkle capacity, query error handling |
| `x/privacy/types` | Msg validation, address, gateway path |
| `x/privacy/client/cli` | CLI parsing, output, disclosure decode helper |
| `x/privacy/client/sdk/*` | identity, deposit, scan, transfer, withdraw, disclosure, prover transport |
| `x/privacy/client/sdk/conformance` | JS/web wallet fixture contract |
| `x/privacy/zk` | artifact manifest/checksum loading |

특정 package만 볼 때:

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
```

## 4. JS/web wallet fixture 검증

```bash
make examples
```

내부적으로 아래가 실행됩니다.

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/js-sdk-fixture-validator run validate
npm --prefix examples/js-sdk-prover-http-client run demo
```

검증 범위:

- audit disclosure key derivation vector와 genesis public key encoding
- fixture address prefix
- prepared transfer payload hash
- prepared withdraw payload hash
- relayed withdraw final payload hash
- prover HTTP request/response version
- timeout/auth client shape

## 5. Localnet smoke

```bash
make localnet-smoke
```

이 target은 임시 home을 만들고 검증용 `clairveild start`를 직접 실행합니다. 이미 기본 Tendermint/RPC 포트를 쓰는 local node가 떠 있으면 충돌할 수 있습니다.

검증 범위:

1. `clairveild` build
2. temporary home 생성
3. `init`
4. key 생성
5. genesis account 추가
6. gentx / collect-gentxs / validate
7. node start
8. block commit log 확인

유용한 환경변수:

| env | 의미 |
| --- | --- |
| `CLAIRVEIL_HOME` | smoke에 사용할 home 고정 |
| `KEEP_HOME=1` | 종료 후 home 삭제하지 않음 |
| `START_SECONDS` | node를 유지할 시간 |
| `CHAIN_ID` | local chain id override |
| `CLAIRVEILD_BIN` | 이미 빌드한 daemon 사용 |

## 5.1 Local init helper

```bash
make init
```

`make init`은 개발자가 수동으로 local chain을 준비할 때 쓰는 편의 target입니다. 자동 smoke test와 달리 기본값은 실제 `~/.clairveil`을 대상으로 합니다.

동작:

1. `make install`로 binary를 Go install 경로에 복사합니다.
2. 기존 home이 있으면 timestamp backup으로 옮깁니다.
3. `alice`, `bob`, `relayer`, `auditor` test key를 만듭니다.
4. genesis account, validator gentx, audit master pubkey를 설정합니다.
5. ZK artifact와 `clairveil.env`를 만듭니다.

실제 홈을 건드리지 않고 검증할 때는 아래처럼 실행합니다.

```bash
tmp="$(mktemp -d)"
GOBIN="$tmp/bin" CLAIRVEIL_HOME="$tmp/home" make init
source "$tmp/home/clairveil.env"
"$tmp/bin/clairveild" start --home "$tmp/home"
```

Strict ZK preflight와 privacy proof command까지 같은 artifact 기준으로 실행하려면 아래를 먼저 적용합니다.

```bash
source ~/.clairveil/clairveil.env
```

## 6. Privacy e2e smoke

```bash
make privacy-e2e-smoke
```

이 target은 이미 떠 있는 `~/.clairveil` 노드에 붙는 테스트가 아닙니다. 임시 work dir, 임시 genesis, 임시 ZK artifact를 만들고 local node를 직접 start한 뒤 CLI flow를 실행합니다.

검증하는 기능:

1. ZK artifact 생성
2. alice/bob/relayer/auditor key 생성
3. genesis audit master pubkey 설정
4. local node start
5. shielded address/view key/disclosure key 파생
6. deposit `11`, `10`, `7`, `0` note
7. private transfer
8. public user disclosure transfer
9. recipient-encrypted user disclosure transfer
10. mandatory audit disclosure decode
11. direct withdraw
12. prepare/relay withdraw
13. final note 상태 확인

유용한 환경변수:

| env | 의미 |
| --- | --- |
| `CLAIRVEIL_E2E_WORK_DIR` | e2e work dir 고정 |
| `KEEP_WORK_DIR=1` | 종료 후 work dir 삭제하지 않음 |
| `CLAIRVEILD_BIN` | 이미 빌드한 daemon 사용 |
| `CLAIRVEIL_SETUP_BIN` | 이미 빌드한 setup binary 사용 |
| `CHAIN_ID` | local chain id override |
| `RPC_PORT`, `P2P_PORT`, `GRPC_PORT`, `API_PORT` | port 충돌 회피 |

이미 `clairveild start`가 기본 포트에서 실행 중이면 아래처럼 포트를 바꿔 실행합니다.

```bash
RPC_PORT=27657 P2P_PORT=27656 GRPC_PORT=9190 API_PORT=1417 make privacy-e2e-smoke
```

실패 디버깅 예:

```bash
KEEP_WORK_DIR=1 make privacy-e2e-smoke
```

## 7. Tutorial 검증 상태

`docs/clairveil-local-privacy-walkthrough-kr.md`는 사람이 한 줄씩 따라 하는 manual tutorial입니다. 같은 핵심 flow는 `scripts/privacy-e2e-smoke.sh`가 자동으로 검증합니다.

현재 튜토리얼은 아래 기준으로 정리되어 있습니다.

- public clone path인 `~/clairveil` 사용
- tutorial workspace인 `~/clairveil-privacy-walkthrough` 사용
- `keyring-backend test` 사용
- placeholder는 tx hash처럼 이전 출력에서 가져와야 하는 값만 사용
- public disclosure, recipient disclosure, audit disclosure, direct withdraw, relayed withdraw 포함

튜토리얼을 수정했다면 최소 아래를 실행합니다.

```bash
make privacy-e2e-smoke
```

명령 문자열 자체를 많이 바꿨다면 manual walkthrough도 한 번 실제 shell에서 따라가야 합니다.

## 8. Release pack 검증

```bash
make release-pack
make release-pack-verify
```

`release-pack-verify`가 확인하는 것:

- 외부 `.sha256`과 archive bytes 일치
- archive 내부 `SHA256SUMS.txt` 검증
- 필수 handoff 파일 존재
- 기본 archive의 manifest commit이 현재 `HEAD`와 일치

## 9. Docker prover 검증

```bash
make docker-proverd-build
```

Docker daemon이 필요합니다. 이 검증은 release-critical하지만 일반 CI 기본 경로에는 포함하지 않습니다.

검증 범위:

- compose config
- Dockerfile build
- image inspect

## 10. 문서만 바꾼 경우

문서만 바꿨더라도 아래는 가볍게 확인합니다.

```bash
git diff --check
make release-pack-verify
```

README, release handoff, 테스트 명령, 튜토리얼 명령을 바꿨다면 `make ci` 또는 관련 smoke test까지 실행하는 편이 안전합니다.
