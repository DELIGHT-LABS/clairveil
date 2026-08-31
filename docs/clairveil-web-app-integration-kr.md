# Clairveil WebApp Integration

English version: [clairveil-web-app-integration.md](clairveil-web-app-integration.md)

이 문서는 feature-gated atomic batch flow를 포함한 [WebApp 범위](clairveil-web-app-scope-kr.md)의 browser WebApp을 위한 production integration guide입니다. `clairveiljs/browser-dapp` public API를 사용합니다. 예제 application의 `public/app.js`를 library contract로 import하면 안 됩니다.

## 1. 검증된 profile 하나로 client 생성

Versioned Web client configuration schema에서 검증된 profile 하나를 load합니다. Active profile만 browser client transport, endpoint, identity prefix의 source입니다. EVM에서는 `evmGasLimit`과 `evmSendGasLimit`도 active profile에서 가져오며, 둘 중 하나 또는 transport가 바뀌면 이후 prepare 또는 wallet request 전에 cached browser client를 다시 만듭니다.

Cache한 recipient-address suggestion은 profile-scoped display data입니다.
Profile이 바뀌면 동기적으로 clear하고, profile이 더 이상 일치하지 않는 진행 중
address lookup 결과는 버립니다. 이전 profile의 transparent 또는 shielded address를
새 profile의 recipient로 제안하면 안 됩니다.

```ts
import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";

// rawConfig는 검증된 config endpoint에서 받은 complete JSON입니다.
const config = validateClairveilWebClientConfig(rawConfig);
const profile = config.activeProfile;
const client = createClairveilBrowserDappClient({
  profile,
  // Product exposure는 명시적이며 SDK/prover 존재 여부로 발견하지 않습니다.
  enableExperimentalBatchTransfer:
    config.serverFeatures?.batchTransfer === true,
  queryTimeoutMs: 30_000,
  nullifierFailover: false
});
```

선택한 field만으로 partial profile을 만들면 안 됩니다. Validator는 complete schema profile을 요구하고 duplicate ID, active profile 누락, flattened compatibility field 불일치를 거부합니다. EVM active profile에는 schema가 요구하는 `evmRpc`, `evmChainId`, `evmPrivacyPrecompileAddress`, `evmNativeDenom`, gas limit이 포함되고 `evmDepositMode`는 `payable-exact-value`여야 합니다. 이 precompile은 proof와 exact `msg.value`를 받는 deposit, self-view/expiry를 보존하는 transfer, legacy output field가 없는 exact-match withdraw, `singleProofBatchTransfer`로 구성된 Clairveil 0.3.1 canonical ABI를 구현해야 합니다. EVM transaction을 준비하기 전 connected wallet network와 `evmChainId`를 비교합니다. Raw chain-profile field는 deployment input일 뿐 신뢰할 수 있는 chain state가 아닙니다.

Optional JSON-safe `evmAuthorizationProfile`은 target chain의 EIP-712 domain과
authorization-kind allowlist를 제공합니다. WebApp이 이를 임의로 만들거나 다른
chain 값을 재사용하면 안 됩니다. 검토된 non-canonical `EvmContractAdapter`는
`globalThis.CLAIRVEIL_EVM_CONTRACT_ADAPTERS[profile.id]`, complete
`PrivacyStateAdapter`는
`globalThis.CLAIRVEIL_PRIVACY_STATE_ADAPTERS[profile.id]`, built-in 또는 검토된
custom finality policy는
`globalThis.CLAIRVEIL_EVM_FINALITY_POLICIES[profile.id]`로 주입할 수 있습니다.
각 adapter는 선택한 profile/precompile에 bind하고 SDK canonical validator가
수락하기 전까지 결과를 신뢰하지 않습니다. Privacy-state adapter가 없으면 typed
host query surface가 계속 필수입니다.

Cosmos-EVM에서는 typed privacy-scan transaction hash와 Ethereum wallet/receipt
hash를 분리해 보존합니다. Scan record에 index된 `ethereumTxHash` 관계를 검증한
뒤 EVM 성공 evidence로 사용하며 두 hash가 같다고 비교하면 안 됩니다.

Spendable note를 보여주기 전과 모든 privacy prepare 직전에 integration은 아래 preflight가 실패하면 fail closed해야 합니다.

