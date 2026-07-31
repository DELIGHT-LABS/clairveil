# Clairveil WebApp 저장소와 복구

English version: [clairveil-web-app-storage-recovery.md](clairveil-web-app-storage-recovery.md)

이 문서는 browser WebApp 범위를 위한 최소 production storage/restart contract입니다. `examples/clairveil-dapp`의 demo-only plaintext storage policy를 대체합니다.

## 민감 데이터와 수명

| 데이터 | 필요한 위치/수명 |
| --- | --- |
| Root signature, root seed, spend/view/disclosure private material | In-memory 또는 wallet-controlled secure store에만 둡니다. Application server, URL, analytics, crash report에 두면 안 됩니다. |
| Decrypted note와 scan cursor | Chain/profile/account 하나에 scope된 encrypted persistent browser store에 둡니다. |
| Reservation state와 operation evidence | Cross-tab locking을 가진 encrypted IndexedDB에 둡니다. Reservation error field에는 wallet/prover/RPC error prose가 아니라 stable internal code만 둡니다. |
| Prepared proof/payload와 raw recipient/amount | 별도 검토된 encrypted recovery design이 없으면 memory에만 둡니다. Example batch-transfer flow는 `prepareTransferBatch`가 broadcast 전에 durable prepared-payload/prepared-proof callback을 요구하므로 account/profile-scoped AES-GCM checkpoint를 별도로 사용합니다. |
| Relay recovery metadata | Encrypted store에 opaque payload hash, reservation ID/status, tx identity, handoff/submission status, expiry만 보존합니다. Persistence ID는 payload hash 또는 reservation ID에서 derive하고 caller-supplied ID나 display-only field는 보존하지 않습니다. |

Browser `localStorage`, plaintext IndexedDB, unencrypted export, in-memory fallback은 demo/test-only입니다. Encrypted persistence 또는 Web Locks가 없을 때 production fallback이 아닙니다.

## Namespace와 암호화 규칙

Current persistence epoch에는 별도의 namespace prefix를 사용합니다.

```text
clairveil:wallet-notes:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:note-reservations:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:relay-withdraw-payloads:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:batch-transfer-artifacts:v1:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
```

`<profile-scope>`는 validated profile의 privacy identity field를 고유하게 식별해야 하며 해당 field가 바뀌면 교체해야 합니다. `<account-scope>`는 선택된 account에 대해서만 stable해야 하며 telemetry로 보내면 안 됩니다. Literal address 대신 opaque keyed identifier를 사용할 수 있습니다. Profile, chain ID, wallet kind, transparent account 사이에서 namespace를 공유하면 안 됩니다.

Batch-transfer checkpoint에는 reservation operation에 복구를 결합하는 데 필요한 exact
canonical prepared payload와 proof만 저장합니다. Checkpoint 전체를 encrypt/authenticate하고
log, export, analytics, crash report에는 절대 기록하지 않습니다. Durable callback 중 하나라도
성공한 뒤 발생한 failure는 recovery state입니다. Matching operation/output evidence를 가진
`ConfirmedSpent` reconciliation 또는 authorized terminal replan/failure transition이 reservation을
해제할 때까지 reservation을 잠그고 새 batch를 막습니다. 해당 terminal reconciliation 뒤에만
checkpoint를 삭제합니다.
Inclusion이 처음에는 pending이면 이후 typed note scan이 이 checkpoint를 다시 load하고
모든 reservation과 expected payment output을 재검증한 뒤 batch review UI의 항목별
evidence 상태를 복원합니다. 서로 다른 terminal state가 섞이면 atomicity conflict로
간주하고 manual review를 위해 잠금을 유지합니다.

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

Relay payload를 copy, download, QR encode, upload하기 전에 immutable payload hash와 `recordRelayHandoff`을 durable하게 기록합니다. Convenience를 위해 raw payload/proof를 persist하지 않습니다. Copy된 payload는 on-chain expiry까지 relayer에게 유효합니다. Local cancel, tab close, lease expiry는 revocation이 아닙니다.

Pending handoff recovery control은 강제 해제가 아니라 evidence 확인 action이어야 합니다. Authoritative chain expiry, 모든 input nullifier의 explicit-unspent 결과, durable no-broadcast 또는 query 가능한 failed/absent broadcast evidence가 모두 일치할 때에만 만료 handoff를 `ReplanRequired`로 전이할 수 있습니다. Query 가능한 broadcast identity와 durable no-broadcast evidence가 모두 없으면 handoff가 relayer에 도달했을 수 있으므로 잠금을 유지하고 그 이유를 안내해야 합니다. 활성화된 control이 조용히 아무 효과도 내면 안 됩니다.

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

## ClairveilJS 0.2 upgrade 절차

ClairveilJS 0.2는 `privacy-fixed-v1` note/disclosure envelope과 strict V5/V2 payload contract를 사용합니다. Pre-0.2 persistence record는 신뢰할 수 없고 호환되지 않습니다.

1. Legacy browser record를 load하기 전에 `v2` namespace prefix를 사용하는 code를 배포합니다.
2. Compatibility decode, reservation migration, relay proof replay를 시도하지 않습니다.
3. Empty current namespace를 만들고 full typed scan 및 모든 nullifier refresh 뒤에만 balance/planner를 노출합니다.
4. Legacy browser record는 그대로 둡니다. v0.2 browser integration은 legacy
   `localStorage`, `sessionStorage`, reservation database record를 포함해 이를
   읽거나 migrate하거나 변경하면 안 됩니다. Migration/support diagnostic을 위해
   legacy record를 upload하면 안 됩니다.

Checked-in DApp은 이 baseline을 구현합니다. Note state는 `EncryptedIndexedDbNoteStore`, reservation state는 `requireLocks: true`와 encrypted `createBrowserReservationStore` callback, relay recovery metadata는 encrypted IndexedDB record에 저장합니다. 각 record는 현재 in-memory root signature와 namespace에서 Web Crypto가 derive한 AES-GCM key를 사용합니다. Key는 non-extractable이며 ciphertext와 함께 저장하지 않습니다. IndexedDB, Web Crypto, Web Locks 중 하나라도 없으면 plaintext/memory fallback 대신 privacy setup을 막습니다.

DApp은 legacy `localStorage`/`sessionStorage`와 legacy reservation record를 읽거나 변경하지 않습니다. `v2` namespace는 compatibility migration이 아니라 fresh scan boundary입니다. 이 example을 쓰는 product는 browser persistence를 production wallet으로 취급하기 전에 key recovery와 user-data retention policy를 별도로 검토해야 합니다.
