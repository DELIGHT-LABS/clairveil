# Clairveil Public Capacity Benchmark 완료 계획

이 문서는 `private/benchmark` 브랜치에서 완료한 public capacity benchmark 하네스와 public claim gate 보강 범위를 기록합니다.

남은 운영형 public capacity claim 실행 계획은 별도 문서인 `docs/clairveil-public-capacity-claim-execution-plan-kr.md`에서 관리합니다. 이 문서는 "이번에 완료한 구현 범위"만 다룹니다.

## 1. 목표

이번 작업의 목표는 smoke/reference benchmark 결과가 곧바로 공개 TPS/RPS/latency 주장으로 오해되지 않도록, benchmark 결과 생성과 public claim 승격 사이에 명확한 gate를 두는 것이었습니다.

완료 범위는 아래 세 가지입니다.

1. Prover RPS, Chain TPS, User latency claim을 machine-readable하게 분리합니다.
2. smoke/reference 결과는 기본적으로 public claim eligible이 되지 않게 합니다.
3. public claim에 필요한 evidence, source hash, artifact checksum, metric completeness가 빠지면 report에 blocking reason을 남깁니다.

## 2. 완료된 Claim 원칙

- Circuit proving benchmark의 단순 역수를 Chain TPS로 말하지 않습니다.
- In-process `httptest` transport benchmark를 운영형 prover RPS로 말하지 않습니다.
- Localnet smoke flow의 gas/fee 결과를 sustained TPS로 말하지 않습니다.
- User latency는 native, remote, browser/WASM, broadcast/inclusion 구간을 분리해서 기록할 수 있게 했습니다.
- 공개 숫자는 source commit, artifact manifest checksum, dirty state, machine profile, chain config, prover config, load profile이 함께 있을 때만 eligible이 될 수 있습니다.

## 3. Phase C0: Claim Schema와 Report Gate

### 완료한 내용

- Benchmark report에 `claim_profile`과 `claim_evidence`를 추가했습니다.
- Claim type을 `chain_tps`, `prover_rps`, `user_latency`로 분리했습니다.
- Run profile을 `smoke`, `reference`, `production_like`, `public_claim`으로 분리했습니다.
- Public claim용 result family를 제한했습니다.
  - `prover_rps`: `privacy-proverd-load` 또는 `public-capacity`
  - `chain_tps`: `privacy-localnet-tps` 또는 `public-capacity`
  - `user_latency`: `privacy-user-latency` 또는 `public-capacity`
- `public-capacity` aggregate report에서 claim type이 둘 이상이면 `claim_evidence_by_type`을 요구합니다.
- `-input`, `-benchmark-summaries`, `-tx-metrics`, evidence file을 `source_files`에 자동 포함하고 SHA-256을 계산합니다.
- Evidence file의 SHA-256과 `source_file_sha256` 값이 다르면 public claim을 차단합니다.
- Artifact descriptor completeness와 실제 artifact file checksum verification을 public claim gate에 포함했습니다.
- `errors/op`, `error_rate`, `failed_tx_rate`, `timeout_rate`, `cancel_rate`는 0 이상이고 SLO 이하이어야 합니다.
- Positive metric은 `mean`, `p50`, `p95`, `p99`, `min`, `max`가 모두 양수여야 합니다.
- Chain TPS public claim은 `failed_tx_rate`를 반드시 포함해야 합니다.
- Markdown report에서 Go benchmark 기본 table과 structured custom metric table을 분리했습니다.
- Ineligible component는 aggregate report의 `Blocked Components` 섹션에 표시합니다.

### 완료 기준

- smoke 결과는 자동으로 `claim_profile.eligible=false`가 됩니다.
- public claim 결과는 필수 metadata, result family, source hash provenance, run window, claim evidence, 64-hex artifact manifest checksum, artifact descriptor completeness, artifact file checksum verification을 요구합니다.
- Structured summary JSON은 `samples > 0`이어야 하고, 각 metric은 `mean`, `p50`, `p95`, `p99`, `min`, `max`를 모두 포함해야 합니다.
- Prover RPS row는 `load_profile`, `route`, `concurrency`, `duration_seconds`를 포함해야 합니다.
- Chain TPS row는 `load_profile`, `duration_seconds`, `target_tx_per_sec`를 포함해야 합니다.
- User latency row는 `flow_profile`, `latency_mode`, `cold_warm`을 포함해야 합니다.
- User latency public claim은 flow/mode/cold-warm bucket별 최소 100 samples 이상이어야 합니다.

