# Clairveil Public Capacity Claim 실행 계획

이 문서는 public capacity claim을 실제로 만들기 위해 남은 운영/확장 작업을 관리합니다.

`docs/clairveil-public-capacity-benchmark-followup-plan-kr.md`는 이미 구현된 benchmark 하네스와 gate를 기록합니다. 이 문서는 그 하네스를 사용해 공개 가능한 TPS, 운영 prover 처리량, 사용자 체감 지연 수치를 산출하기 위한 남은 일을 다룹니다.

## 1. 목표

아래 세 가지 public claim을 각각 독립적으로 말할 수 있게 합니다.

1. Prover RPS: production-like `clairveil-proverd`가 유지할 수 있는 proof requests/sec.
2. Chain TPS: localnet 또는 staging chain에서 shielded tx가 end-to-end로 포함되는 sustained TPS.
3. User latency: wallet 사용자가 prepare, prove, submit, inclusion까지 체감하는 지연 시간.

공개 claim은 반드시 `public_claim` run profile과 eligible report를 사용합니다. Smoke/reference 결과는 진단용으로만 사용합니다.

## 2. 공통 실행 원칙

- 모든 public claim run은 source commit, artifact manifest checksum, active circuit set, dirty state, machine profile, OS/arch, Go version을 기록합니다.
- Artifact descriptor completeness와 실제 artifact file checksum verification이 통과해야 합니다.
- Public claim evidence file은 `source_files`와 `source_file_sha256`에 포함되어야 합니다.
- `run_ended_at - run_started_at`은 `claim_evidence.steady_state_seconds` 이상이어야 합니다.
- Positive metric은 `mean`, `p50`, `p95`, `p99`, `min`, `max`가 모두 양수여야 합니다.
- Error/failure/timeout/cancel rate는 0 이상이며 명시된 SLO 이하이어야 합니다.
- Missing evidence는 낮은 성능 수치가 아니라 ineligible blocker로 해석합니다.

## 3. Track A: Prover RPS Public Claim

목표: 실제 `clairveil-proverd` 프로세스가 production-like 환경에서 유지할 수 있는 requests/sec와 latency를 측정합니다.

### 남은 작업

- Public claim용 varied request pool을 생성합니다.
  - varied input notes
  - varied Merkle roots and paths
  - varied disclosure policy
  - varied payload expiry
  - transfer and withdraw route 모두 포함
- Request pool 생성 산출물의 SHA-256을 evidence로 기록합니다.
- Production-like `clairveil-proverd` config file을 고정하고 SHA-256을 기록합니다.
- Strict preflight와 artifact checksum verification을 활성화합니다.
- Bearer token 또는 운영 auth 설정을 활성화하고 `auth_enabled=true`로 기록합니다.
- Concurrency sweep을 최소 두 bucket 이상 실행합니다.
  - 권장: `1,2,4,8,16,32`
- 각 bucket은 warmup 30-60초, steady-state 10분 이상으로 실행합니다.
- `/debug/vars` telemetry를 수집해 CPU/RSS/max RSS/goroutine 추이를 기록합니다.
- Saturation profile evidence file을 작성합니다.
  - max sustainable RPS
  - overload region
  - chosen public claim point
  - p99 latency SLO
  - error rate SLO

### 산출물

- `benchmarks/privacy-proverd-load/latest.json`
- `benchmarks/privacy-proverd-load/latest.md`
- prover config file and SHA-256
- saturation profile file and SHA-256
- request pool manifest and SHA-256
- artifact manifest and verified artifact file checksums

### 완료 기준

- `claim_profile.run_profile=public_claim`
- `claim_profile.claim_types=["prover_rps"]`
- `claim_profile.eligible=true`
- 최소 2개 이상 concurrency bucket
- `requests/sec`, `latency_ms`, `error_rate`, `timeout_rate`, `cpu_percent`, `rss_bytes` 또는 `max_rss_bytes` 포함
- p99 latency가 SLO 이하
- error rate가 SLO 이하
- RSS가 steady-state 동안 계속 증가하지 않음

## 4. Track B: Chain TPS Public Claim

