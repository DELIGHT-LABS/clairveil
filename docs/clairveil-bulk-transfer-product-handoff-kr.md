# Clairveil Bulk Transfer Product Handoff

English version: [clairveil-bulk-transfer-product-handoff.md](clairveil-bulk-transfer-product-handoff.md)

이 handoff는 one-proof `BatchJoinSplit16x32` bulk transfer를 배포하기 위해 필요한
현재 product와 operations 작업을 정의합니다.

## 구현된 reference surface

Repository는 다음을 제공합니다.

- atomic reservation contract v3와 operation graph
- input 1..16개 / output 1..32개 batch planning
- one-proof prepare, prove, broadcast
- Cosmos `MsgBatchTransfer`와 ClairveilJS 0.3.1 EVM
  `singleProofBatchTransfer` 지원
- typed `privacy-scan-v2` output recovery
- item별 disclosure와 operation-evidence 검증
- bounded prover service와 single-endpoint privacy default
- SQLite/PostgreSQL reference persistence test
- restart/retry, lease expiry, relay, ambiguous broadcast recovery
- fixture와 live localnet release gate

## Product integration 요구사항

Product 팀은 다음을 구현합니다.

1. 각 row의 recipient, canonical amount/denom, disclosure policy, encryption
   target 검증
2. 승인 전 total, change, input/output capacity, atomicity 표시
3. 여러 독립 batch에 대한 명시적 승인
4. prepared payload/proof checkpoint 암호화
5. proof, broadcast, reconciliation 전체에서 linked input reservation 유지
6. complete matching operation evidence만으로 item 성공 판정
7. 권한 있는 operator에게 `OPERATION_STATE_MIXED`와
   `OPERATION_EVIDENCE_CONFLICT` detail 표시
8. 실제 wallet, prover, chain, storage 배포 test

## Backend와 operations 요구사항

Unique-active owner/nullifier constraint, durable queue, idempotent lease,
backup/restore, tenant isolation, retention을 갖춘 managed transactional store를
배포합니다. Audit log에는 witness와 disclosure secret을 기록하지 않습니다.

기본적으로 선택한 prover endpoint 하나만 사용합니다. 같은 witness를 다른
endpoint에 보내려면 모든 endpoint를 명시한 privacy opt-in이 필요합니다. TLS,
authentication, body/queue limit, process isolation, monitoring, incident
response를 구성합니다.

## Release gate

다음을 실행합니다.

```bash
make reservation-sql-integration
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
make release-check
```

Downstream production acceptance에는 target-chain gas calibration, staging
load/fault test, production database evidence, formal trusted setup과 audit,
signed artifact provenance, Cosmos/EVM product E2E가 추가로 필요합니다.

[Reference payroll product](clairveil-reference-payroll-product-kr.md),
[Rehearsal guide](clairveil-reference-payroll-rehearsal-kr.md),
[Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial-kr.md)을
참고합니다.
