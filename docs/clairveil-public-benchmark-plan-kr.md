# Clairveil Privacy Circuit/Prover 공개 벤치마크 계획

이 문서는 `clairveil` privacy accounting remediation 이후의 circuit/prover 성능을 공개 가능한 형태로 측정하기 위한 계획입니다. 현재 `x/privacy/circuit/bench_test.go`의 proving benchmark는 smoke baseline으로 유용하지만, 그 결과만으로 chain TPS나 production prover capacity를 주장해서는 안 됩니다. 공개 벤치마크는 proving, verification, artifact setup, prover HTTP, localnet end-to-end throughput을 분리해 측정해야 합니다.

## 1. 목표

1. remediation 이후 `DepositCircuit`, `SpendCircuit`, `JoinSplitCircuit`의 비용을 재현 가능하게 공개합니다.
2. prover-side throughput과 validator-side verification cost를 분리합니다.
3. chain TPS는 circuit proving 결과에서 추정하지 않고 localnet end-to-end 결과로만 말합니다.
4. benchmark 환경, commit, artifact set, dependency version을 함께 공개합니다.
5. downstream integrator가 자기 환경에서 같은 benchmark를 다시 실행할 수 있게 합니다.

## 2. 공개 원칙

- `go test -bench` 1회 결과는 smoke result로만 표기합니다.
- 공개 숫자는 같은 commit에서 `-count=10` 이상 실행하고 `benchstat`으로 요약합니다.
- machine spec, Go version, gnark version, OS/arch, CPU governor 또는 power mode를 기록합니다.
- benchmark 결과에는 `active_set_id`, artifact manifest checksum, source commit hash를 포함합니다.
- native proving TPS, remote prover RPS, validator verification TPS, chain transaction TPS를 같은 숫자로 섞지 않습니다.
- local laptop 숫자는 "reference workstation" 결과로 표기하고 production capacity로 표현하지 않습니다.

## 3. 측정 범위

### 3.1 Native circuit proving

목적: 순수 Groth16 proof generation cost를 측정합니다.

대상:

- `BenchmarkDepositCircuitProve`
- `BenchmarkSpendCircuitProve`
- `BenchmarkJoinSplitCircuitProve`

현재 상태: 구현됨. `make privacy-bench`가 raw `go test -bench` 출력과 JSON/Markdown report를 함께 생성합니다.

권장 명령:

```bash
BENCH_PATTERN='Benchmark(Deposit|Spend|JoinSplit)CircuitProve$' \
BENCH_COUNT=10 \
make privacy-bench
```

### 3.2 Native proof verification

목적: validator가 tx 처리 중 proof를 검증하는 비용을 별도로 측정합니다.

구현된 benchmark:

- `BenchmarkDepositCircuitVerify`
- `BenchmarkSpendCircuitVerify`
- `BenchmarkJoinSplitCircuitVerify`
- `BenchmarkDepositCircuitPublicWitness`
- `BenchmarkSpendCircuitPublicWitness`
- `BenchmarkJoinSplitCircuitPublicWitness`

구현 기준:

- benchmark setup 단계에서 circuit compile, setup, proof generation을 완료합니다.
- timer reset 이후에는 `frontend.NewWitness(..., frontend.PublicOnly())`와 `groth16.Verify(...)` 비용만 측정합니다.
- public witness 생성 비용과 순수 verify 비용을 필요하면 별도 benchmark로 분리합니다.

권장 명령:

```bash
BENCH_PATTERN='Benchmark(Deposit|Spend|JoinSplit)Circuit(Verify|PublicWitness)$' \
BENCH_COUNT=10 \
make privacy-bench
```

### 3.3 Compile/setup/artifact generation

목적: release artifact 생성 비용과 artifact 크기를 공개합니다.

측정 항목:

- circuit compile time: `Benchmark*CircuitCompile`
- Groth16 setup time: `Benchmark*CircuitSetup`
- artifact write time: `Benchmark*CircuitArtifactWrite`
- R1CS/PK/VK file size
- artifact SHA-256
- `privacy_zk_manifest.json` content

현재 구현은 compile/setup/artifact write latency를 benchmark report에 담고, artifact manifest가 존재하면 manifest checksum을 report metadata에 기록합니다. 개별 R1CS/PK/VK size와 checksum을 행 단위로 공개하려면 release artifact manifest를 함께 첨부해야 합니다.

권장 출력:

```text
artifact_set_id
commit
generated_at
circuit_id
r1cs_bytes
pk_bytes
vk_bytes
r1cs_sha256
pk_sha256
vk_sha256
compile_ms
setup_ms
```

### 3.4 Prover HTTP benchmark

목적: `clairveil-proverd`가 remote/local sidecar로 동작할 때 request/response overhead를 포함한 성능을 측정합니다.

대상 endpoint:

- `POST /v1/prover/transfer`
- `POST /v1/prover/withdraw`

Deposit은 현재 별도 HTTP prover endpoint가 없습니다. 공개 벤치마크에서 deposit remote proving까지 비교하려면 다음 중 하나를 먼저 정해야 합니다.

- deposit proof는 CLI/SDK local proving만 측정합니다.
- `/v1/prover/deposit` endpoint를 추가한 뒤 같은 방식으로 측정합니다.
- browser/WASM prover adapter를 별도 범주로 둡니다.

측정 항목:

- concurrency: 1, 2, 4, 8, 16
- latency: p50, p95, p99
- throughput: successful proofs/sec
- error rate
- request/response payload bytes
- prover memory high-water mark

현재 구현:

- `BenchmarkHTTPProverClientTransferRoundTrip`
- `BenchmarkHTTPProverClientWithdrawRoundTrip`
- `make privacy-proverd-bench`

이 benchmark는 `httptest` 기반 in-process HTTP transport round trip입니다. JSON encode/decode, request/response validation, HTTP client/server overhead를 고정하지만, 별도 `clairveil-proverd` 프로세스의 concurrency sweep이나 OS-level memory high-water mark는 아직 측정하지 않습니다. 공개 문구에서는 "prover HTTP transport benchmark"로 표기하고 production sidecar capacity로 쓰지 않습니다.

### 3.5 Localnet end-to-end throughput

목적: 실제 chain tx path에서 deposit, transfer, withdraw 처리량을 측정합니다.

측정 플로우:

1. fixed localnet을 시작합니다.
2. fresh artifact set과 `active_set_id`를 기록합니다.
3. funded accounts와 shielded identities를 생성합니다.
4. deposit batch를 실행합니다.
5. transfer batch를 실행합니다.
6. withdraw batch를 실행합니다.
7. `reserve/{denom}` query가 `invariant_holds=true`인지 확인합니다.
8. tx success rate, block inclusion latency, gas, block time, end-to-end TPS를 기록합니다.

현재 구현:

- `privacy-e2e-smoke.sh`가 각 tx inclusion query JSON을 `*-query.json`으로 저장합니다.
- `make privacy-bench-localnet`가 e2e smoke를 실행하고, 저장된 tx query에서 `gas_used`, `gas_wanted`, `success`를 추출해 `benchmarks/privacy-localnet/tx-metrics-*.json`을 생성합니다.
- 같은 명령이 `clairveil-benchreport -tx-metrics ...`를 호출해 localnet gas 기반 예상 fee report를 생성합니다.

아직 batch throughput runner와 chain TPS 계산은 구현하지 않았습니다. 현재 localnet 명령은 fee/gas smoke scaffold이며, chain TPS 공개 수치로 쓰면 안 됩니다.

주의:

- prover가 client-side인지 remote sidecar인지 반드시 분리해 표기합니다.
- chain TPS는 localnet consensus 설정, block time, mempool, account sequence handling에 영향을 받습니다.
- circuit proving benchmark에서 chain TPS를 환산해 공개하지 않습니다.