## 4. Phase C1: External `clairveil-proverd` Load Harness

### 완료한 내용

- `cmd/clairveil-proverload`를 추가해 이미 실행 중인 external `clairveil-proverd`에 HTTP POST load를 걸 수 있게 했습니다.
- `scripts/privacy-proverd-load-bench.sh`와 `make privacy-proverd-load-bench` 경로를 추가했습니다.
- 기본 load input은 실제 circuit witness를 만족하는 generated transfer/withdraw prover request입니다.
- `PROVERLOAD_FIXTURE_BUNDLE`, `PROVERLOAD_TRANSFER_REQUEST`, `PROVERLOAD_WITHDRAW_REQUEST`를 지정하면 명시 입력이 generated request를 override합니다.
- `transfer_only`, `withdraw_only`, `mixed_80_20` profile을 지원합니다.
- Concurrency list, warmup, steady-state duration, request timeout, bearer token을 지원합니다.
- 각 route를 measured bucket 전에 preflight합니다.
- Preflight 실패 시 measured bucket을 실행하지 않고 non-zero exit로 종료합니다.
- Preflight 실패 메시지에는 route, status code, response body preview가 포함됩니다.
- `clairveil-proverd`의 `/debug/vars`를 샘플링해 CPU/RSS/heap/goroutine metric을 수집합니다.
- Bucket duration은 새 request scheduling 중단 기준으로만 사용합니다. 이미 시작된 request는 client timeout 안에서 완료시켜 결과에 포함합니다.

### 수정한 문제

이전 smoke run에서 `privacy_prover_example_bundle.json`은 HTTP schema fixture로는 유효했지만 실제 JoinSplit witness로는 유효하지 않아 `constraint #32267 is not satisfied`로 실패했습니다.

이번 작업 후 기본 generated request 경로는 아래 두 레벨에서 검증됩니다.

- In-memory Groth16 integration test가 transfer와 withdraw proof 생성을 확인합니다.
- External `clairveil-proverd` smoke가 generated artifact와 generated valid request로 `error_rate=0`을 확인했습니다.

### 완료 기준

- `benchmarks/privacy-proverd-load/latest.json` 생성 경로가 있습니다.
- concurrency별 latency/RPS/error table을 만들 수 있습니다.
- p99와 error rate가 public report에 포함됩니다.
- artifact preflight mode와 checksum이 report에 포함됩니다.
- auth enabled/disabled 여부가 report에 포함됩니다.
- invalid fixture 또는 route preflight 실패 시 `latest.json`을 성공 결과처럼 생성하지 않습니다.
- Bucket 종료 시점의 정상 in-flight proof가 context deadline error로 집계되지 않습니다.

## 5. Phase C2: Chain TPS Summary Harness

### 완료한 내용

- `cmd/clairveil-localnetload`가 localnet tx metrics를 `chain_tps` structured benchmark summary로 변환합니다.
- `scripts/privacy-localnet-tps-bench.sh`와 `make privacy-localnet-tps-bench` 경로를 추가했습니다.
- 기존 `privacy-bench-localnet.sh`는 tx query JSON에서 gas, success, height, included timestamp를 추출하고 submitted timestamp와 함께 tx metrics를 생성합니다.
- Tx별 `success`가 누락되면 성공으로 추정하지 않고 실패로 계산합니다.
- Structured row는 `claim_type=chain_tps`, `load_profile`, `duration_seconds`, `target_tx_per_sec`와 아래 metric을 포함합니다.
  - `submitted_tx/sec`
  - `accepted_tx/sec`
  - `included_tx/sec`
  - `successful_tx/sec`
  - `tx/sec`
  - `failed_tx_rate`
  - `inclusion_latency_ms`
  - `gas_used`
- Positive inclusion latency sample이 하나도 없으면 `inclusion_latency_ms` metric을 생성하지 않습니다.

### 완료 기준

- `benchmarks/privacy-localnet-tps/latest.json` 생성 경로가 있습니다.
- failed tx가 성공 처리량에 섞이지 않습니다.
- smoke report가 0ms inclusion latency를 public evidence처럼 보이게 만들지 않습니다.
- Public claim gate는 positive inclusion latency evidence가 없으면 `chain_tps metrics missing: inclusion_latency_ms`로 차단합니다.

## 6. Phase C3: User Perceived Latency Harness

### 완료한 내용

