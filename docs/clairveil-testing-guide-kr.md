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
| `make build` | `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`와 benchmark/load tool(`clairveil-benchreport`, `clairveil-proverload`, `clairveil-localnetload`, `clairveil-userlatency`, `clairveil-bulktransferbench`) build |
| `make install` | `make build` 후 Clairveil binary를 `GOBIN` 또는 `GOPATH/bin`으로 복사 |
| `make init` | `make install` 후 기본 local chain home을 초기화해 `clairveild start` 준비 |
| `make proto` | privacy protobuf/gateway Go file 재생성 |
| `make examples` | JS audit key, fixture validator, prover HTTP client, browser DApp 예제 실행 |
| `make ci` | `test`, `build`, `examples` 묶음 |
| `make vulncheck` | govulncheck policy gate 실행 |
| `make localnet-smoke` | reference daemon이 genesis부터 start 가능한지 짧게 검증 |
| `make privacy-e2e-smoke` | deposit, transfer, disclosure, withdraw 전체 flow 검증 |
| `make reference-payroll-demo` | reference payroll product의 validate, prepare, plan, reserve, simulated daemon, final report 흐름 검증 |
| `make reference-payroll-live-localnet` | 실제 localnet에서 payroll input, reservation, transfer-batch, recipient scan, settle, final report 흐름 검증 |
| `make reference-payroll-rehearsal` | reference payroll capacity simulation과 선택적 live localnet smoke 검증 |
| `make dapp-local` | 수동 테스트용 local Clairveil node, prover, browser DApp stack 실행 |
| `make release-check` | `ci`, `vulncheck`, `localnet-smoke`, `privacy-e2e-smoke`, localnet transfer-batch smoke를 포함한 bulk readiness 묶음 |
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
| `x/privacy/circuit` | Deposit/Spend/native JoinSplit constraint, shared NoteV1 consistency, non-production BatchJoinSplit16x32 feasibility circuit |
| `x/privacy/keeper` | deposit/transfer/withdraw state transition, global commitment uniqueness, `AssetRegistryV1`, `privacy-scan-v2`, same-root path snapshot, Merkle capacity, query error handling |
| `x/privacy/types` | Msg validation, canonical `privacy-fixed-v1` payload, NoteV1 domain/empty root/key validation, future batch vector/public-input contract, max-shape wire/state feasibility, address, gateway path |
| `x/privacy/client/cli` | CLI parsing, output, disclosure decode helper |
| `x/privacy/client/sdk/*` | identity, deposit, scan, transfer, withdraw, disclosure, prover transport |
| `x/privacy/client/sdk/proverservice` | bounded request handling과 circuit별 admission(default in-flight `1`, queued `4`, positive body limit `8 MiB`) |
| `x/privacy/client/sdk/conformance` | JS/web wallet fixture contract와 independent NoteV1/future-batch golden vector |
| `x/privacy/zk` | consensus identity, public-input schema hash, role-aware lazy artifact loading, bounded batch gas/resource formula |