목표: chain tx path에서 shielded tx의 sustained TPS와 inclusion latency를 측정합니다.

### 남은 작업

- Open-loop 또는 batch tx runner를 구현하거나 staging runner output을 현재 `tx_metrics` schema로 feed합니다.
- Account sequence contention을 피하기 위해 producer별 sender account pool을 준비합니다.
- Sender funding과 key generation을 자동화합니다.
- Shielded state를 tx type별로 준비합니다.
  - transfer 전 spendable note pool
  - withdraw 전 exact-match note pool
  - dummy note pool
- Tx mix profile을 분리합니다.
  - `deposit_only`
  - `transfer_only`
  - `withdraw_only`
  - `mixed_deposit_transfer_withdraw`
- 최소 두 개 이상의 target tx/sec bucket을 실행합니다.
- 각 bucket은 10분 이상 steady-state를 유지합니다.
- Before/after reserve snapshot을 저장하고 SHA-256을 evidence로 기록합니다.
- Chain config file과 SHA-256을 evidence로 기록합니다.
  - block time
  - minimum gas price
  - mempool config
  - validator count
  - app commit
  - artifact manifest checksum
- Throughput window와 saturation profile evidence file을 작성합니다.
- Positive inclusion latency source를 확보합니다. Coarse CLI timestamp로 모두 0ms가 나오는 결과는 public latency evidence로 사용하지 않습니다.

### 산출물

- `benchmarks/privacy-localnet-tps/latest.json`
- `benchmarks/privacy-localnet-tps/latest.md`
- chain config file and SHA-256
- before/after reserve snapshot files and SHA-256
- saturation profile file and SHA-256
- tx metrics bucket input and SHA-256

### 완료 기준

- `claim_profile.run_profile=public_claim`
- `claim_profile.claim_types=["chain_tps"]`
- `claim_profile.eligible=true`
- 최소 2개 이상 target tx/sec bucket
- `submitted_tx/sec`, `accepted_tx/sec`, `included_tx/sec`, `successful_tx/sec`, `failed_tx_rate`, `inclusion_latency_ms`, `gas_used` 포함
- reserve invariant true
- failed tx rate가 SLO 이하
- p95 inclusion latency가 SLO 이하
- target tx/sec 증가에 따른 saturation point가 관측됨

## 5. Track C: User Latency Public Claim

목표: 사용자가 실제로 체감하는 wallet flow latency를 native, remote, browser/WASM mode별로 분리해 측정합니다.

### Native CLI

- `USER_LATENCY_REPEAT>=100`으로 flow/mode/cold-warm bucket별 sample floor를 만족시킵니다.
- `USER_LATENCY_FLOW_FILTER`로 flow profile을 분리합니다.
- `CLAIM_LATENCY_MODE=native`를 사용합니다.
- cold/warm 결과를 섞지 않습니다.
- Inclusion latency를 주장하려면 tx hash와 positive inclusion latency source를 함께 확보합니다.

### Remote Prover

- 실제 remote prover client trace를 같은 trace schema로 수집합니다.
- Remote topology를 evidence로 기록합니다.
- Prover config file/SHA-256을 evidence로 기록합니다.
- Eligible `prover_rps` public claim report를 `CLAIM_LINKED_PROVER_REPORT_FILE`로 연결합니다.
- Linked prover report는 instance profile, prover config SHA-256, active set, artifact manifest SHA-256이 현재 user latency report와 일치해야 합니다.

### Browser/WASM

- Browser/WASM prover adapter를 준비합니다.
- Browser/device matrix를 명시합니다.
- Adapter version, adapter file, adapter SHA-256을 evidence로 기록합니다.
- `browser_adapter_ready=true`가 아니면 browser latency public claim으로 승격하지 않습니다.
- Chrome desktop, Safari desktop, Firefox desktop을 우선 matrix로 둡니다.

### 산출물

- `benchmarks/privacy-user-latency/latest.json`
- `benchmarks/privacy-user-latency/latest.md`
- native/remote/browser mode별 trace file and SHA-256
- browser adapter file and SHA-256, if browser mode
- linked eligible prover RPS report and SHA-256, if remote mode
- chain config file and SHA-256, if inclusion latency is claimed

