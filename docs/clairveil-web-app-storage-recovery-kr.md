# Clairveil WebApp 저장소와 복구

English version: [clairveil-web-app-storage-recovery.md](clairveil-web-app-storage-recovery.md)

이 문서는 browser WebApp 범위를 위한 최소 production storage/restart contract입니다. `examples/clairveil-dapp`의 demo-only plaintext storage policy를 대체합니다.

## 민감 데이터와 수명

| 데이터 | 필요한 위치/수명 |
| --- | --- |
| Root signature, root seed, spend/view/disclosure private material | In-memory 또는 wallet-controlled secure store에만 둡니다. Application server, URL, analytics, crash report에 두면 안 됩니다. |
| Decrypted note와 scan cursor | Chain/profile/account 하나에 scope된 encrypted persistent browser store에 둡니다. |
| Reservation state와 operation evidence | Cross-tab locking을 가진 encrypted IndexedDB에 둡니다. Reservation error field에는 wallet/prover/RPC error prose가 아니라 stable internal code만 둡니다. |
| Account transaction boundary marker | Privacy root signature 없이 열 수 있는 canonical transaction-scope/account durable record 두 개로 분리합니다. Public send/deposit record에는 EVM wallet request 전에 `attempting`과 random attempt ID를 담을 수 있고 tx hash가 돌아오면 즉시 승격합니다. 물리적으로 분리된 private Cosmos record에는 정확한 signed tx hash와 generic `privacy` kind만 기록합니다. Recipient, amount, calldata, note/reservation ID 또는 다른 private operation data는 어느 record에도 저장하지 않습니다. |
| Prepared proof/payload와 raw recipient/amount | 검토된 encrypted recovery design이 있는 flow만 persist합니다. Feature-gated batch flow는 restart recovery에 필요한 exact prepared batch payload/proof/evidence를 저장하고 EVM deposit/transfer/withdraw는 나중 receipt를 검증할 original prepared transaction binding을 저장합니다. 이 artifact는 전용 encrypted namespace를 사용하며 relay metadata와 섞지 않습니다. |
| Relay recovery metadata | Cross-tab lock이 있는 encrypted multi-record store에 opaque payload hash, reservation ID/status, tx identity, handoff/submission status, expiry만 보존합니다. 각 persistence ID는 payload hash 또는 reservation ID에서 derive하며 payload 하나가 다른 payload를 overwrite하면 안 됩니다. Caller-supplied ID나 display-only field는 보존하지 않습니다. |

Sensitive wallet data를 위한 browser `localStorage`, plaintext IndexedDB,
unencrypted export와 모든 in-memory persistence fallback은 demo/test-only입니다.
위 account boundary marker는 transaction intent나 private material을 담지 않으므로
좁게 scope된 plaintext storage를 사용할 수 있습니다. Required encrypted persistence
또는 Web Locks가 없을 때 이를 fallback으로 사용하면 안 됩니다.

## Namespace와 암호화 규칙

Current persistence epoch에는 별도의 v0.3.1 namespace prefix를 사용합니다.
Checked-in example은 아래와 동등한 scoped key와 encrypted reservation
namespace를 사용합니다.

```text
clairveil:v0.3.1:notes-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:operations-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>:<payload-hash>
clairveil:v0.3.1:batch-transfer-artifact-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:evm-deposit-artifact-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:evm-operation-artifact-encrypted:<operation-id>:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:public-pending:<transaction-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:privacy-pending:<transaction-scope>:<storage-epoch>:<account-scope>
reservation namespace: <chain-id>:<privacy-scope>:<storage-epoch>:<account-scope>
```

`<privacy-scope>`는 on-chain privacy system(Cosmos chain ID 또는 EVM chain ID와
privacy precompile)을 canonical하게 식별하고, `<transaction-scope>`는 account
sequence/nonce domain을 식별해야 합니다. 같은 identity를 가리키는 동등한 UI
profile은 이 scope를 공유해야 하며 profile label과 endpoint 선택은 storage
identity가 아닙니다. Privacy identity field가 바뀌면 privacy scope를 교체합니다.
`<account-scope>`는 선택된 account에 대해서만 stable해야 하며 telemetry로 보내면
안 됩니다. Literal address 대신 opaque keyed identifier를 사용할 수 있습니다.
서로 다른 privacy system, chain ID, wallet kind, transparent account 사이에서는
namespace를 공유하면 안 됩니다.