```ts
await client.health();
await client.assertTransferProtocolConfig(profile.denom);
await client.queryReserve(profile.denom);
```

`assertTransferProtocolConfig`는 consensus circuit identity, authoritative asset mapping, audit config, disclosure config를 검증하고 `health()`는 current node/tree state를 검증합니다. EVM은 configured EVM RPC의 read-only `eth_chainId`도 `evmChainId`와 비교합니다. 실패나 profile mismatch가 있으면 fresh successful preflight 전까지 모든 privacy action을 막고 spendable inventory를 숨겨야 합니다.

Privacy prepare에서 이 preflight를 실행할 때는 모든 request와 success/failure UI
update를 해당 prepare의 privacy-session generation에 묶습니다. 진행 중 account,
wallet, profile 또는 선택한 REST endpoint가 바뀌면 old endpoint/profile 결과로
new session의 chain-safety state를 설정하지 말고 결과를 버립니다. Note mutation
또는 account transaction이 대기/실행 중이면 endpoint 선택을 비활성화합니다.

예제는 `/api/health`가 없거나 unreachable일 때만 static config를 사용합니다. 이 경우 production gate가 검증하는 same-origin `/dapp-config.json` artifact를 fetch합니다. Reachable server configuration의 schema invalid, profile mismatch, non-success response는 조용히 fallback할 이유가 아니라 sync failure입니다.

브라우저는 `/api/health` bootstrap 및 static-config fetch를 30초와 1 MiB로 제한합니다. Health timeout은 도달 불가로 취급하여 검증된 static artifact를 사용할 수 있지만, 너무 크거나 malformed인 응답은 계속 sync failure입니다.

Static host가 없는 `/api/health` route에 HTML shell을 반환하는 경우에는 direct static artifact가 `serverBacked: false`로 검증된 뒤에만 health endpoint가 없는 것으로 처리합니다. Invalid server JSON configuration으로 조용히 fallback하지 않습니다.

Bootstrap 및 health data도 profile-scoped view로 취급합니다. 이전 health request가
pending인 동안 더 최근 health refresh 또는 profile selection이 완료되면, 이전
response/error가 current profile의 config, feature visibility, helper data를 다시
적용하지 않도록 버립니다.

`waitForEvmTransaction(...)`이 일반 confirmation helper입니다. Higher-level wrapper가 없는 EVM read(예: receipt recovery)에만 typed `evmJsonRpc<TResult>(method, params)`를 사용합니다. 이 API는 MetaMask/EIP-1193이 아니라 configured RPC endpoint를 사용하며 account 접근, signing, transaction submission에 쓰면 안 됩니다.

## 2. Privacy session 생성

Account가 바뀔 때마다 새로운 in-memory privacy session을 시작합니다.

1. Configured wallet을 연결하고 chain/network를 확인합니다.
2. Transparent address와 public key를 얻습니다.
3. `client.buildRootSigningMessage(address, pubKeyHex)`로 domain-separated root signing message를 만듭니다.
4. Keplr `signArbitrary` 또는 MetaMask `personal_sign`으로 signature를 요청하고 EVM signature는 base64로 encode합니다.
5. `client.derivePrivacyAccount({ address, pubKeyHex, signatureBase64 })`를 호출합니다.
6. Signature/root material을 log, analytics, URL, server request, crash report에 남기지 않습니다.

Root signature는 privacy-sensitive authority material입니다. 일반 transaction signature로 대체하거나 다른 transparent account, chain, profile의 signature를 조용히 재사용하면 안 됩니다.

## 3. Spendable balance 표시 전 scan

[WebApp 저장소와 복구](clairveil-web-app-storage-recovery-kr.md)의 encrypted note store와 reservation manager를 만든 뒤 privacy session으로 scan합니다.

```ts
const scan = await client.scanWalletNotes({
  address,
  pubKeyHex,
  signatureBase64,
  noteStore,
  ...resumeCursor
});

await noteStore.save({
  notes: scan.notes,
  scanCursor: scan.scanCursor
});
```

Complete `privacy-scan-v2` cursor를 persist합니다. 성공한 explicit nullifier response가 `used: false`임을 확정할 때만 note가 spendable입니다. Unknown, malformed, failed nullifier result는 제외합니다. Typed-scan failure는 terminal이며 cursor semantic이 다른 source로 조용히 fallback하면 안 됩니다. 자세한 retry/failover 규칙은 [Client API checklist](clairveil-client-api-checklist-kr.md)에 있습니다.

