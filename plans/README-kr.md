# Clairveil 계획 상태 Index

> English version: [README.md](README.md)

`plans/`에는 구현 의도, 순서, acceptance criteria, 유용한 completion ledger를 둡니다. 현재 product/protocol 지식은 `docs/`에 둡니다. 완료된 plan은 decision 또는 gate evidence가 계속 유용할 때만 tracked 상태로 유지하고, superseded/duplicate working plan은 ignored `tmpdocs/` archive로 이동합니다.

## 상태 규칙

- **Active**: 남은 작업과 exit criteria가 아직 구현을 지배합니다.
- **Completed record**: 구현/gate가 끝났습니다. 현재 동작은 current `docs/`와 코드를 사용합니다.
- **Implemented design**: 배포된 reference design과 제외된 future scope를 설명합니다.
- **Archived**: superseded, duplicate, temporary 상태이며 authoritative하지 않고 release pack에도 포함되지 않습니다.

구현 상태가 바뀌면 plan status, owner/scope, superseding document를 함께 갱신합니다. 최상위 지식 문서는 English/Korean pair가 필요하지만 historical plan은 index에 명시하면 Korean-only로 유지할 수 있습니다.

## Active

| Plan | 상태 | 현재 exit boundary |
| --- | --- | --- |
| [Public capacity claim 실행](clairveil-public-capacity-claim-execution-plan-kr.md) | Active; Korean-only | Harness 측정을 근거 없는 production claim으로 바꾸지 않고 eligible operational prover RPS, chain TPS, user-latency evidence 생성 |

## 완료된 prover API record

| Plan | 상태 |
| --- | --- |
| [`clairveil-proverd` deposit API 구현](clairveil-proverd-deposit-api-implementation-plan-kr.md) | Completed record; Korean-only; circuit, proto, artifact contract, DApp 변경 없이 language-neutral `POST /v1/prover/deposit`, 네 route 공통 HTTP policy, conformance/schema/docs와 모든 최종 gate 완료 |

## 완료된 deposit funder record

| Plan | 상태 |
| --- | --- |
| [Deposit actor/funder 분리 구현](clairveil-deposit-funder-implementation-plan-kr.md) | Complete implementation record; Korean-only; 현재 reviewed checkout은 self-funding 보강을 포함하며 downstream용 immutable tag/commit 제공은 pending |
| [Deposit actor/funder 분리 변경 요청](clairveil-deposit-funder-separation-handoff-kr.md) | Fulfilled by implementation plan; downstream EVM snapshot e2e는 downstream repository 책임 |

## 완료된 batch program record

| Plan | 상태 |
| --- | --- |
| [BatchJoinSplit16x32 master roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md) | Complete; Session 4 PASS, `PUBLICATION_READY_EXPERIMENTAL` |
| [Session 1 security remediation](clairveil-batch-joinsplit-16x32-session-1-security-remediation-kr.md) | Gate 1 PASS |
| [Session 2 foundation](clairveil-batch-joinsplit-16x32-session-2-foundation-kr.md) | Gate 2 PASS |
| [Session 3A circuit/chain core](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md) | Gate 3A PASS |
| [Session 3B integration](clairveil-batch-joinsplit-16x32-session-3b-integration-kr.md) | Gate 3B PASS |
| [Session 4 publication validation](clairveil-batch-joinsplit-16x32-session-4-publication-validation-kr.md) | Complete; experimental source-publication gate PASS |

현재 batch 동작은 `docs/clairveil-batch-joinsplit-16x32*.md`, `docs/clairveil-batch-transfer-integration-handoff*.md`, current code에 있습니다. 이 plan들은 historical gate ledger이며 API reference와 경쟁하지 않습니다.

## 완료된 benchmark record

| Plan | 상태 |
| --- | --- |
| [Public benchmark plan](clairveil-public-benchmark-plan-kr.md) | Harness/design record 완료. Operational public claim은 active execution plan에서 관리 |
| [Public capacity benchmark follow-up](clairveil-public-capacity-benchmark-followup-plan-kr.md) | Completed implementation/gate record |

## 구현된 reference design

| Plan | English | 한국어 | 상태 |
| --- | --- | --- | --- |
| Note scan optimization | [English](clairveil-scan-optimization-implementation-plan.md) | [한국어](clairveil-scan-optimization-implementation-plan-kr.md) | Implemented; future exclusion 유지 |
| `clairveild` reference app | [English](clairveild-reference-app-plan.md) | [한국어](clairveild-reference-app-plan-kr.md) | Implemented reference-host design |

## Archive 경계

Superseded phase-1 bulk-transfer 파일 `clairveil-bulk-transfer-implementation-plan-kr.md`, `clairveil-bulk-transfer-strategy-kr.md`, `clairveil-bulk-transfer-time-simulation-kr.md`는 `tmpdocs/archived-plans/`로 이동했습니다. Release input이나 current protocol guidance가 아닙니다. 위 batch program record와 `docs/`의 durable bulk/payroll 지식을 사용하세요.
