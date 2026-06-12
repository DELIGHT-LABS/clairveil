# Clairveil Public Capacity Benchmark 후속 계획

이 문서는 현재 `make privacy-bench`, `make privacy-proverd-bench`, `make privacy-bench-localnet`가 제공하는 smoke/reference benchmark를 public capacity claim에 사용할 수 있는 수준으로 확장하기 위한 후속 계획입니다.

목표는 아래 세 가지 claim을 분리해서 말할 수 있게 하는 것입니다.

1. Chain TPS: 특정 localnet 또는 staging chain config에서 shielded tx가 end-to-end로 처리되는 지속 처리량.
2. Prover throughput: 운영형 `clairveil-proverd` 배포가 성공적으로 생성할 수 있는 proofs/sec 또는 requests/sec.
3. User perceived latency: wallet 사용자가 proof 생성, remote proving, broadcast, inclusion까지 경험하는 지연 시간.

## 1. Claim 원칙

- Circuit proving benchmark의 단순 역수를 chain TPS로 말하지 않습니다.
- In-process `httptest` transport benchmark를 운영형 prover RPS로 말하지 않습니다.
- Localnet smoke flow의 gas/fee 결과를 sustained TPS로 말하지 않습니다.
- User latency는 native Go proof latency, browser/WASM proof latency, remote prover latency, broadcast/inclusion latency를 분리해서 기록합니다.
- 공개 숫자는 source commit, artifact manifest checksum, dirty state, machine profile, chain config, prover config, load profile을 함께 공개할 때만 사용합니다.

## 2. Public Claim Gate

아래 gate를 모두 통과한 결과만 public capacity claim으로 사용할 수 있습니다.

| Claim | 필수 gate | 공개 가능한 표현 |
| --- | --- | --- |
| Chain TPS | batch localnet/staging runner, fixed chain config, 10분 이상 steady-state, reserve invariant true, failed/retried tx 분리 | "Config A에서 shielded transfer sustained TPS p50/p95는 X/Y입니다." |
| Prover RPS | 실제 `clairveil-proverd` 프로세스, external client load, concurrency sweep, p50/p95/p99, error rate, CPU/RSS 기록 | "Instance profile B에서 transfer proof RPS는 X이고 p95 latency는 Y입니다." |
| User latency | wallet flow별 cold/warm run, local/remote/browser mode 분리, broadcast/inclusion 포함 여부 명시 | "Desktop Chrome warm proof 기준 transfer submit-ready p95는 X ms입니다." |

Public claim 결과에는 반드시 "reference environment" 또는 "production-like environment"를 붙입니다. production capacity라고 말하려면 production과 같은 instance class, artifact set, auth/rate-limit 설정, chain block config에서 측정해야 합니다.

## 3. Phase C0: Claim Schema와 Report Gate

목적: benchmark 결과가 public claim으로 승격 가능한지 machine-readable하게 판단합니다.

구현 항목:

- `capacity_claim` section을 benchmark JSON에 추가합니다.
- claim type을 명시합니다: `chain_tps`, `prover_rps`, `user_latency`.
- run profile을 명시합니다: `smoke`, `reference`, `production_like`, `public_claim`.
- report generator가 public claim에 필요한 필드가 없으면 `claim_eligible=false`로 표시합니다.
- Markdown report에 "Eligible for public claim: yes/no"를 출력합니다.

필수 JSON 필드:

```json
{
  "claim_profile": {
    "run_profile": "public_claim",
    "claim_types": ["chain_tps", "prover_rps"],
    "eligible": false,
    "blocking_reasons": []
  },
  "environment": {
    "machine_profile": "",
    "cpu_governor": "",
    "memory_gib": "",
    "os": "",
    "arch": ""
  },
  "artifact_set": {
    "active_set_id": "",
    "manifest_sha256": "",
    "r1cs_sha256": "",
    "pk_sha256": "",
    "vk_sha256": ""
  }
}
```

완료 기준:

- smoke 결과는 자동으로 `claim_eligible=false`가 됩니다.
- public claim 결과는 필수 metadata 누락 시 report generation이 실패하거나 blocking reason을 기록합니다.

## 4. Phase C1: 운영형 `clairveil-proverd` External Load Benchmark

목적: 실제 daemon/process 형태의 remote prover capacity를 측정합니다.

현재 `make privacy-proverd-bench`는 `httptest` 기반 in-process transport benchmark입니다. 후속 작업은 실제 `clairveil-proverd` 바이너리를 별도 프로세스로 띄우고 external HTTP client로 부하를 걸어야 합니다.