## 4. 지원 흐름 실행

| 흐름 | Prepare | External boundary 전 | External boundary 후 |
| --- | --- | --- | --- |
| Deposit | 제품이 제공하는 `DepositCircuit` proof provider와 `prepareDeposit` | Remote provider를 쓰면 active profile의 정확한 HTTPS `depositProofUrl`을 pin하고 필요한 proof material만 그 origin에 보냅니다. 반환된 Cosmos sign doc을 user가 sign하거나 EVM transaction을 보냅니다. | Wait/lookup 후 output note를 scan합니다. |
| Transfer | Privacy session/encrypted store로 `prepareTransfer` | status가 `self_merge_required`면 ordinary self-transfer의 명시적 승인을 받고 완료/scan 후 replan합니다. | Prepared transfer 하나를 sign/broadcast하고 reservation/nullifier를 reconcile합니다. |
| 원자적 batch transfer | Feature gate된 `prepareTransferBatch`, payment별 disclosure, encrypted payload/proof checkpoint callback | Authoritative prepare scan 뒤 각 행을 실제 recipient, amount, disclosure, total, change, 1–16 input, 1–32 output에 bind해 wallet을 열기 전에 확인합니다. 조용히 분할하지 않고 여러 독립 atomic batch에는 명시적 승인을 받습니다. | Cosmos `MsgBatchTransfer` 하나 또는 canonical EVM `singleProofBatchTransfer` call 하나를 제출합니다. 결과가 불명확하면 모든 input nullifier와 expected payment output이 typed operation evidence와 일치할 때까지 pending으로 유지합니다. |
| Direct withdraw | `prepareWithdraw` | Exact-match note 하나와 필요한 경우 current chain time을 요구합니다. | Sign/broadcast 후 input nullifier를 reconcile합니다. |
| Relay withdraw | `prepareRelayWithdraw` | Fresh chain time을 확인하고 payload copy/upload 전에 durable relay handoff를 기록합니다. Browser handoff는 local relayer helper 없이도 동작해야 합니다. | Relayer는 expiry까지 제출할 수 있으므로 local cancel을 revocation으로 취급하지 말고 reconcile합니다. |

Cosmos wallet submission은 account sequence를 읽는 prepare call 전에 canonical
on-chain chain ID와 transparent account로 scope한 cross-tab transaction lock을
획득하고 signing과 durable broadcast 경계까지 유지합니다. 이 lock에는 UI profile
ID나 local storage epoch를 넣으면 안 됩니다. 동등한 profile 및 서로 다른 local
epoch를 관측한 tab도 같은 on-chain account를 제어하기 때문입니다. Public/private
action은 같은 lock을 사용하며, unresolved public marker 또는 private reservation
broadcast가 있으면 reconcile 전에는 새 sequence를 만들지 않습니다. Local-test
mode에서는 lock 안에서 current genesis/storage epoch를 다시 조회·비교하고 stale
tab의 sequence 준비를 거부합니다. 다른 relayer account가 submit하고 local self
transaction도 필요 없는 relay-only preparation만 이 account lock에서 제외합니다.
Transparent send는 root-signature session 복원 전에도 가능하므로, 각 private Cosmos
tx hash를 RPC broadcast 직전에 non-sensitive canonical account marker에도 기록하고
encrypted reservation store를 열지 않은 상태에서도 이를 검사합니다. Matching chain
및 reservation reconciliation이 safe terminal state에 도달한 뒤에만 marker를
제거합니다.

EVM profile이 EIP-712 domain을 제공하면 feature-gated batch panel에서
`singleProofBatchTransferWithAuthorization`을 시험할 수 있습니다. Built-in 경로는
연결된 account를 effective sender와 실제 제출 executor로 함께 bind하고 fresh random
nonce를 사용하며 authorization deadline을 prepared operation expiry로 설정합니다.
다른 external executor에는 별도의 durable product handoff flow가 필요합니다.

권한 있는 batch-audit view는 typed `privacy-scan-v2`를 사용해 output을 transaction
hash별로 묶고 각 output의 mandatory audit disclosure를 검증·복호화해야 합니다. Raw
`batch_transfer` event에는 operation root와 audit-key identity만 있고 output별 audit
ciphertext/digest가 없으므로 batch disclosure를 재구성하는 입력으로 쓰면 안 됩니다.