전용 batch checkpoint에는 canonical prepared payload/proof, reservation identity,
expected aggregate/item evidence, execution transport, 사용한 경우 검증된 EIP-712
authorization transaction binding만 둡니다. Active profile, account, exact storage
key에 authenticate하고 external boundary 전에 저장합니다. Ambiguous result에서는
모든 linked reservation을 잠근 채 유지하고 complete input/expected-output set을
terminal typed reconciliation으로 검증한 뒤에만 삭제합니다.

EVM deposit과 private-operation artifact는 batch/relay namespace와 분리합니다.
Original prepared transaction, operation 또는 reservation identity, 이후
wallet/receipt hash를 bind해 receipt finality를 wallet에 요청한 내용과 대조할 수
있어야 합니다. Corrupt, missing, identity-mismatched artifact는 fail closed하고 original
operation이 unresolved인 동안 새 prepare로 대체하면 안 됩니다. 모든 artifact
mutation은 Web Locks와 commit 직전 active-session 재검사를 요구합니다.

Reservation에는 `createBrowserReservationStore`를 아래 조건으로 사용합니다.

- `requireLocks: true`: IndexedDB 또는 Web Locks failure는 spend preparation에 fatal입니다.
- Complete record를 persist 전에 encrypt/authenticate하는 paired `encodeState`/`decodeState` callback
- Wallet-private material 또는 검토된 secure-key service에서 얻은 non-extractable, namespace-separated encryption key
- `unsafeAllowPlaintext`, `unsafeAllowMemoryFallback`, public lookup-key opt-in 금지

Note cache도 같은 at-rest encryption standard를 만족해야 합니다. Production adapter는 `LocalStorageNoteStore`를 대체할 수 있지만 complete scan cursor를 보존하고 corrupt/decrypt-failed record에서 fail closed해야 합니다.

## Account, tab, lease owner

Account, wallet, chain-profile이 바뀌면 아래를 수행합니다.

1. Heartbeat를 전부 멈추고 in-memory session material만 clear합니다.
2. Old namespace를 닫고 note, lease, prepared data를 new scope로 복사하지 않습니다.
3. New privacy session을 derive하고 해당 encrypted namespace만 load합니다.
4. Spendable balance를 보여주기 전 scan/nullifier refresh를 실행하거나 이어갑니다.

1단계의 in-memory material에는 UI single-flight guard도 포함됩니다. 이전 proof,
wallet, relay request가 오래 걸려도 새 account의 action을 막지 않도록 invalidation에서
guard를 동기적으로 reset합니다. 다만 이전 completion은 계속 replacement session을
render하거나 mutate하지 못해야 합니다.

Product가 local account selector 또는 profile-scoped helper view(예: relayer
balance)를 제공한다면 해당 read도 해당하는 selected account/profile에 묶습니다. 이전
selection 또는 profile의 response가 scope 변경 뒤 current view를 덮어쓰거나 note
list를 refresh하면 안 됩니다.

비동기 setup, encrypted-store load, scan, nullifier refresh, receipt watcher,
relay recovery continuation은 모두 시작 시점의 privacy-session generation에
묶어야 합니다. 각 `await` 경계 뒤 browser state나 UI를 쓰기 전에 generation을
다시 확인합니다. Stale completion은 버려야 하며, old namespace를 new session에
반영하거나 old setup 실패로 new session을 invalidate하면 안 됩니다. 특히 이전
account의 EVM receipt callback이 새 account의 transaction status를 갱신하면 안
되며, 이전 session은 다시 열릴 때 durable reservation/transaction recovery로
처리합니다. Scan이 시작한 reservation reconciliation에도 같은 규칙을 적용합니다. 모든
recovery helper에 session token을 전달하고 reservation record를 cache하거나
note-reservation UI를 갱신하기 전에 다시 확인합니다.
나중에 도착한 EVM receipt 또는 Cosmos 실행 실패 복구도 예외가 아닙니다.
`Unknown` marker, chain-time/nullifier 확인, `ReplanRequired` 전이, reservation
view refresh는 operation을 준비한 session을 계속 사용해야 합니다. 해당 session이
바뀌면 receipt callback은 중단해야 하며 새로 연결한 account에 reconcile 결과를
적용하거나 렌더링하면 안 됩니다.
Receipt callback은 result handler 전체도 await하고 내부에서 처리해야 합니다.
Reconciliation이나 UI refresh를 fire-and-forget으로 실행하지 말고,
session-invalidated 결과를 전파하여 replacement session에서 성공/실패 문구를
쓰거나 busy state를 해제하기 전에 handler를 중단합니다.
Top-level UI handler는 이 sentinel을 toast, modal, 기타 stale error를 replacement
session에 보이는 일반 오류가 아니라 no-op으로 처리해야 합니다.
Encrypted metadata read, expiry check, nullifier query, reservation transition,
metadata write를 포함한 relay recovery에도 같은 규칙을 적용합니다. Stale
continuation은 새 session의 pending relay list를 바꾸거나 render하면 안 됩니다.
여기에는 startup recovery뿐 아니라 pending handoff의 상태를 refresh하거나 local
use를 위해 payload를 restore하는 사용자가 직접 시작한 동작도 포함됩니다.
Prepared relay heartbeat도 설치 시점의 session과 immutable payload version을
capture해야 하며, account/profile invalidation은 in-memory state를 reset하기 전에
이를 중단해야 합니다. 이전 in-flight tick이 나중에 끝나더라도 새 heartbeat의
ownership token을 clear하거나 교체하면 안 됩니다.
열린 approval/signing modal도 같은 전환에서 닫고 pending approval을 rejected로
resolve하여, 이전 recipient, amount, planner, failure text가 새 session의 DOM에
남지 않게 합니다.