### 3.6 예상 fee 산출

목적: deposit, transfer, withdraw를 실제 chain에서 사용할 때 사용자가 부담할 예상 fee를 공개 가능한 방식으로 계산합니다.

원칙:

- fee는 proving latency나 prover throughput에서 추정하지 않습니다.
- fee는 localnet e2e benchmark의 tx별 `gas_used` 관측값과 명시된 fee policy로만 계산합니다.
- chain마다 `min_gas_price`, fee denom, gas adjustment, fee grant, priority fee 정책이 다를 수 있으므로 "Clairveil default fee"처럼 일반화하지 않습니다.
- 공개 자료에는 계산에 사용한 fee policy를 반드시 같이 표기합니다.

필수 기록 항목:

```text
tx_type
gas_used_p50
gas_used_p95
gas_limit_policy
gas_adjustment
min_gas_price
fee_denom
estimated_fee_p50 = ceil(gas_used_p50 * gas_adjustment * min_gas_price)
estimated_fee_p95 = ceil(gas_used_p95 * gas_adjustment * min_gas_price)
```

권장 tx type:

- `deposit`
- `transfer`
- `withdraw`
- `relay_withdraw`
- `dummy_deposit`
- `self_merge`

예시 표:

| Tx type | Gas p50 | Gas p95 | Gas adjustment | Min gas price | Estimated fee p50 | Estimated fee p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| deposit | TBD | TBD | `1.2` | `0.025uclair` | TBD | TBD |
| transfer | TBD | TBD | `1.2` | `0.025uclair` | TBD | TBD |
| withdraw | TBD | TBD | `1.2` | `0.025uclair` | TBD | TBD |

주의:

- localnet gas result는 reference app/config 기준입니다. Downstream chain이 ante handler, fee market, wasm/precompile, custom authz, telemetry hook을 추가하면 gas가 달라질 수 있습니다.
- prover cost는 chain fee가 아닙니다. Remote prover 운영비를 공개하려면 별도 "prover infra cost" 항목으로 CPU time, memory, cloud instance price를 분리해 계산합니다.
- 공개 문구에는 "이 fee는 특정 benchmark config의 예상값이며, production chain fee policy가 확정되면 달라질 수 있음"을 붙입니다.

실행 예:

```bash
FEE_DENOM=uclair \
MIN_GAS_PRICE=0.025 \
GAS_ADJUSTMENT=1.2 \
make privacy-bench-localnet
```

이미 수집된 gas metric을 circuit/prover report와 합치려면 아래처럼 실행합니다.

```bash
TX_METRICS=benchmarks/privacy-localnet/latest-tx-metrics.json \
FEE_DENOM=uclair \
MIN_GAS_PRICE=0.025 \
GAS_ADJUSTMENT=1.2 \
make privacy-bench
```

### 3.7 Browser/WASM benchmark

목적: web wallet 사용자가 직접 proving할 때의 UX budget을 측정합니다.

측정 항목:

- browser: Chrome, Safari, Firefox
- device class: desktop, laptop, mobile
- first proof latency
- warm proof latency
- WASM bundle size
- peak JS heap
- cancellation/timeout behavior

이 항목은 JS/WASM prover adapter가 준비된 뒤 별도 문서로 확장합니다.

## 4. 결과 파일 형식

공개 벤치마크는 아래 파일을 생성하는 것을 목표로 합니다.

```text
benchmarks/privacy-circuits/latest.json
benchmarks/privacy-circuits/latest.md
benchmarks/privacy-circuits/<date>-<commit>.json
benchmarks/privacy-circuits/<date>-<commit>.md
benchmarks/privacy-proverd/latest.json
benchmarks/privacy-proverd/latest.md
benchmarks/privacy-localnet/latest.json
benchmarks/privacy-localnet/latest.md
benchmarks/privacy-localnet/latest-tx-metrics.json
```