`depositProofUrl`은 검토된 product-owned deposit proof service를 위한 optional profile field입니다. Prover base URL이 아니라 정확한 `POST` endpoint이며 browser는 provider가 기대하는 `note_json`, `note_commitment_hex` material을 보내고 `Content-Type: application/json`의 versioned JSON proof response를 요구합니다. Static WebApp은 이 URL이나 browser/WASM provider가 없으면 Deposit을 unavailable로 두어야 합니다. 예제의 same-origin `/api/deposit/proof` route는 loopback local-test fallback 전용입니다. 예제는 응답을 120초 안에 끝내지 못한 deposit-proof request를 abort합니다. 이 client bound는 provider 쪽 body-size/timeout limit을 대체하지 않습니다. Redirect를 허용하지 않고 final response URL이 pin된 endpoint와 같은지 확인하며, parsing 전 deposit-proof response를 1 MiB로 제한합니다.

Preparation이 시작될 때 pin된 proof endpoint를 capture합니다. SDK proof-provider
callback은 나중에 active profile을 다시 읽으면 안 됩니다. Proof material을 보내기
전과 bounded response가 돌아온 뒤 preparation session을 확인합니다. Account, wallet,
profile이 바뀌었다면 결과를 버리며, 이전 operation을 교체된 profile의 endpoint로
보내면 안 됩니다.

예제는 일반 privacy preflight와 reservation check가 끝나면 browser가 relay payload를 항상 준비하고 copy할 수 있게 합니다. `Relay` button은 optional local `POST /api/relayer/withdraw` submission adapter일 뿐입니다. Static deployment는 Copy(또는 product-owned handoff adapter)를 사용해야 하며 local helper가 없다는 이유로 payload prepare를 막으면 안 됩니다.

Dummy withdraw output을 만들면 안 됩니다. Browser가 broadcast response를 놓쳤다는 이유만으로 transaction을 retry하면 안 됩니다. 먼저 알려진 transaction identity를 lookup하고 nullifier를 refresh합니다.

## 5. Reservation과 broadcast 경계

가능하면 reservation manager와 prepared reservation metadata를 모두 전달해 SDK의 `signDirectAndBroadcast`, `broadcastSignedTx`, supported EVM submission integration을 사용합니다. Custom submission integration은 operation *전체*에 아래 순서를 정확히 따라야 합니다.

1. `Proving`/`ProofReady` lease를 유지합니다.
2. 사용 가능한 tx/sign-doc identity를 포함해 `markBroadcastAttempting(...)`을 durable하게 기록합니다.
3. External wallet/RPC/relayer boundary를 정확히 한 번 통과합니다.
4. 실제 submit 뒤에만 `markSubmitted(...)`, network 도달 가능성이 있으면 `markUnknown(...)`을 기록합니다.
5. Replan, release, 재제출 전에 tx identity와 explicit nullifier evidence로 reconcile합니다.

4단계 기록이 실패해도 2단계 marker는 safety lock으로 남습니다. 이를 지우거나 재제출하지 말고 recovery로 진입합니다. Relay handoff는 payload를 노출하기 전에 reservation batch 전체 lease를 immutable payload expiry까지 먼저 연장하고 그 다음 `recordRelayHandoff(...)`을 호출합니다. Authoritative expiry/reconciliation까지 lock을 유지합니다.

Same-origin local-relayer submit button에는 이 외부 handoff state를 기록하면 안 됩니다. 아직 raw payload가 browser 밖으로 나가지 않았기 때문입니다. Fresh chain/nullifier 확인 뒤 prepared-payload lease heartbeat를 멈추고 `markBroadcastAttempting(...)`을 기록한 다음 정확히 한 번 local relayer를 요청합니다. 이 marker 뒤의 실패는 제출되었을 수 있으므로 reconcile해야 합니다. Expiry까지의 lease 연장을 먼저 수행하고 그 다음 `recordRelayHandoff(...)`을 기록하는 순서는 Copy, download, QR, upload 등 실제 payload egress에만 사용합니다.