구현 항목:

- `scripts/privacy-proverd-load-bench.sh` 추가
- `cmd/clairveil-proverload` 또는 동등한 Go load generator 추가
- `clairveil-proverd`를 strict preflight, fixed artifact dir, optional bearer token으로 실행
- fixture-backed prepared transfer/withdraw payload를 N개 생성하거나 conformance fixture를 반복 사용
- route별 load profile:
  - `transfer_only`
  - `withdraw_only`
  - `mixed_80_20`
- concurrency sweep:
  - `1, 2, 4, 8, 16, 32`
- measurement mode:
  - warmup: 30-60초
  - steady-state: 최소 5분, public claim은 10분 이상
  - cooldown: 30초
- metrics:
  - successful proofs/sec
  - p50/p95/p99 latency
  - error rate
  - timeout rate
  - request/response bytes
  - CPU percent
  - RSS/max RSS
  - goroutine count if exposed

완료 기준:

- `benchmarks/privacy-proverd-load/latest.json` 생성
- concurrency별 latency/RPS/error table 생성
- p99와 error rate가 public report에 포함
- artifact preflight mode와 checksum이 report에 포함
- auth enabled/disabled 여부가 report에 포함

Public claim gate:

- error rate <= 0.1% 또는 명시된 SLO 이하
- p99 latency가 SLO 이하
- RSS가 steady-state 동안 계속 증가하지 않음
- saturation point를 "max sustainable RPS"와 "overload region"으로 분리

## 5. Phase C2: Chain TPS Batch Runner

목적: chain tx path에서 sustained TPS를 측정합니다.

현재 `make privacy-bench-localnet`는 e2e smoke 기반 gas/fee/invariant check입니다. Chain TPS를 말하려면 batch runner가 필요합니다.

구현 항목:

- `scripts/privacy-localnet-tps-bench.sh` 추가
- batch runner 추가:
  - option A: Go command `cmd/clairveil-localnetload`
  - option B: shell + CLI wrapper, 단 account sequence 처리 안정성이 낮으므로 Go command 권장
- tx mix profiles:
  - `deposit_only`
  - `transfer_only`
  - `withdraw_only`
  - `mixed_deposit_transfer_withdraw`
- sender account pool:
  - account sequence contention 방지를 위해 tx producer별 account 분리
  - funding and key generation 자동화
- shielded state preparation:
  - transfer batch 전에 spendable notes pool 생성
  - withdraw batch 전에 exact-match notes pool 생성
  - dummy note pool 관리
- load modes:
  - closed-loop: 각 worker가 tx inclusion까지 기다린 뒤 다음 tx 제출
  - open-loop: target tx/s rate로 제출하고 inclusion lag를 별도 측정
- metrics:
  - submitted tx/sec
  - accepted tx/sec
  - included tx/sec
  - successful shielded tx/sec
  - failed tx count and reason
  - retried tx count
  - block inclusion latency p50/p95/p99
  - gas used p50/p95
  - block height/time distribution
  - mempool rejected tx count
  - reserve invariant before/after

완료 기준:

- `benchmarks/privacy-localnet-tps/latest.json` 생성
- tx type별 TPS와 mixed workload TPS가 분리됨
- failed/retried tx가 성공 처리량에 섞이지 않음
- benchmark 전후 `query privacy reserve {denom}` snapshot이 포함됨
- public report에 chain config가 포함됨:
  - block time
  - minimum gas price
  - mempool config
  - validator count
  - app commit
  - artifact manifest checksum

Public claim gate:

- 10분 이상 steady-state run
- reserve invariant true
- failed tx가 SLO 이하
- p95 inclusion latency가 명시된 SLO 이하
- target tx/s를 올렸을 때 saturation point가 관측되어야 함

## 6. Phase C3: User Perceived Latency Benchmark

목적: 사용자가 실제로 체감하는 wallet flow latency를 측정합니다.

사용자 latency는 아래 네 구간을 분리해서 기록해야 합니다.

1. prepare latency: note scan, path lookup, payload build
2. prove latency: local native, browser/WASM, 또는 remote prover
3. submit latency: tx signing and broadcast
4. inclusion latency: tx가 block에 포함되기까지의 시간

구현 항목:

- `benchmarks/privacy-user-latency` result family 추가
- CLI/native mode:
  - local proof
  - remote prover proof
- browser/WASM mode:
  - Chrome desktop
  - Safari desktop
  - Firefox desktop
  - mobile device class는 별도 phase로 분리 가능