JSON에는 최소한 아래 필드를 포함합니다.

```json
{
  "schema_version": "v1",
  "commit": "",
  "active_set_id": "privacy-accounting-v2",
  "go_version": "",
  "gnark_version": "",
  "os": "",
  "arch": "",
  "cpu": "",
  "fee_model": {
    "fee_denom": "",
    "min_gas_price": "",
    "gas_adjustment": ""
  },
  "benchmarks": [],
  "fees": []
}
```

Markdown 결과는 공개 설명용으로 사용하고, JSON은 CI trend와 downstream 재현용으로 사용합니다.

## 5. 현재 smoke baseline

아래 숫자는 공개 throughput 주장이 아니라 remediation 후 benchmark가 동작하는지 확인한 smoke baseline입니다.

환경:

- machine: Apple M5 Pro
- arch: arm64
- benchmark mode: `-benchtime=1x`
- source commit: `4c162c6`
- active set id: `privacy-accounting-v2`

| Benchmark | Smoke latency | 단순 역수 |
| --- | ---: | ---: |
| Deposit prove | 약 7.4 ms | 약 134 proofs/sec |
| Spend prove | 약 71.8 ms | 약 13.9 proofs/sec |
| JoinSplit prove | 약 154.5 ms | 약 6.5 proofs/sec |

해석:

- `-benchtime=1x`는 variance를 설명하지 못합니다.
- 단순 역수는 single-process native proving rate일 뿐입니다.
- 이 숫자는 validator verification TPS나 chain TPS가 아닙니다.
- 공개 자료에는 `-count=10`, `benchstat`, verification benchmark, localnet 결과를 포함해야 합니다.

## 6. 구현 작업

### Phase B0: benchmark harness 정리

- existing proving benchmark의 setup/timer boundary를 재확인합니다.
- verify benchmark를 추가합니다.
- compile/setup benchmark를 별도로 추가합니다.
- benchmark helper가 valid fixture assignment를 공유하되 mutable state 오염이 없도록 합니다.

완료 기준:

- `go test ./x/privacy/circuit -run '^$' -bench Benchmark -benchmem` 통과
- proving/verification benchmark가 같은 valid assignments와 artifact assumptions를 사용

현재 상태: 완료. Prove/PublicWitness/Verify/Compile/Setup/ArtifactWrite benchmark가 추가되었습니다.

### Phase B1: metadata collector 추가

- commit hash
- dirty worktree 여부
- Go version
- gnark module version
- OS/arch
- CPU model
- active circuit set id
- artifact manifest checksum

완료 기준:

- benchmark JSON에 환경 메타데이터가 자동 기록됨
- dirty worktree일 경우 결과에 `dirty: true`가 표시됨

현재 상태: 완료. `cmd/clairveil-benchreport`가 commit, dirty state, Go/gnark/gnark-crypto version, OS/arch, CPU, active set id, optional manifest checksum을 기록합니다. `make privacy-bench`, `make privacy-proverd-bench`, `make privacy-bench-localnet`는 output 파일을 만들기 전에 source commit과 dirty state를 캡처해 report artifact 생성 자체가 `dirty: true`로 오인되지 않게 합니다.

### Phase B2: report generator 추가

- raw `go test -bench` output을 JSON으로 저장합니다.
- `benchstat`이 설치되어 있으면 별도 `latest-benchstat.txt`로 요약을 저장합니다.
- smoke result와 public result를 명확히 구분합니다.

완료 기준:

- `make privacy-bench` 또는 동등한 명령으로 `latest.json`, `latest.md` 생성
- Markdown 결과가 공개 문서에 그대로 붙일 수 있는 형태임

현재 상태: 완료. `scripts/privacy-bench.sh`가 raw output, optional benchstat output, JSON/Markdown report를 생성합니다.

### Phase B3: prover HTTP benchmark 추가

