# Clairveil 시작 가이드

> English version: [clairveil-getting-started.md](clairveil-getting-started.md)

이 문서는 현재 checkout의 전제조건, 초기화, 설정, 초기 장애 대응 기준입니다. Clairveil은 `PUBLICATION_READY_EXPERIMENTAL` 상태이며, 아래 절차는 development chain과 development Groth16 artifact를 만듭니다. Production 배포나 trusted setup ceremony가 아닙니다.

## 1. 전제조건

기본 repository workflow에 필요한 도구:

| 도구 | 기준 | 사용처 |
| --- | --- | --- |
| Go | `1.25.12` | build, test, binary, circuit setup |
| Python | `3.9+` | init/release script와 JSON 검증 |
| Bash | `/bin/bash` | Make target과 script |
| Git | Repository가 최소 version을 고정하지 않음 | clone, exact-ref 문서, release manifest |
| Make | Repository가 최소 version을 고정하지 않음 | repository build, test, init, release target |
| Node.js/npm | Node.js `22+` | `make examples`, `make ci` |

선택 도구는 작업별로 필요합니다. 외부 DSN 없이 PostgreSQL reservation integration을 실행할 때는 Docker, live batch localnet gate에는 `grpcurl`, proto output 재생성에만 `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`, `buf`, `clang-format`이 필요합니다.

초기화 전에 주요 버전을 확인합니다.

```bash
git --version
make --version
go version
python3 --version
bash --version
node --version
npm --version
```

Script는 Unix-like command environment를 가정합니다. Platform 지원 범위는 repository CI와 downstream project가 입증해야 하며, 이 문서는 검증하지 않은 OS 지원을 주장하지 않습니다.

## 2. 리소스 계획

`make init`은 active development circuit artifact 전체를 생성합니다. 기록된 full-shape batch run의 peak RSS는 약 `3,339,862,016`~`3,354,689,536` bytes였고 batch R1CS와 PK는 각각 `122,813,535`, `209,218,621` bytes입니다. 이 값은 hard limit가 아니라 reference measurement입니다. 사용 가능한 memory 4 GiB 초과, free disk 1 GiB 이상을 계획하고 Go build cache와 local chain data를 위한 여유를 추가하세요.

Validator는 exact consensus identity 비교 후 required VK만 필요합니다. Prover는 선택한 R1CS/PK pair도 필요하므로 storage와 memory 경계가 더 큽니다.