- cold/warm 구분:
  - cold: process/browser start, artifact/WASM first load 포함
  - warm: artifact/WASM loaded, wallet state prepared
- flow profiles:
  - deposit
  - transfer all-private
  - transfer with disclosure
  - withdraw direct
  - relayed withdraw

metrics:

- prepare p50/p95/p99
- proof p50/p95/p99
- time-to-submit p50/p95/p99
- time-to-inclusion p50/p95/p99
- total user-visible latency p50/p95/p99
- timeout/cancel rate
- browser heap peak if browser/WASM

완료 기준:

- native CLI latency report와 browser latency report가 분리됨
- remote prover latency report가 prover RPS benchmark의 instance profile과 연결됨
- "submit-ready latency"와 "included latency"가 별도 표로 출력됨

Public claim gate:

- cold/warm 결과를 섞지 않음
- browser/device matrix를 명시
- remote prover topology를 명시
- inclusion latency는 chain config와 함께만 공개

## 7. Phase C4: Capacity Report Generator 확장

목적: 여러 benchmark family를 하나의 public report로 결합합니다.

입력:

- `privacy-circuits/latest.json`
- `privacy-proverd-load/latest.json`
- `privacy-localnet-tps/latest.json`
- `privacy-user-latency/latest.json`
- artifact manifest
- reserve snapshots

출력:

```text
benchmarks/public-capacity/latest.json
benchmarks/public-capacity/latest.md
benchmarks/public-capacity/<date>-<commit>.json
benchmarks/public-capacity/<date>-<commit>.md
```

Markdown report 구성:

1. Claim eligibility summary
2. Environment and artifact set
3. Native circuit baseline
4. Prover production-like capacity
5. Chain TPS
6. User perceived latency
7. Fee/gas
8. Reserve/accounting invariant
9. Known limits and non-claims

완료 기준:

- report가 public claim 문구와 non-claim 문구를 자동 생성
- missing gate가 있으면 public claim section이 "not eligible"로 표시됨
- smoke/reference/public claim 결과가 파일명과 report header에서 구분됨

## 8. Phase C5: Benchmark 운영 절차

목적: benchmark가 한 번의 수동 실험이 아니라 release/review 프로세스가 되게 합니다.

운영 규칙:

- release candidate마다 같은 machine profile에서 benchmark 실행
- `benchstat` 설치를 benchmark runner prereq로 고정
- public claim run은 dirty worktree 금지
- benchmark artifact는 source commit, artifact manifest checksum, Docker image digest와 함께 보존
- 결과 regression threshold:
  - proving latency +10% 이상 악화 시 review
  - prover p95 latency +15% 이상 악화 시 review
  - chain TPS -10% 이상 하락 시 review
  - user latency p95 +15% 이상 악화 시 review

완료 기준:

- `docs/clairveil-release-versioning-policy-kr.md`에 public capacity benchmark checklist 추가
- release handoff pack에 latest public capacity report 링크 추가
- benchmark result를 PR/release review에서 비교할 수 있음

## 9. 권장 작업 순서

1. Phase C0: claim schema와 gate 추가
2. Phase C1: external `clairveil-proverd` load benchmark
3. Phase C2: localnet TPS batch runner
4. Phase C4: public report generator 통합
5. Phase C3: user latency native CLI benchmark
6. Phase C3 확장: browser/WASM benchmark
7. Phase C5: release process에 연결

이 순서를 권장하는 이유는 prover capacity와 chain TPS가 public claim의 핵심이고, user latency는 chain/prover topology가 고정되어야 해석이 가능하기 때문입니다.

## 10. 최종 공개 문구 예시

사용 가능:

- "Reference workstation native benchmark에서 JoinSplit proof generation p50은 X ms입니다."
- "Production-like prover instance profile P에서 transfer proof endpoint는 RPS X, p95 Y ms, error rate Z%를 10분 동안 유지했습니다."
- "Single-validator localnet config L에서 mixed shielded workload sustained TPS는 X이고 p95 inclusion latency는 Y ms입니다."
- "Desktop Chrome warm remote-prover flow에서 transfer submit-ready p95는 X ms입니다."

사용 금지:

- "Native proving 역수 기준으로 chain TPS는 X입니다."
- "`httptest` transport benchmark 기준으로 운영 prover RPS는 X입니다."
- "Localnet smoke gas/fee 결과 기준으로 production capacity는 X입니다."
- "Warm desktop result를 mobile cold-start user latency로 표현합니다."