- local `clairveil-proverd`를 고정 config로 실행합니다.
- prepared transfer/withdraw payload fixture를 사용합니다.
- concurrency sweep과 latency percentile을 측정합니다.

완료 기준:

- transfer/withdraw endpoint별 p50/p95/p99, RPS, error rate 기록
- request timeout/auth/malformed response failure가 benchmark와 별도 smoke test에서 검증됨

현재 상태: 부분 완료. `make privacy-proverd-bench`가 in-process HTTP transport round trip benchmark를 생성합니다. 별도 `clairveil-proverd` 프로세스, concurrency sweep, p99/error-rate 집계, memory high-water mark는 후속 작업입니다.

### Phase B4: localnet e2e benchmark 추가

- deterministic localnet setup script를 사용합니다.
- deposit/transfer/withdraw batch runner를 추가합니다.
- block inclusion latency, gas, success rate, reserve invariant를 기록합니다.

완료 기준:

- benchmark 종료 후 `reserve/{denom}`가 `invariant_holds=true`
- failed tx와 retried tx가 결과에서 분리됨
- chain TPS는 localnet 결과에서만 계산됨

현재 상태: 부분 완료. `make privacy-bench-localnet`가 e2e smoke tx query에서 gas/success metric을 추출하고 fee report를 생성합니다. Batch runner, reserve invariant report inclusion, block inclusion latency aggregation, chain TPS 계산은 후속 작업입니다.

### Phase B5: 예상 fee report 추가

- localnet e2e benchmark 결과에서 tx type별 gas p50/p95를 수집합니다.
- fee denom, min gas price, gas adjustment를 명시적으로 입력받습니다.
- 예상 fee p50/p95를 JSON과 Markdown report에 함께 출력합니다.
- prover infra cost와 chain fee를 별도 표로 분리합니다.

완료 기준:

- deposit/transfer/withdraw/relay-withdraw/dummy-deposit별 예상 fee가 계산됨
- fee 산식과 입력 policy가 report에 포함됨
- chain fee와 remote prover 운영비를 같은 비용으로 합산하지 않음

현재 상태: 완료. `clairveil-benchreport -tx-metrics`가 `gas_used` p50/p95와 명시된 fee policy로 예상 fee를 계산합니다. Self-merge는 localnet runner가 해당 tx type을 생성하면 같은 schema로 추가할 수 있습니다.

## 7. 공개 완료 기준

공개 benchmark로 사용하려면 아래를 모두 만족해야 합니다.

- source commit과 artifact manifest checksum이 고정되어 있습니다.
- proving benchmark와 verification benchmark가 모두 있습니다.
- `-count=10` 이상 반복 결과와 `benchstat` 요약이 있습니다.
- remote prover 또는 localnet 결과는 별도 표로 분리되어 있습니다.
- chain TPS는 localnet e2e benchmark에서만 보고합니다.
- 예상 fee는 localnet gas 관측값과 명시된 fee policy로만 계산합니다.
- reserve invariant 결과가 포함되어 있습니다.
- 결과 문서에 "smoke", "reference workstation", "production capacity"의 의미가 구분되어 있습니다.

## 8. 공개 문구 가이드

사용 가능:

- "Reference workstation에서 JoinSplit proving p50은 X ms입니다."
- "Native single-process proving benchmark 기준 처리량은 Y proofs/sec입니다."
- "Localnet 설정 A에서 end-to-end shielded transfer TPS는 Z입니다."
- "Localnet 설정 A와 fee policy B에서 transfer 예상 fee p50은 F denom입니다."

피해야 할 표현:

- "JoinSplit benchmark 역수이므로 chain TPS는 Z입니다."
- "local laptop 결과가 production prover capacity입니다."
- "verification benchmark 없이 validator throughput을 추정합니다."
- "smoke benchmark 1회 결과를 공개 성능 수치로 확정합니다."
- "proving 시간이 짧으므로 tx fee도 낮습니다."
