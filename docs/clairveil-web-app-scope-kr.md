# Clairveil WebApp 범위

English version: [clairveil-web-app-scope.md](clairveil-web-app-scope.md)

이 문서는 `examples/clairveil-dapp`에서 배포하는 browser WebApp의 제품 경계를 고정합니다. Clairveil chain core나 ClairveilJS 전체 기능을 나열하는 문서가 아니라 제품 integration 범위입니다.

## 지원 사용자 흐름

현재 WebApp은 아래 privacy 흐름만 노출할 수 있습니다.

| 흐름 | Browser client surface | 필요한 결과 |
| --- | --- | --- |
| 설정과 sync | `buildRootSigningMessage`, `derivePrivacyAccount`, `scanWalletNotes` | account별 shielded identity를 만들고 검증된 scan cursor에 도달합니다. |
| Deposit | `prepareDeposit` | 제품이 제공한 `DepositCircuit` proof를 얻고 sign/broadcast한 뒤 생성 note를 scan합니다. |
| 단일 shielded transfer | `prepareTransfer` | planner가 필요로 하면 self-merge 단계를 포함한 일반 transfer operation 하나를 준비합니다. |
| Feature gate된 원자적 batch transfer | `prepareTransferBatch` | Cosmos에서만 1–16 input / 1–32 output `MsgBatchTransfer` 하나를 준비하고 한 transaction으로 sign/broadcast한 뒤 모든 payment output을 검증해야 성공으로 표시합니다. |
| Direct withdraw | `prepareWithdraw` | exact-match note 하나를 spend하고 sign/broadcast 및 nullifier reconciliation을 수행합니다. |
| Relayed withdraw | `prepareRelayWithdraw` | handoff 경계를 durable하게 기록하고 relayer 제출 뒤 expiry 또는 spent evidence까지 reconcile합니다. |
| Disclosure 검토 | `decodeUserDisclosure`, `decodeSelfViewDisclosure`, `decodeBatchSelfViewDisclosure`, `decodeAuditDisclosure` | digest가 verified인 경우에만 plaintext를 표시합니다. Batch self-view 검토는 complete validated `privacy-scan-v2` output을 읽고 각 output을 따로 검증해야 합니다. |

`prepareTransfer`가 기본 진입점입니다. Batch editor는 server-backed config가 `serverFeatures.batchTransfer`를 활성화한 경우에만 노출하며 EVM profile이나 일반 transfer의 자동 fallback으로 사용하지 않습니다.

EVM profile은 privacy precompile이 Clairveil 0.2 canonical ABI를 구현할 때만 사용할 수 있습니다. 즉 proof를 포함한 deposit, self-view disclosure와 absolute expiry를 보존하는 transfer, legacy output-note field가 없는 withdraw가 필요합니다. WebApp은 ABI fallback이나 dummy withdraw output을 지원하지 않습니다.

## 명시적으로 범위 밖인 기능

이번 WebApp release에는 아래 기능의 UI, route, 자동 fallback, background worker를 추가하지 않습니다.

- payroll, treasury allocation, recipient-file import, bulk payment review
- capacity를 초과한 batch의 자동 또는 조용한 분할
- 명시적 review와 feature gate가 없는 background batch 제출

Payroll과 unattended bulk orchestration은 유효한 core/reference integration contract이지만 browser 제품 경계 밖입니다.

## Release contract

WebApp은 ClairveilJS `0.2.x`와 아래 fixed privacy contract를 대상으로 합니다.

- note/disclosure envelope: `privacy-fixed-v1`
- transfer prepared payload/proof/prover envelope: `v5` / `v2` / `v2`
- withdraw와 relay handoff: `v2`
- 원자적 batch transfer: `BatchJoinSplit16x32`, `MsgBatchTransfer`
- preferred wallet scan: `privacy-scan-v2`

WebApp은 ClairveilJS 0.2를 새 persistence epoch으로 취급합니다. Pre-0.2 note cache, reservation, lease, relay snapshot, prepared payload, proof는 절대 decode/reuse하지 않습니다. Upgrade 동작은 [WebApp 저장소와 복구](clairveil-web-app-storage-recovery-kr.md)에 정의합니다.

## 구현 gate

각 흐름을 노출하기 전에 WebApp은 다음을 만족해야 합니다.

1. [`clairveil-web-client-config.schema.json`](schemas/clairveil-web-client-config.schema.json)으로 chain profile을 검증하고 duplicate profile ID나 active profile과 다른 flattened compatibility field(최상위 `keplrChainInfo` 포함)를 거부합니다. Active EVM profile에는 `keplrChainInfo`를 내보내지 않습니다. Legacy top-level EVM `accountPrefix`는 privacy-client input이 아닌 host metadata이므로 ClairveilJS에는 active profile의 prefix만 전달합니다.
2. Wallet/network identity를 확인하고 current on-chain circuit, audit, disclosure, asset, tree config를 query합니다. Static configuration은 consensus authority가 아닙니다.
3. [WebApp 저장소와 복구](clairveil-web-app-storage-recovery-kr.md)의 storage와 durable reservation recovery 규칙을 사용합니다.
4. [WebApp integration](clairveil-web-app-integration-kr.md)의 lifecycle/API 규칙을 사용합니다.
5. [WebApp 배포](clairveil-web-app-deployment-kr.md)의 browser/prover deployment boundary를 만족합니다.
6. Batch transfer는 payload/proof checkpoint를 암호화하고 wallet 승인 전에 total, change, input/output capacity, all-or-nothing 경계를 표시합니다. 여러 독립 원자 batch로 나누기 전에는 명시적 opt-in을 받고, typed output evidence와 모든 input nullifier가 reconcile된 항목만 성공으로 표시합니다.

## 일반 client 문서와의 관계

[Client API checklist](clairveil-client-api-checklist-kr.md)와 [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)는 다른 제품에 필요한 batch capability까지 포함한 Clairveil contract 전체를 설명합니다. Protocol semantic은 계속 그 문서들이 authority입니다. 현재 browser WebApp이 노출할 수 있는 기능 범위는 이 문서가 authority입니다.