각 tab/worker는 `browser-tab:${crypto.randomUUID()}` 같은 fresh random `leaseOwner`를 사용합니다. 다른 tab과 lease token을 공유하면 안 됩니다. Web Locks는 store mutation을 serialize하지만 stale worker ownership을 valid하게 만들지는 않습니다. Live lease를 가진 manager만 `Proving`/`ProofReady` operation을 전진할 수 있습니다.

Cosmos transaction 준비에는 canonical on-chain chain ID와 transparent account로
scope된 하나의 exclusive cross-tab lock도 필요합니다. 이 lock은 동등한 UI
profile끼리 공유해야 하며 local storage epoch를 포함하면 안 됩니다. Public send,
deposit, private transfer, direct withdraw, self-merge/helper transaction은 account
sequence/sign-doc을 읽는 시점부터 wallet signing과 durable broadcast 경계까지 같은
lock을 공유해야 합니다. Lock 전에 sign doc을 만들면 proof 또는 wallet 작업 중
다른 tab이 그 sequence를 소비할 수 있으므로 충분하지 않습니다. Local-test
mode에서는 lock 안에서 current genesis/storage epoch를 다시 조회·비교하여 chain
restart 뒤 stale tab이 fail closed하게 합니다. Durable public pending marker가
있거나 reservation에 `broadcast_in_flight`, `Submitted`, `Unknown`, submitted tx
hash가 남아 있으면 reconcile할 때까지 새 Cosmos sequence를 만들지 않습니다.
다른 account가 submit할 relay-only payload는 browser wallet의 Cosmos sequence를
소비하지 않지만, 그 payload를 만들기 위해 실행하는 local self transaction에는
동일한 lock을 적용합니다.

Encrypted reservation store만으로는 restart-safe account fence가 되지 않습니다.
재접속 후 user가 root-signature session을 다시 만들기 전에도 transparent send가
가능하기 때문입니다. 따라서 모든 private Cosmos submission은 RPC 호출 직전에
정확한 signed tx hash를 non-sensitive account boundary marker에도 기록합니다.
Public/private sequence 준비는 reservation store를 열지 않고도 이 marker를
검사해야 합니다. `privacy` entry는 chain result가 명시적으로 포함(success 또는
failure)되고 matching encrypted reservation이 그 결과에 맞는 safe terminal
reconciliation state에 도달한 뒤에만 지웁니다.

Public send/deposit marker와 private Cosmos marker는 같은 canonical account
transaction lock 아래 서로 다른 물리 record로 저장합니다. Guarded manual clear는
wallet history 확인 뒤 public record만 삭제할 수 있으며 private record를 삭제하거나
rewrite하면 안 됩니다. Private record 자체를 authenticate/decode할 수 없으면 account
boundary를 fail-closed로 유지하고 reviewed fresh-state reset 절차만 안내합니다. Generic
corrupt-state clear는 private broadcast가 없었다는 증거가 아닙니다.

## Durable operation state machine

Relevant high-level state progression은 아래와 같습니다.