Value-moving action은 최초 click부터 wallet request까지 single-flight로
처리합니다. 비동기 privacy setup이 끝난 뒤에만 button을 비활성화하면 reentrancy
window에서 서로 독립적으로 준비된 두 submission이 생길 수 있습니다. Operation이
known pending 또는 terminal path에 도달한 뒤에만 UI lock을 해제하고, 이후의 명시적
사용자 action이 새 operation을 만들게 합니다.
Local relayer의 chain-time 또는 recovery preflight 전에도 이를 적용합니다. Server
side idempotency는 backstop일 뿐, 같은 relay payload를 두 browser flow가 동시에
전진하게 두는 이유가 될 수 없습니다.

Wallet 서명과 raw transaction submission은 별도의 asynchronous boundary로
취급합니다. Wallet approval 동안 prepared operation의 privacy session을 유지하고,
signature가 돌아온 직후와 raw broadcast를 시작하기 직전에 다시 검사합니다.
Wallet이 열린 동안 session이 바뀌면 signed checkpoint를 버리고 원래 session의
no-broadcast recovery만 수행합니다. 교체된 account에서 이를 제출하면 안 됩니다.
MetaMask에서는 `eth_sendTransaction`이 approval/submission boundary입니다. Chain과
gas setup 뒤 및 해당 호출 직전에 captured session을 다시 검사합니다. 최종 검사 전에
아니라 최종 검사 직후 wallet request 바로 앞에서 account-scoped hashless
`attempting` marker를 durable하게 기록합니다. Tx hash가 돌아오면 이후 session
검사나 receipt polling 전에 동기적으로 해당 marker를 hash evidence로 승격합니다.
명시적인 provider rejection(`4001`)만 wallet이 submit하지 않았음을 증명하므로
hashless marker를 자동 삭제할 수 있습니다. Crash, timeout, 기타 provider error에서는
fail-closed로 유지하고 사용자가 wallet history를 확인한 뒤 guarded manual clear를
사용해야 합니다. 이 clear는 canonical account lock 아래 별도로 저장된 public
send/deposit record만 삭제하고 private Cosmos signed-tx fence는 절대 삭제하지 않습니다.
Private fence가 손상되면 fail-closed를 유지하고 reviewed fresh-state reset 경로만
제공합니다. Marker 전 session 변경은 no-broadcast failure이고 marker 이후
session 변경은 recovery state를 유지합니다.

## 6. Failure별 안정적인 사용자 action

문구를 parsing하지 말고 error를 action으로 map합니다.

| 조건 | 사용자 action | 자동 action |
| --- | --- | --- |
| Typed scan/config validation 실패 | Sync unavailable을 표시하고 same-source retry 또는 reset/rescan을 제안합니다. | Scan semantic을 downgrade하지 않습니다. |
| Nullifier result unknown | Balance/input을 unavailable로 표시합니다. | 기본적으로 같은 endpoint만 retry합니다. |
| Planner self-merge 필요 | 명시적인 self-transfer 승인을 요청합니다. | Batch/multi-send로 fallback하지 않습니다. |
| Batch capacity 초과 | 여러 atomic batch를 명시적으로 선택하게 하고 batch 사이의 일부 완료 가능성을 설명합니다. 승인 전에 준비된 각 recipient와 disclosure mode를 보여줍니다. | Payment를 조용히 분할·재정렬·개별 retry하지 않고, 검증된 행은 이후 retry에서 제외합니다. |
| Batch checkpoint/evidence 미해결 | Reservation recovery를 표시하고 관련 input을 계속 잠급니다. | Reconciliation이 terminal 상태가 되기 전 encrypted checkpoint를 덮어쓰거나 다음 batch를 제출하지 않습니다. |
| Prover timeout/transport 실패 | 같은 prover에 cancel/retry를 표시합니다. | 명시적 privacy opt-in 없이 두 번째 prover로 failover하지 않습니다. |
| Broadcast timeout/terminal-write 실패 | Pending reconciliation을 표시합니다. | Tx identity/nullifier를 query하고 resubmission을 막습니다. |
| Payload expiry | Rebuild/re-sign합니다. | 기존 payload를 extend하지 않습니다. |
| Manual review | 권한 있는 operator에게 chain/payload evidence를 보여줍니다. Query 가능한 submitted-transaction/signed-transaction identity가 없으면 미확정 wallet request의 경우를 설명하고 명시적인 local-cancellation 확인을 받습니다. | Note를 자동으로 spendable로 만들지 않습니다. |