특정 package만 볼 때:

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
```

### 3.1 Session 2 Foundation Gate

현재 active circuit set은 `privacy-note-v1`입니다. 현재 deposit, spend, native 2x2 JoinSplit path는 NoteV1을 공유하고 canonical plaintext/envelope는 `privacy-fixed-v1`을 사용합니다. Note plaintext는 정확히 350 bytes, disclosure plaintext는 정확히 392 bytes, typed envelope header는 정확히 20 bytes입니다. `AssetRegistryV1`이 denom/asset-ID mapping의 authoritative source입니다. `privacy-scan-v2`는 global lexicographic cursor `(height, global_sequence, output_index)`를 사용하고 path test는 선택한 root와 일치하는 하나의 snapshot을 강제합니다.

이 계약의 focused test 위치는 아래와 같습니다.

- `x/privacy/types/note_v1_test.go`, `x/privacy/circuit/note_v1_consistency_test.go`: domain-separated commitment/nullifier/tree, exact empty root, canonical key, circuit/scanner 간 단일 shared 구현.
- `x/privacy/types/fixed_payload_test.go`, `x/privacy/types/batch_contract_test.go`: exact 350/392/20-byte encoding, envelope kind, reserved byte, padding, trailing-byte rejection, canonical `audit_key_id` validation. Audit key ID는 1..64 bytes이며 `[a-z0-9][a-z0-9._-]*`와 일치해야 합니다.
- `x/privacy/keeper/asset_registry_test.go`: one-to-one registry, collision/corruption rejection, query bound, canonical genesis export.
- `x/privacy/keeper/privacy_scan_test.go`, `x/privacy/keeper/path_snapshot_test.go`: global scan order, event 내부 resume, record/byte bound, sequence reuse rejection, same-root path, exact event type/fixed envelope kind/digest/key/zero·disabled sentinel/orphan·non-adjacent output의 fail-closed validation.
- `x/privacy/zk/registry_test.go`, `x/privacy/client/sdk/proverservice/admission_test.go`: role-aware lazy artifact access, exact identity behavior, queue bound, cancellation lifetime, unbounded-value rejection.
- `x/privacy/client/sdk/conformance/session2_contract_test.go`: `privacy_note_v1_contract.json`, `privacy_batch_joinsplit_v1_contract.json`의 independent verification.

Future `BatchJoinSplit16x32` public input은 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo` 순서로 정확히 reserve됩니다. Prototype protobuf와 circuit은 production batch message, keeper handler, artifact, prover route를 만들지 않습니다.

Same-root path 동작에는 두 가지 resource boundary가 있습니다. Current root request는 incremental tree node를 사용하므로 1,048,576-leaf rebuild cap이 없습니다. 모든 non-current historical-root request는 persisted `(root, leaf_count, height)` metadata를 요구하고 node를 deterministic rebuild하므로 1,048,576 leaves로 제한됩니다. 그 이상에서는 current root 또는 trusted local historical-path index를 사용해야 합니다. Complete per-prefix snapshot metadata index가 persisted되어 있다면 cap을 넘는 tree도 genesis export할 수 있습니다. 이 경우 export는 모든 historical node를 rebuild할 필요가 없습니다.

항상 실행되는 max-shape protobuf/Tx/KV/event/query wire-state gate는 아래와 같습니다.

```bash
go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v
```

정정된 max-shape golden은 canonical owner-effect payload `65,384` bytes, Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, query response `74,551` bytes입니다.

비용이 큰 full-shape circuit setup/prove/resource gate는 명시적으로 실행합니다.

```bash
CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v
```

Full gate는 16x32 circuit compile, development Groth16 setup, 여러 shape의 proof를 수행하며 constraint, artifact size, proving/verification timing, resource measurement를 출력하므로 opt-in입니다. 정정된 reference run은 constraint `1,111,837`, peak RSS `3,339,862,016` bytes, max-shape warm proving cost `55.892 ms/output`, 현재 native 2x2 baseline 대비 per-output `2.789x` 개선을 측정했습니다. Production artifact generation이나 trusted setup command가 아니라 feasibility measurement입니다.

Prepared transfer payload `v5`는 현재 outer prepared-payload contract로 그대로 유효합니다. 이 version을 inner note/disclosure encoding과 혼동하면 안 됩니다. Inner canonical payload와 envelope는 `privacy-fixed-v1`입니다. Compatibility fallback은 금지됩니다. External ClairveilJS package는 이 handoff 시점에 아직 legacy이므로 upgrade 전까지 old decoder로 해석하지 말고 새 fixed fixture를 fail closed로 거부해야 합니다.

## 4. JS/web wallet fixture 검증

```bash
make examples
```

