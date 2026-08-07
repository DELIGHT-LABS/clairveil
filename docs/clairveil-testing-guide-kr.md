# Clairveil 테스트 가이드

영문: [clairveil-testing-guide.md](clairveil-testing-guide.md)

이 문서로 변경을 입증하는 최소 테스트를 선택합니다. 초기화와 privacy end-to-end
walkthrough는 [시작 가이드](clairveil-getting-started-kr.md)를 따릅니다.

## 일상 및 release 검증

일반 PR에서는 다음을 실행합니다.

```bash
make ci
make vulncheck
```

두 명령 모두 실행 중인 `clairveild`가 필요 없습니다. `make ci`는 문서 검사, Go
test와 build, JS example을 실행합니다. Release 후보나 넓은 변경에는 다음도 실행합니다.

```bash
make release-check
```

| 명령 | 용도 |
| --- | --- |
| `make test` | 전체 Go unit/integration test |
| `make build` | 모든 project binary와 build-only load tool |
| `make init` | 수동 development-chain home 준비. 기본 home은 `~/.clairveil` |
| `make docs-check` | Markdown, EN/KR pair, manifest, prover schema fixture |
| `make examples` | JS audit key, fixture validator, prover HTTP client 검사 |
| `make localnet-smoke` | fresh genesis에서 daemon 시작 |
| `make privacy-e2e-smoke` | deposit, transfer, disclosure, direct/relayed withdraw 흐름 |
| `make docker-proverd-build` | Dockerfile/compose build 검증 |

`make release-check`는 `ci`, `vulncheck`, `localnet-smoke`,
`privacy-e2e-smoke`, **static** batch gate와
`RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`를 실행합니다.
실제 one-proof batch gate, `reference-payroll-*`, 실제 16x32 capacity workload는
실행하지 않습니다.

## Batch와 payroll gate

| Gate | 검증하는 것 | 검증하지 않는 것 |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | Static BatchJoinSplit16x32 fixture와 SDK conformance | Node/prover를 시작하거나 proof를 만들지 않음 |
| `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` | Local node/prover를 통한 실제 one-proof workflow | Throughput 또는 production capacity |
| `make reference-payroll-demo` | Legacy multi-message repository-local regression | 실제 node 또는 one-proof batch transfer |
| `make reference-payroll-live-localnet` | Legacy multi-message `transfer-batch` localnet regression | One-proof workflow 또는 production-capacity claim |
| `make reference-payroll-rehearsal` | Legacy simulation, regression, capacity-planning report | One-proof production capacity |

Release 또는 mainnet acceptance가 실제 one-proof gate를 주장하면 별도로 실행합니다.

```bash
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

16x32 production-capacity claim에는 실제 16x32 workload의 tag/commit-bound artifact가
필요합니다. proof/sec, tx/sec, item/sec, RSS, CPU, shape distribution,
retry/replanning/manual review, circuit/artifact identity, checksum, execution
environment를 기록합니다. Synthetic 및 legacy payroll 결과는 해당 label을 유지합니다.
Legacy reference-product handoff는
[reference-payroll README](../examples/reference-payroll/README-kr.md)를 참고합니다.

비용이 큰 full-shape feasibility gate는 opt-in이며 development setup과 proving을
수행합니다. Production artifact generation이나 trusted setup은 아닙니다.

```bash
CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v
```

Disclosure-blinding resource 비교도 opt-in입니다.

```bash
CLAIRVEIL_RUN_JOINSPLIT_BLINDING_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestJoinSplitDisclosureBlindingSeparationResourceGate$' -count=1 -v
```

## Focused Go 및 API 검사

해당 contract를 변경할 때 package test를 실행합니다.

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v
```

Deposit proving과 공통 HTTP policy는 다음으로 검사합니다.

```bash
go test ./x/privacy/client/sdk/deposit -count=1
go test ./x/privacy/client/sdk/provertransport -count=1
go test ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/conformance -count=1
go test ./cmd/clairveil-proverd -count=1
```

이 suite는 fixture shape와 semantic 동작, NoteV1, fixed payload, batch
state/gas/rollback, scan/path snapshot, prover admission, HTTP error를 다룹니다.
Deposit을 포함한 normative route contract는
[proverd HTTP API](clairveil-proverd-http-api-kr.md#deposit)에 있습니다. Schema shape와
semantic validation의 경계는 [schemas/README-kr.md](schemas/README-kr.md)를 참고합니다.

## JS example

```bash
make examples
```

이 target은 다음 명령 목록을 실행합니다.

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/js-sdk-fixture-validator run validate
npm --prefix examples/js-sdk-prover-http-client run demo
```

## Local home과 port

`make localnet-smoke`와 `make privacy-e2e-smoke`는 독립 temporary home/work
directory를 만들며 실행 중인 `~/.clairveil` node에 연결하지 않습니다. 다만 다른
process가 default Tendermint/RPC service port를 사용하면 충돌할 수 있습니다. E2E에는
다음처럼 port override를 사용합니다.

```bash
RPC_PORT=27657 P2P_PORT=27656 GRPC_PORT=9190 API_PORT=1417 make privacy-e2e-smoke
```

기본 home을 바꾸지 않는 반복 가능한 local init 예시는 다음과 같습니다.

```bash
tmp="$(mktemp -d)"
GOBIN="$tmp/bin" CLAIRVEIL_HOME="$tmp/home" make init
source "$tmp/home/clairveil.env"
"$tmp/bin/clairveild" start --home "$tmp/home"
```

`CLAIRVEIL_HOME`, `KEEP_HOME=1`, `START_SECONDS`, `CHAIN_ID`, `CLAIRVEILD_BIN`은
localnet smoke를 설정합니다. `CLAIRVEIL_E2E_WORK_DIR`, `KEEP_WORK_DIR=1`,
`CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, `CHAIN_ID`, port 변수는 privacy E2E smoke를
설정합니다. Privacy E2E flow는 이제 [시작 가이드](clairveil-getting-started-kr.md)에
포함된 walkthrough의 자동 검증 대응물입니다.

## Release pack과 문서 변경

Packaging은 clean committed tree에서 실행합니다. Publishable release에는 그 commit을
가리키는 annotated exact-SemVer tag도 필요합니다.

```bash
make release-pack
make release-pack-verify
```

`release-pack-verify`는 외부/내부 checksum, 필수 handoff file, manifest commit을
검사합니다. Untagged clean snapshot은 packaging CI 자료이며 publishable release가
아닙니다. Release 절차와 소유권 규칙은 [CONTRIBUTING-kr.md](../CONTRIBUTING-kr.md)를
참고합니다.

문서만 변경한 경우 다음을 실행합니다.

```bash
make docs-check
git diff --check
```

명령이나 기대 동작이 바뀌면 관련 smoke test도 실행합니다. Remote profile과 Merkle
restore/recovery 경계는 [operations guide](clairveil-operations-guide-kr.md)를 따릅니다.