```text
Verified unspent note
  -> Reserved -> Proving -> ProofReady
  -> BroadcastAttempting -> Submitted | Unknown
  -> ConfirmedSpent | ReplanRequired | ManualReview
```

`Reserved`는 inventory를 보호합니다. `Proving`과 `ProofReady`는 batch lease를 보유합니다. 하나의 operation input은 함께 이동해야 하며 note 하나만 broadcasting/submitted로 표시하면 안 됩니다. Proof construction과 final `Proving -> ProofReady` transition까지 heartbeat를 유지합니다.

External wallet/RPC/relayer를 호출하기 직전에 operation 전체에 `markBroadcastAttempting`을 persist합니다. 가능한 tx hash, signed-tx hash, sign-doc hash를 포함합니다. 그 다음:

- 실제 submit됐을 때만 `Submitted`를 기록합니다.
- Network에 도달했을 수 있으면 `Unknown`을 기록합니다.
- Post-broadcast `ReplanRequired`에는 tx identity와 explicit nullifier evidence가 필요합니다.
- `ConfirmedSpent`는 chain-spent evidence를 이용한 reconciliation을 통해서만 전이합니다.
- Unresolved evidence는 release가 아니라 `ManualReview`로 보존합니다.

그 직전의 fresh-nullifier preflight도 같은 session-bound operation의 일부입니다.
이미 spent인 input을 query/reconcile하는 중 privacy session이 바뀌면 새 broadcast
attempt를 기록하거나 새 session의 reservation view를 갱신하기 전에 중단합니다.
같은 captured session은 모든 failure branch에도 사용합니다. Rejected-wallet
bookkeeping, `Unknown`, `ReplanRequired`, `ManualReview` 전이를 operation owner가
바뀐 account에 적용하면 안 됩니다.

Browser가 attempt marker 뒤 crash하면 startup은 active operation을 찾아 known transaction identity를 query하고 모든 input nullifier를 refresh해야 합니다. Safe terminal state에 도달할 때까지 해당 input으로 다른 transaction을 prepare/submit하면 안 됩니다.

오래된/local operation에 `sign_doc_hash`만 있고 query 가능한 submitted/signed-transaction identity가 없으면 자동 recovery는 이를 `ManualReview`에 유지해야 합니다. Sign-doc hash만으로 wallet prompt가 거절됐는지, 열리기 전에 중단됐는지, 승인 직후 browser가 결과를 잃었는지 증명할 수 없기 때문입니다. Product는 이 대안을 보여 주고 이미 승인된 chain transaction을 취소하는 기능이 아님을 경고한 뒤에만 명시적인 local-cancellation recovery를 제공할 수 있습니다. 사용자는 wallet과 explorer에서 제출이 없음을 acknowledgement해야 하며, active reviewing account는 reservation batch를 다시 읽고 모든 input nullifier가 unspent인지 확인해야 합니다. 그 acknowledgement를 기록하고 operation을 `ReplanRequired`로 전이하며, 직접 `Released`/spendable로 전이하면 안 됩니다. 이 예외는 relay payload/handoff에는 적용하지 않으며, 이들은 별도의 expiry와 reconciliation boundary를 유지합니다.

## Relay withdraw 복구

Relay payload를 copy, download, QR encode, upload하기 전에 reservation batch 전체의 lease를 payload의 on-chain expiry까지 먼저 durable하게 연장하고, 그 다음 immutable payload hash와 `recordRelayHandoff`을 durable하게 기록합니다. Convenience를 위해 raw payload/proof를 persist하지 않습니다. Copy된 payload는 on-chain expiry까지 relayer에게 유효합니다. Local cancel, tab close, lease expiry는 revocation이 아닙니다.

Pending handoff recovery control은 강제 해제가 아니라 evidence 확인 action이어야 합니다. Authoritative chain expiry, 모든 input nullifier의 explicit-unspent 결과, durable no-broadcast 또는 query 가능한 failed/absent broadcast evidence가 모두 일치할 때에만 만료 handoff를 `ReplanRequired`로 전이할 수 있습니다. Query 가능한 broadcast identity와 durable no-broadcast evidence가 모두 없으면 handoff가 relayer에 도달했을 수 있으므로 잠금을 유지하고 그 이유를 안내해야 합니다. 활성화된 control이 조용히 아무 효과도 내면 안 됩니다.