내부적으로 아래가 실행됩니다.

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/js-sdk-fixture-validator run validate
npm --prefix examples/js-sdk-prover-http-client run demo
npm --prefix examples/clairveil-dapp ci
npm --prefix examples/clairveil-dapp run check:dapp
npm --prefix examples/clairveil-dapp run test:dapp
npm --prefix examples/clairveil-dapp run check:clairveiljs
npm --prefix examples/clairveil-dapp run test:clairveiljs
```

검증 범위:

- audit disclosure key derivation vector와 genesis public key encoding
- fixture address prefix
- chain/expiry, disclosure blinding, owner signature, `view_tag_hexes`를 포함한 prepared transfer payload `v5` hash
- sender self-view disclosure digest/payload field
- prepared withdraw payload hash
- relayed withdraw final payload hash
- relay withdraw handoff의 relayer `creator` / payload `recipient` mapping
- `scan_events` request/response fixture shape, cursor field, scan/view tag version, projection output
- batch `check_nullifiers` request/response fixture shape
- prover HTTP request/response version
- timeout/auth client shape
- browser DApp boundary check, static bundle 최신성, local helper route policy, ClairveilJS package surface smoke test

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
- public disclosure, recipient disclosure, sender self-view disclosure, audit disclosure, direct withdraw, relayed withdraw 포함

튜토리얼을 수정했다면 최소 아래를 실행합니다.

```bash
make privacy-e2e-smoke
```

명령 문자열 자체를 많이 바꿨다면 manual walkthrough도 한 번 실제 shell에서 따라가야 합니다.

## 7.1 Reference Payroll Demo

```bash
make reference-payroll-demo
```

이 target은 local chain을 띄우지 않고 repo-local 파일만으로 reference payroll product 흐름을 검증합니다.

검증 범위:

1. sample payroll input validation
2. note preparation analysis
3. payroll plan 생성
4. durable reservation state에 plan 확정
5. `clairveil-payrolld -once` simulated scheduler tick 실행
6. reservation/operation status 재조회
7. final payroll report export

기본 출력은 `tmp/reference-payroll-demo/` 아래에 생성됩니다. 성공 기준은 `status-after-daemon.json`의 모든 reservation이 `ConfirmedSpent`, 모든 operation이 `Succeeded`이고, `final-report.json`의 payroll status가 `Confirmed`인 것입니다.

## 7.2 Reference Payroll Live Localnet

```bash
make reference-payroll-live-localnet
```

이 target은 실제 localnet을 시작하고 payroll reference product를 실제 `transfer-batch` tx까지 연결해 검증합니다.

검증 범위:

1. localnet init/start
2. treasury shielded note deposit
3. `list-notes --json` 결과에서 payroll input 생성
4. payroll validate, prepare, plan, reserve
5. 실제 `clairveild tx privacy transfer-batch` broadcast
6. recipient note scan delta 확인
7. `clairveil-payroll settle-transfer-batch`
8. final payroll report export

자세한 수동 단계는 [clairveil-reference-payroll-live-localnet-tutorial-kr.md](clairveil-reference-payroll-live-localnet-tutorial-kr.md)를 따릅니다.

## 7.3 Reference Payroll Rehearsal

```bash
make reference-payroll-rehearsal
```

이 target은 1천건, 1만건, 10만건, 100개 회사 x 1천건 profile의 payroll capacity simulation report를 생성합니다. 필요하면 `RUN_LOCALNET=1 LOCALNET_PAYROLL_ITEM_COUNT=2 make reference-payroll-rehearsal`처럼 작은 live localnet smoke를 함께 실행할 수 있습니다.

1천건 repo-local restart/retry rehearsal은 아래처럼 실제 localnet transfer path를 실행하되, deposit 준비 시간을 줄이기 위해 localnet-only seed helper를 사용합니다.

```bash
PAYROLL_SEED_NOTES=1 PAYROLL_ITEM_COUNT=1000 PAYROLL_CHUNK_SIZE=20 GAS_PRICES=0uclair make reference-payroll-live-localnet
```

자세한 rehearsal 기준과 결과 해석은 [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md)와 [clairveil-reference-payroll-localnet-rehearsal-result-kr.md](clairveil-reference-payroll-localnet-rehearsal-result-kr.md)를 따릅니다.

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
