# Clairveil Plan Status Index

> Korean version: [README-kr.md](README-kr.md)

`plans/` contains implementation intent, sequencing, acceptance criteria, and useful completion ledgers. Current product/protocol knowledge belongs in `docs/`. A plan remains tracked after completion only when its decisions or gate evidence are still useful; superseded or duplicate working plans move to the ignored `tmpdocs/` archive.

## Status Rules

- **Active**: remaining work and exit criteria still govern implementation.
- **Completed record**: implementation/gate is complete; use current `docs/` and code for behavior.
- **Implemented design**: the plan explains a shipped reference design and its excluded future scope.
- **Archived**: superseded, duplicate, or temporary; not authoritative and not part of a release pack.

Plan status, owner/scope, and superseding document should be updated when the implementation state changes. Top-level knowledge documents require English/Korean pairs; historical plans may be Korean-only when they are explicitly indexed as such.

## Active

| Plan | Status | Current exit boundary |
| --- | --- | --- |
| [Public capacity claim execution](clairveil-public-capacity-claim-execution-plan-kr.md) | Active; Korean-only | Produce eligible operational prover RPS, chain TPS, and user-latency claim evidence without converting harness measurements into unsupported production claims |

## Completed Deposit Funder Record

| Plan | Status |
| --- | --- |
| [Deposit actor/funder separation implementation](clairveil-deposit-funder-implementation-plan-kr.md) | Complete implementation record; Korean-only; current reviewed checkout includes self-funding hardening, while an immutable downstream tag/commit remains pending |
| [Deposit actor/funder separation request](clairveil-deposit-funder-separation-handoff-kr.md) | Fulfilled by the implementation plan; downstream EVM snapshot e2e remains the downstream repository's responsibility |

## Completed Batch Program Records

| Plan | Status |
| --- | --- |
| [BatchJoinSplit16x32 master roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md) | Complete; Session 4 PASS, `PUBLICATION_READY_EXPERIMENTAL` |
| [Session 1 security remediation](clairveil-batch-joinsplit-16x32-session-1-security-remediation-kr.md) | Gate 1 PASS |
| [Session 2 foundation](clairveil-batch-joinsplit-16x32-session-2-foundation-kr.md) | Gate 2 PASS |
| [Session 3A circuit/chain core](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md) | Gate 3A PASS |
| [Session 3B integration](clairveil-batch-joinsplit-16x32-session-3b-integration-kr.md) | Gate 3B PASS |
| [Session 4 publication validation](clairveil-batch-joinsplit-16x32-session-4-publication-validation-kr.md) | Complete; experimental source-publication gate PASS |

Current batch behavior is documented in `docs/clairveil-batch-joinsplit-16x32*.md`, `docs/clairveil-batch-transfer-integration-handoff*.md`, and the current code. These plans are historical gate ledgers, not a competing API reference.

## Completed Benchmark Records

| Plan | Status |
| --- | --- |
| [Public benchmark plan](clairveil-public-benchmark-plan-kr.md) | Harness/design record complete; operational public claims remain in the active execution plan |
| [Public capacity benchmark follow-up](clairveil-public-capacity-benchmark-followup-plan-kr.md) | Completed implementation/gate record |

## Implemented Reference Designs

| Plan | English | 한국어 | Status |
| --- | --- | --- | --- |
| Note scan optimization | [English](clairveil-scan-optimization-implementation-plan.md) | [한국어](clairveil-scan-optimization-implementation-plan-kr.md) | Implemented; future exclusions retained |
| `clairveild` reference app | [English](clairveild-reference-app-plan.md) | [한국어](clairveild-reference-app-plan-kr.md) | Implemented reference-host design |

## Archive Boundary

The superseded phase-1 bulk-transfer files `clairveil-bulk-transfer-implementation-plan-kr.md`, `clairveil-bulk-transfer-strategy-kr.md`, and `clairveil-bulk-transfer-time-simulation-kr.md` were moved to `tmpdocs/archived-plans/`. They are not release inputs or current protocol guidance. Use the batch program records above and the durable bulk/payroll knowledge under `docs/`.