사용자가 시작한 ManualReview resolution은 승인한 reviewing account와 privacy
session에 묶습니다. `ReplanRequired` transition을 요청하기 직전에 해당 session에서
모든 reservation, nullifier, transaction-outcome evidence를 다시 읽습니다. Check나
transition 진행 중 account, wallet, profile이 바뀌면 이전 operator의 승인을 적용하거나
새 session에 결과/stale error를 render하지 않고 중단합니다.

### 추적할 수 없는 wallet request의 명시적 예약 취소

`sign_doc_hash`는 wallet에 표시된 요청을 식별할 뿐, wallet request가 승인·거절·제출되었음을 증명하지 않습니다. 따라서 browser에는 sign-doc identity는 있지만 query 가능한 `submitted_tx_hash`나 `tx_bytes_hash`가 없는 `ManualReview` reservation이 남을 수 있습니다.

이 상태의 non-relay operation에는 product가 명시적인 **예약 취소 및 다시 준비** action을 제공할 수 있습니다. 이는 chain cancellation이 아니라 local recovery action입니다. UI는 wallet prompt를 열기 전에 중단됐을 수 있음, 사용자가 요청을 거절했을 수 있음, 사용자가 승인했지만 browser가 submission result를 잃었을 수 있음을 설명해야 합니다. 또한 이미 승인된 거래가 나중에 제출될 수 있음을 경고하고, reviewing user가 wallet activity와 explorer에서 제출된 거래가 없음을 확인하도록 요구해야 합니다.

이 action은 operation의 모든 reservation에 query 가능한 submitted/signed-transaction identity가 여전히 없을 때만 허용합니다. 명시적 acknowledgement 직후 같은 privacy session과 reviewing account에서 모든 reservation evidence를 다시 읽고, 모든 input nullifier가 explicit unspent임을 요구합니다. 그 뒤 해당 operation만 `ReplanRequired`로 전이하고 operator approval과 explicit-untracked-request cancellation reason을 기록할 수 있습니다. Note를 직접 spendable로 만들거나 relay/handoff reservation을 조용히 release하거나, 대체 transaction의 정상 scan/planner 경로를 우회하면 안 됩니다.

SDK가 제공하는 경우 `ClairveilErrorCode`를 사용합니다. Prover HTTP error는 versioned `invalid_request`, `method_not_allowed`, `not_found`, `unauthorized`, `unavailable`, `proof_failed` code를 사용합니다. [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)를 참고합니다.

## 7. 필수 integration test

Product는 mock만이 아니라 실제 wallet adapter와 endpoint를 test해야 합니다.

- Initial sync, interruption, encrypted-store reload, forced rescan
- Cosmos/EVM transport 각각에서 Deposit, 필요한 self-merge, transfer 하나,
  one-proof batch transfer, exact-match withdraw, relay withdraw
- Broadcast 전 wallet rejection, broadcast 후 RPC timeout, broadcast 후 local status write 실패
- Wallet prompt, public send, local helper 응답, SDK preparation 중 account, wallet-network 또는 선택 profile이 바뀌는 경우. stale UI/error update를 버리고 invalidation 뒤 새 send가 wallet boundary를 넘지 않는지 확인
- `Proving`, `ProofReady`, `Submitted`, relay handoff 중 tab contention/reload
- sign-doc identity만 남은 untracked wallet request, acknowledgement UI, fresh
  nullifier check, audit 가능한 `ManualReview -> ReplanRequired` transition
- Current chain/circuit/audit config mismatch
- EVM wallet-network mismatch와 receipt recovery
- Startup에 이전 development persistence record가 있어도 required full rescan 전
  current v0.3.1 namespace로 decode/migrate되지 않음
- Batch gate off/on, canonical Cosmos/EVM batch 제출, 설정된 경우 EIP-712
  authorization binding, encrypted checkpoint reload, ambiguous receipt recovery,
  typed item/audit reconciliation
- Deployed origin의 `verify:production-deployment` gate와 같은 origin에서의 수동 Keplr/MetaMask connect, sign, scan, recovery flow

[Testing guide](clairveil-testing-guide-kr.md)의 DApp package check와 canonical conformance fixture를 이용한 SDK release verification을 실행합니다.