- CLI privacy tx command는 `CLAIRVEIL_PRIVACY_LATENCY_TRACE_FILE`이 설정된 경우 JSONL trace를 append합니다.
- 기본 사용자 실행에서는 trace를 쓰지 않아 CLI 출력과 command interface가 유지됩니다.
- Trace event는 `flow_id`, `flow_profile`, `latency_mode`, `cold_warm`, `phase`, `duration_ms`, `success`, optional `txhash`를 포함합니다.
- Native CLI trace는 deposit/transfer/withdraw/prepare-withdraw/relay-withdraw에서 `prepare`, `proof`, `submit`, `total` phase를 기록합니다.
- Transfer는 tx hash를 trace에 기록하므로 localnet tx metrics와 inclusion latency를 매칭할 수 있습니다.
- `cmd/clairveil-userlatency`가 trace JSONL 또는 JSON array를 flow 단위로 집계합니다.
- `scripts/privacy-user-latency-bench.sh`와 `make privacy-user-latency-bench` 경로를 추가했습니다.
- `USER_LATENCY_REPEAT`를 지원합니다.
- `RUN_PROFILE=public_claim`에서는 기본적으로 `USER_LATENCY_REPEAT>=100`을 요구합니다.

### 완료 기준

- Native CLI latency report를 별도 result family로 생성할 수 있습니다.
- "submit-ready latency"와 optional inclusion latency가 분리됩니다.
- Public claim mode에서 sample count 부족을 실행 전 guard 또는 report gate로 차단합니다.

## 7. Phase C4: Capacity Report Generator

### 완료한 내용

- `clairveil-benchreport -merge-reports`가 component benchmark report JSON을 `benchmarks/public-capacity` aggregate report로 묶습니다.
- `scripts/privacy-public-capacity-report.sh`와 `make privacy-public-capacity-report` 경로를 추가했습니다.
- Aggregate report는 component report file SHA-256, component claim type, eligibility, commit, active set, artifact manifest checksum을 `component_reports`에 보존합니다.
- Component별 `claim_evidence`를 `claim_evidence_by_type`에 보존합니다.
- Multi-claim public gate에서 claim별 evidence를 서로 섞지 않고 평가합니다.
- Component benchmark rows와 fee rows는 aggregate report에 병합됩니다.
- Ineligible component는 `Blocked Components` 섹션에 별도로 표시됩니다.
- `error_rate=1`, `requests/sec=0`인 invalid measured bucket은 zero capacity가 아니라 invalid/blocked result로 표시됩니다.

### 완료 기준

- report가 public claim 문구와 non-claim 문구를 구분합니다.
- missing gate가 있으면 public claim section이 "not eligible"로 표시됩니다.
- smoke/reference/public claim 결과가 파일명과 report header에서 구분됩니다.
- per-claim evidence가 없는 multi-claim `public-capacity` report는 eligible로 승격되지 않습니다.

## 8. Phase C5: Benchmark 운영 연결

### 완료한 내용

- Release/versioning 문서와 scripts README에 public capacity benchmark 경로와 guard를 반영했습니다.
- Benchmark result를 PR/release review에서 비교할 수 있도록 result family, run profile, source provenance, artifact checksum, dirty state를 report에 포함했습니다.
- Dirty worktree 판단에서 generated benchmark result family와 로컬 build binary만 제한적으로 제외합니다.

### 완료 기준

- smoke/reference/public claim 결과가 같은 report schema로 기록됩니다.
- public claim run은 dirty worktree, missing evidence, missing source hash, malformed checksum을 차단합니다.
- Regression 비교는 blessed baseline report와 같은 machine profile, artifact set, Go version family, run profile에서만 의미가 있습니다.

## 9. 검증

완료 시점에 아래 검증을 수행했습니다.

- `go test ./...`
- `go test ./cmd/clairveil-proverload`
- `go test ./cmd/clairveil-benchreport ./cmd/clairveil-localnetload ./cmd/clairveil-userlatency`
- `bash -n scripts/privacy-user-latency-bench.sh scripts/privacy-localnet-tps-bench.sh scripts/privacy-proverd-load-bench.sh scripts/privacy-public-capacity-report.sh scripts/privacy-bench.sh scripts/privacy-bench-localnet.sh scripts/privacy-proverd-bench.sh`
- External `clairveil-proverd` smoke with generated valid requests
- `RUN_PROFILE=public_claim USER_LATENCY_REPEAT=1` guard smoke

## 10. 비범위

이번 완료 계획은 구현된 benchmark 하네스와 gate를 기록하는 문서입니다. Public capacity claim 산출을 위해 남은 운영/확장 작업은 `docs/clairveil-public-capacity-claim-execution-plan-kr.md`에서 관리합니다.