각 handoff recovery는 canonical payload hash(또는 동등하게 derive된 reservation
identity)를 key로 하는 독립 encrypted record로 저장합니다. Restart에서는 모든 record를
enumerate하고 검증합니다. Cross-tab save/load/clear는 payload-scoped Web Lock을 사용하며
terminal reconciliation은 matching payload record만 삭제합니다. 서로 다른 payload를
준비한 두 tab이 상대 recovery evidence를 overwrite하거나 account-wide clear하면 안 됩니다.

이 handoff flow도 immutable payload version뿐 아니라 privacy-session generation에
묶습니다. Await되는 chain, nullifier, lease, persistence 작업마다 둘 다 다시
확인하고, clipboard, download, QR encoder, upload에 payload를 노출하기 직전에도
한 번 더 확인합니다. Session이 바뀌었다면 이전 session의 payload를 노출하거나
그 handoff 결과를 새 session에 render하지 않고 중단합니다.

Same-origin local relayer-submit button은 captured session과 immutable payload
version을 fresh chain/nullifier 확인, `BroadcastAttempting`, 제출, 결과 기록,
실패 복구까지 계속 묶되, 그 local request만을 이유로 `recordRelayHandoff`을
호출하거나 lease를 payload expiry까지 연장하면 안 됩니다. Durable attempt marker를
정확히 한 번의 local-relayer request 직전에 기록하므로 그 뒤의 모호한 실패는 lock을
유지한 채 reconcile합니다. `recordRelayHandoff`과 expiry-length lease는 Copy,
download, QR, upload처럼 실제 payload egress 전에는 계속 필수입니다. 둘 중 하나가
바뀌면 새 external boundary를 넘기기 전에 중단하고 그 결과나 error를 새 session에
쓰면 안 됩니다.

Restart와 expiry 시 authoritative chain time, tx evidence, 각 input nullifier를 확인합니다. SDK가 요구하는 post-broadcast reconciliation evidence 뒤에만 release/replan합니다. 그렇지 않으면 manual review를 위해 lock을 보존합니다.

## ClairveilJS 0.3.1 fresh-state 초기화

ClairveilJS 0.3.1은 `privacy-fixed-v1` note/disclosure envelope과 current V5/V2
payload contract를 사용합니다. 이 WebApp은 이전 persistence contract로 release된
적이 없으므로 fresh initialization만 지원합니다. Browser cache나 lifecycle
store migration을 정의하지 않고 in-place downgrade도 지원하지 않습니다.

1. Wallet state를 load하기 전에 current `clairveil:v0.3.1:*` namespace를 사용하는
   code를 배포합니다.
2. 이전 development cache, reservation, operation, relay record, recovery artifact,
   prepared payload, proof를 compatibility decode하거나 migrate하지 않습니다.
3. Empty current namespace를 초기화하고 full typed scan과 모든 nullifier refresh
   뒤에만 balance/planner를 노출합니다.
4. 오래된 development record를 migration이나 support diagnostic 목적으로 upload하지
   않습니다. Example은 명시적으로 이름 붙은 legacy plaintext note-cache key만
   삭제하고 Reset & Rescan을 요구합니다. 이 삭제는 migration이 아니며
   나머지 이전 record는 지원하는 input contract 밖에 둡니다.

Checked-in DApp은 이 baseline을 구현합니다. Note state는
`EncryptedLocalStorageNoteStore`, reservation state는 `requireLocks: true`와 encrypted
`createBrowserReservationStore` IndexedDB callback, relay recovery metadata는 payload
hash로 key된 multi-record이자 Web Locks가 필수인
`EncryptedLocalStorageOperationStore`에 저장합니다. Batch/EVM prepared-transaction
recovery는 별도의 one-record, Web Locks 필수
`EncryptedRecoveryArtifactStore`를 사용합니다. 각 record는 current in-memory
wallet material과 namespace에서 Web Crypto가 derive한 AES-GCM key를 사용합니다.
Key는 non-extractable이며 ciphertext와 함께 저장하지 않습니다. 필요한 browser
storage, IndexedDB, Web Crypto, Web Locks 중 하나라도 없으면 plaintext/memory
fallback 대신 privacy setup을 막습니다.

DApp은 이전 cache나 reservation record를 current state로 decode하지 않습니다.
v0.3.1 namespace는 compatibility migration이 아니라 fresh scan boundary입니다. 이
example을 쓰는 product는 browser persistence를 production wallet으로 취급하기 전에
key recovery와 user-data retention policy를 별도로 검토해야 합니다.
