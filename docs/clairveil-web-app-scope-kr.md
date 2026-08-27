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
| Direct withdraw | `prepareWithdraw` | exact-match note 하나를 spend하고 sign/broadcast 및 nullifier reconciliation을 수행합니다. |
| Relayed withdraw | `prepareRelayWithdraw` | handoff 경계를 durable하게 기록하고 relayer 제출 뒤 expiry 또는 spent evidence까지 reconcile합니다. |
| Disclosure 검토 | `decodeUserDisclosure`, `decodeSelfViewDisclosure`, `decodeAuditDisclosure` | digest가 verified인 경우에만 plaintext를 표시합니다. |

`prepareTransfer`가 transfer 진입점입니다. Checked-in v0.3.1 example은
one-proof batch prepare/submission을 노출하지 않습니다. Server는
`serverFeatures.batchTransfer=false`를 반환하고 UI는 `prepareTransferBatch`를
호출하지 않습니다. ClairveilJS와 chain core가 별도로 검토된
product를 위한 하위 batch contract를 제공할 수는 있지만 config 값만으로
그 contract를 WebApp feature로 바꾸면 안 됩니다.

EVM profile은 privacy precompile이 Clairveil 0.3.1 canonical ABI를 구현할 때만 사용할 수 있습니다. 즉 proof를 포함한 deposit, self-view disclosure와 absolute expiry를 보존하는 transfer, legacy output-note field가 없는 withdraw가 필요합니다. WebApp은 ABI fallback이나 dummy withdraw output을 지원하지 않습니다.

## 명시적으로 범위 밖인 기능

이번 WebApp release에는 아래 기능의 UI, route, 자동 fallback, background worker를 추가하지 않습니다.

- payroll, treasury allocation, recipient-file import, bulk payment review
- one-proof batch-transfer prepare, durable checkpoint, submission UI
- 어떤 batch든 자동·조용한 분할 또는 background 제출

Payroll과 unattended bulk orchestration은 유효한 core/reference integration contract이지만 browser 제품 경계 밖입니다.

## Release contract

WebApp은 ClairveilJS `0.3.1`과 아래 fixed privacy contract를 대상으로 합니다.

- note/disclosure envelope: `privacy-fixed-v1`
- transfer prepared payload/proof/prover envelope: `v5` / `v2` / `v2`
- withdraw와 relay handoff: `v2`
- protocol identity: `batch-joinsplit-16x32-v1`을 required 네 번째
  chain-core circuit으로 포함하는 `privacy-note-v1`
- preferred wallet scan: `privacy-scan-v2`

아직 release되지 않은 이 WebApp은 current v0.3.1 namespace와 current lifecycle
schema에서 초기화한 fresh state만 지원합니다. 이전 browser cache나
lifecycle store에서의 migration 또는 in-place downgrade contract는 없습니다.
오래된 note cache, reservation, lease, relay snapshot, prepared payload, proof를 current
state로 decode하지 말고 empty current namespace를 초기화한 뒤 full scan을
수행합니다. 전체 정책은 [WebApp 저장소와 복구](clairveil-web-app-storage-recovery-kr.md)에
정의합니다.

## 구현 gate

각 흐름을 노출하기 전에 WebApp은 다음을 만족해야 합니다.

1. [`clairveil-web-client-config.schema.json`](schemas/clairveil-web-client-config.schema.json)으로 chain profile을 검증하고 duplicate profile ID나 active profile과 다른 flattened compatibility field(최상위 `keplrChainInfo` 포함)를 거부합니다. Active EVM profile에는 `keplrChainInfo`를 내보내지 않습니다. Legacy top-level EVM `accountPrefix`는 privacy-client input이 아닌 host metadata이므로 ClairveilJS에는 active profile의 prefix만 전달합니다.
2. Wallet/network identity를 확인하고 current on-chain circuit, audit, disclosure, asset, tree config를 query합니다. Static configuration은 consensus authority가 아닙니다.
3. [WebApp 저장소와 복구](clairveil-web-app-storage-recovery-kr.md)의 storage와 durable reservation recovery 규칙을 사용합니다.
4. [WebApp integration](clairveil-web-app-integration-kr.md)의 lifecycle/API 규칙을 사용합니다.
5. [WebApp 배포](clairveil-web-app-deployment-kr.md)의 browser/prover deployment boundary를 만족합니다.
6. 이 example에서 `serverFeatures.batchTransfer`를 false로 유지하고 one-proof
   batch prepare/submission UI를 노출하지 않습니다. SDK/reference test에서 쓰는
   loopback prover proxy route는 이 product gate를 바꾸지 않습니다. 향후 product integration은
   이 제품 경계를 바꾸기 전에 별도의 encrypted checkpoint, wallet confirmation,
   reconciliation, end-to-end release gate를 정의해야 합니다.

## 일반 client 문서와의 관계

[Client API checklist](clairveil-client-api-checklist-kr.md)와 [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)는 다른 제품에 필요한 batch capability까지 포함한 Clairveil contract 전체를 설명합니다. Protocol semantic은 계속 그 문서들이 authority입니다. 현재 browser WebApp이 노출할 수 있는 기능 범위는 이 문서가 authority입니다.