## 3. 초기화와 시작

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
make init
source ~/.clairveil/clairveil.env
clairveild start
```

`make init`은 전체 binary를 빌드하고 README에 나열된 project binary 여섯 개를 `GOBIN` 또는 `$(go env GOPATH)/bin`에 설치한 뒤 아래를 수행합니다. 설치되는 `clairveil-verify`는 legacy-only debugging helper이며 초기화와 현행 note 검증에서는 사용하지 않습니다.

1. 기존 `~/.clairveil`을 timestamp backup으로 이동합니다.
2. `privacy-note-v1` development artifact set을 생성합니다.
3. `alice`, `bob`, `relayer`, `auditor` development key를 만듭니다.
4. Genesis를 초기화하고 계정 funding과 validator gentx를 수행합니다.
5. Auditor disclosure public key를 genesis에 기록합니다.
6. Export된 runtime environment를 `~/.clairveil/clairveil.env`에 씁니다.

생성된 `init-out/*-key.json`에는 development key material이 들어 있습니다. Home 권한을 제한하고, 이 key를 재사용하거나 production 환경으로 복사하지 마세요.

활성화된 기본 listener는 RPC `26657`, P2P `26656`, gRPC `9090`입니다. Generated `app.toml`은 REST address를 `tcp://localhost:1317`로 설정하지만 `[api] enable = false`이므로 기본상태에서는 `1317`을 bind하지 않습니다. `enable = true`를 명시하거나 `--api.enable`로 시작한 경우에만 해당 address를 bind합니다. Reference node를 시작하기 전에 활성 port를 쓰는 다른 local node를 멈추세요. Smoke-test script는 port override를 받지만 일반 `clairveild start`는 generated config 또는 daemon flag로 변경합니다.

## 4. 주요 설정

| 변수 | 기본값 | 범위 |
| --- | --- | --- |
| `GOBIN` | `go env GOBIN`, 이후 `$(go env GOPATH)/bin` | Binary 설치 |
| `CLAIRVEIL_HOME` | `~/.clairveil` | `make init` home과 backup 위치 |
| `CHAIN_ID` | `clairveil-local-1` | Init/smoke-test chain ID |
| `NODE_NAME` | `local` | Init/smoke-test node moniker |
| `KEYRING_BACKEND` | `test` | Development init keyring |
| `CLAIRVEIL_INIT_ACCOUNTS` | `alice bob relayer auditor` | 공백 구분 init key |
| `VALIDATOR_KEY` / `AUDITOR_KEY` | `alice` / `auditor` | 필수 역할이며 account list에 포함돼야 함 |
| `FUND_AMOUNT` / `STAKE_AMOUNT` | Script 기본값 | Development genesis balance |
| `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR` | `make init`에서는 `<home>/artifacts/privacy`, unset runtime fallback은 `.` | Artifact output/runtime directory. `clairveil.env`를 source하거나 명시적으로 설정 |
| `CLAIRVEILD_BIN` / `CLAIRVEIL_SETUP_BIN` | 설치된 binary | 명시적 binary override |
| `RPC_PORT`, `P2P_PORT`, `ABCI_PORT`, `GRPC_PORT`, `API_PORT`, `PPROF_PORT` | Script 기본값 | Smoke/localnet script 설정. REST disabled 상태에서 `API_PORT`는 bind하지 않음 |

격리된 초기화 예:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home \
CHAIN_ID=my-local-chain \
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" \
make init
```

`clairveil-setup`이 쓰는 `privacy_zk_checksums.env`는 exported variable이 아니라 shell assignment입니다. `make init` 밖에서 raw file을 source할 때는 directory를 명시하고 모든 assignment를 export합니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/absolute/path/to/artifacts/privacy
set -a
source "$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

`make init`이 만든 `~/.clairveil/clairveil.env`에는 이미 `export`가 포함되어 있어 바로 source할 수 있습니다.

## 5. 첫 검증

실행 중인 node 없이:

```bash
make docs-check
go test ./...
make build
```

활성화된 기본 RPC, P2P, gRPC port에 다른 node가 없을 때:

```bash
make localnet-smoke
make privacy-e2e-smoke
```

더 무거운 batch, payroll, benchmark, release gate는 [clairveil-testing-guide-kr.md](clairveil-testing-guide-kr.md)에서 선택합니다.

## 6. Troubleshooting과 정리

- `clairveild: command not found`: `$(go env GOPATH)/bin` 또는 `go env GOBIN` 값을 `PATH`에 넣고 `make install`을 다시 실행합니다.
- setup이 kill되거나 out-of-memory/no-space를 보고함: 경쟁 workload를 멈추고 disk를 확보하거나 더 큰 host에서 artifact를 생성합니다. Strict preflight를 통과한 complete artifact directory만 재사용합니다.
- artifact checksum/circuit identity mismatch: 오래된 development artifact와 proof job을 제거하고 exact active set을 다시 생성한 뒤 fresh genesis에서 시작합니다. Environment checksum은 consensus identity를 override할 수 없습니다.
- `address already in use`: 기존 node를 멈추거나 smoke script의 관련 port 전체를 바꿉니다. RPC/P2P/gRPC/REST 중 하나만 바꾸면 안 됩니다.
- `privacy_scan`이 `ResourceExhausted`를 반환함: typed record 하나가 `max_encoded_bytes`를 초과했다면 server 최대치 안에서 byte budget을 늘립니다. Output/event limit을 줄여도 단일 record는 분할되지 않습니다. 최대치에도 들어가지 않으면 server/contract incident로 취급합니다. 마지막으로 수락된 cursor를 저장하고 건너뛰지 않습니다.
- `commitment_paths_at_root`가 `ResourceExhausted`를 반환함: historical rebuild가 너무 크면 current root 또는 trusted local historical index를 사용하고, 일시적인 rebuild admission 포화라면 bounded retry합니다. 요청 commitment 수를 줄여도 historical tree의 leaf count는 줄지 않습니다.
- `make release-pack-verify`가 dirty tree를 거부함: 편집 중에는 `make docs-check`를 사용합니다. Clean untagged commit은 commit-bound CI snapshot에만 사용하고, 공개 가능한 release pack은 최종 annotated exact-SemVer tagged commit에서 생성·검증합니다. 또는 out-of-band commit과 함께 explicit archive를 검증합니다.

Disposable home을 지우려면 node를 먼저 멈추고 자신이 명시적으로 선택한 path만 삭제합니다. `make init`은 기존 home을 `<home>.backup-YYYYMMDD-HHMMSS`로 보존합니다. 이 backup에도 private development key와 wallet data가 있을 수 있으므로 직접 검토한 뒤 삭제하세요.

다음 문서: [아키텍처](clairveil-architecture-kr.md), [local walkthrough](clairveil-local-privacy-walkthrough-kr.md), [운영 가이드](clairveil-operations-guide-kr.md), [전체 문서 index](README-kr.md).
