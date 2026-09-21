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
| `make init` | binary를 build/install하고 필요한 `clairveild init --audit-config ... --chain-id ...` command 형태만 안내. Home은 초기화하지 않음 |
| `make docs-check` | Markdown, EN/KR pair, manifest, prover schema fixture |
| `make examples` | JS audit key, fixture validator, prover HTTP client 검사 |
| `make privacy-batch-joinsplit-localnet` | node/prover를 시작하지 않는 static legacy batch fixture와 SDK conformance |
| `make privacy-bulk-readiness-check` | focused unit/reservation check와 synthetic legacy capacity planning. `RUN_LOCALNET` 거절 |
| `make docker-proverd-build` | Dockerfile/compose build 검증 |

`make release-check`는 `ci`, `vulncheck`, static legacy batch conformance gate, default static/unit/synthetic bulk-readiness check를 실행합니다. Node/prover를 시작하지 않으므로 live V2 또는 capacity 증적을 제공하지 않습니다.

## Batch와 payroll gate

| Gate | 검증하는 것 | 검증하지 않는 것 |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | process를 시작하지 않는 static legacy fixture와 SDK conformance | V2 runtime validation |
| `make reference-payroll-demo` | Legacy multi-message repository-local regression | 실제 node 또는 one-proof batch transfer |
| `make reference-payroll-rehearsal` | Legacy simulation과 capacity-planning report | Live node, V2 runtime validation 또는 one-proof production capacity |

`privacy-batch-joinsplit-localnet`, `privacy-bulk-readiness-check`, `reference-payroll-rehearsal`은 0이 아닌 `RUN_LOCALNET`을 작업 전에 거절합니다. Native CLI walkthrough는 이 gate와 별개이며 public audit config, 일치 artifact bundle, node startup, transaction 실행, rescan, auditor 검사를 명시적으로 기록해야 합니다.

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

현재 checkout에는 end-to-end native V2 smoke target이 없습니다. Historical localnet/latency report와 legacy load example은 reference 전용으로 유지합니다.

`make init`은 home이나 `clairveil.env`를 만들지 않고 binary를 install한 뒤 다음 command만 출력합니다. 수동 V2 native run은 [시작 가이드의 초기화](clairveil-getting-started-kr.md#초기화와-시작)를 따르고, 격리가 필요하면 explicit `--home`을 선택하며, 선택한 chain mode에 필요한 일반 Cosmos account/genesis-account/gentx/collect-gentxs 준비를 start 전에 완료합니다. Public audit configuration은 `init/start`에, artifact directory는 `start`에 전달하고 같은 artifact를 `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR`로 `clairveil-proverd`에 지정합니다. Private audit key는 node initialization이 아니라 external auditor에 속합니다.

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

명령이나 기대 동작이 바뀌면 사용 가능한 관련 검사를 실행하며, 사용 불가 wrapper를 통과 증적으로 보고하면 안 됩니다. Remote profile과 Merkle
restore/recovery 경계는 [operations guide](clairveil-operations-guide-kr.md)를 따릅니다.