### 완료 기준

- `claim_profile.run_profile=public_claim`
- `claim_profile.claim_types=["user_latency"]`
- `claim_profile.eligible=true`
- flow/mode/cold-warm bucket별 100 samples 이상
- `prepare_latency_ms`, `proof_latency_ms`, `time_to_submit_ms`, `submit_ready_ms` 또는 `total_latency_ms`, `error_rate`, `timeout_rate` 또는 `cancel_rate` 포함
- p99 latency가 SLO 이하
- error/timeout/cancel rate가 SLO 이하
- browser claim은 browser adapter evidence 포함
- remote claim은 eligible linked prover RPS report 포함

## 6. Track D: Aggregate Public Capacity Report

목표: Prover RPS, Chain TPS, User latency 결과를 하나의 public capacity report로 묶습니다.

### 남은 작업

- 각 component report가 먼저 `public_claim` eligible인지 확인합니다.
- `scripts/privacy-public-capacity-report.sh` 또는 `make privacy-public-capacity-report`로 aggregate를 생성합니다.
- Aggregate report에 component report file SHA-256, claim type, eligibility, commit, active set, artifact manifest checksum을 보존합니다.
- `claim_evidence_by_type`에 claim별 evidence가 들어가는지 확인합니다.
- Ineligible component가 있으면 public capacity claim으로 사용하지 않습니다.
- Native circuit baseline은 claim component가 아니라 참고 baseline으로만 연결합니다.

### 산출물

- `benchmarks/public-capacity/latest.json`
- `benchmarks/public-capacity/latest.md`
- component report files and SHA-256
- public release note 또는 benchmark result report

### 완료 기준

- Aggregate `claim_profile.eligible=true`
- 모든 component report가 eligible
- component source hash와 aggregate source hash가 일관됨
- active set과 artifact manifest checksum이 component 간 일치
- public claim 문구와 non-claim 문구가 Markdown에서 분리됨

## 7. 권장 작업 순서

1. Track A: Prover RPS public claim용 varied request pool과 10분 concurrency sweep을 먼저 완료합니다.
2. Track D: Prover RPS 단일 claim aggregate smoke를 생성해 evidence plumbing을 검증합니다.
3. Track B: Chain TPS open-loop/batch runner와 reserve snapshot evidence를 완성합니다.
4. Track C: Native user latency 100-sample public run을 먼저 완료합니다.
5. Track C: Remote prover latency를 eligible prover RPS report와 연결합니다.
6. Track C: Browser/WASM adapter가 준비되면 browser matrix를 추가합니다.
7. Track D: 세 claim을 모두 묶은 final public capacity report를 생성합니다.

## 8. 공개 문구 기준

사용 가능한 표현:

- "Production-like prover instance profile P에서 transfer proof endpoint는 10분 동안 RPS X, p95 Y ms, error rate Z%를 유지했습니다."
- "Staging chain config C에서 mixed shielded workload sustained TPS는 window avg X이고 p95 inclusion latency는 Y ms입니다."
- "Desktop Chrome warm remote-prover transfer flow의 submit-ready p95는 X ms입니다."

사용 금지:

- "Native proving 역수 기준으로 chain TPS는 X입니다."
- "`httptest` transport benchmark 기준으로 운영 prover RPS는 X입니다."
- "Localnet smoke 결과 기준으로 production capacity는 X입니다."
- "Browser/WASM adapter 없이 browser user latency를 공개 claim으로 말합니다."
- "Ineligible aggregate report의 blocked component를 zero capacity로 해석합니다."

## 9. 종료 조건

이 실행 계획은 아래 조건을 만족하면 완료로 봅니다.

- `prover_rps`, `chain_tps`, `user_latency` 각각에 대해 eligible public claim report가 있습니다.
- 세 claim을 묶은 `public-capacity` aggregate report가 eligible입니다.
- 모든 public 숫자에 source commit, artifact manifest checksum, machine profile, config SHA-256, evidence SHA-256이 연결되어 있습니다.
- 공개 문서에는 smoke/reference/production-like claim의 의미가 구분되어 있습니다.
