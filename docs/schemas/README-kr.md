# Clairveil JSON Schema

이 디렉터리는 Clairveil JS/TS SDK와 웹월렛 연동을 위한 machine-readable contract를 담습니다.

English version: [README.md](README.md)

## Schema

- `clairveil-js-wallet-contract.schema.json`: `x/privacy/client/sdk/conformance/testdata` 아래 wallet-facing conformance fixture의 JSON Schema입니다.
- `clairveil-web-client-config.schema.json`: Versioned browser WebApp chain-profile configuration의 JSON Schema입니다.

## 사용 방법

외부 SDK는 live network integration을 시작하기 전에 CI에서 fixture를 검증하는 편이 좋습니다.

Browser 제품은 선택한 runtime profile을 `clairveil-web-client-config.schema.json`으로 추가 검증하고 duplicate profile ID와 active profile에 맞지 않는 compatibility/flattened field를 거부해야 합니다. Optional profile `depositProofUrl`은 검토된 HTTPS deposit proof-service의 정확한 endpoint를 나타내며 `proverUrl`과 별도의 privacy boundary입니다. 모든 HTTP(S) endpoint에는 credential, query, fragment가 없어야 합니다. URL userinfo와 query string은 bearer-token mechanism이 아니며, configuration artifact와 deployment diagnostic에서 secret이 노출되지 않도록 거부합니다. Legacy top-level EVM `accountPrefix`는 host metadata이므로 active profile의 privacy identity prefix와 비교하지 않습니다. Schema는 deployment input을 설명할 뿐이므로 browser는 계속 consensus circuit, audit, disclosure, asset, tree config를 chain에서 얻어야 합니다.

EVM profile은 `evmNativeDenom`, canonical
`evmDepositMode: "payable-exact-value"`, target chain의 EIP-712 domain과
authorization-kind allowlist를 담는 optional JSON-safe
`evmAuthorizationProfile`도 bind합니다. Adapter/finality 구현은 runtime dependency로
남기고 executable configuration data로 넣지 않으며 validated profile ID로 선택해야
합니다.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

repo의 예제 validator는 실행 부담을 줄이기 위해 dependency-free subset validator를 사용합니다. Production JS/TS SDK는 같은 schema 파일을 AJV 같은 full JSON Schema validator로 검증해도 됩니다.

## Schema가 다루는 것

- browser signer/root seed derivation fixture shape
- wallet readonly address, view key, disclosure, scan fixture
- prepared transfer prover payload `v5` shape, final owner intent, disclosure blinding, `view_tag_hexes`, sender self-view disclosure field
- prepared withdraw prover payload shape
- final prepared withdraw payload shape
- relay withdraw handoff request와 relayer `MsgWithdraw` mapping shape
- prover HTTP route, request, response, error contract shape
- note reservation status, transition, active uniqueness, lease precondition, lookup-key vector, operation success evidence contract shape
- `scan_events` request/response fixture shape, cursor field, projection output, `scan_format_version`, `view_tag_version`
- batch `check_nullifiers` request/response fixture shape
- send-capable reference flow fixture shape
- active circuit identity `privacy-note-v1`, authoritative `AssetRegistryV1` query shape, global cursor `(height, global_sequence, output_index)`를 포함한 `privacy-scan-v2` record
- canonical `privacy-fixed-v1` note/disclosure plaintext hex와 typed encrypted-envelope field

이 schema는 field presence, basic type, version constant, address prefix, fixed-size hash, 2-byte view tag hex string, 현재 transfer payload array size, scan cursor/version field, note reservation enum/transition array, HMAC lookup-key vector, Merkle path helper bit, canonical non-negative uint64 amount string, Cosmos SDK coin string을 확인합니다.

단, semantic verification을 대신하지는 않습니다. payload hash 재계산, disclosure digest 검증, `DISCLOSURE-BLINDING-SEPARATION`, sender self-view payload 복호화/검증, Merkle path 재계산, scan cursor 전진 동작, 안전한 view-tag mismatch fallback, proof verification은 SDK/test가 별도로 수행해야 합니다.

## NoteV1 및 BatchJoinSplit16x32 independent contract

아래 language-neutral fixture 3개가 wallet-shape schema를 보완합니다.

- `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`은 NoteV1 domain, domain constant, asset-ID/commitment/nullifier vector, exact empty root, `privacy-fixed-v1` size를 동결합니다.
- `x/privacy/client/sdk/conformance/testdata/privacy_batch_joinsplit_v1_contract.json`은 production 16/32 capacity, canonical 1..64-byte `audit_key_id` grammar `[a-z0-9][a-z0-9._-]*`, vector root, effect ID, exact canonical owner-effect digest, corrected max-shape wire-state value와 12개 public input을 동결합니다. 순서는 정확히 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`입니다. Independent payload SHA-256은 `f2588c...24b0`, max canonical payload는 `65,384` bytes입니다. Wire golden은 Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, query response `74,551` bytes입니다.
- `x/privacy/client/sdk/conformance/testdata/privacy_disclosure_blinding_v1_contract.json`은 `DBS-01..03`, active all-private/disabled sentinel, slot별 circuit separation과 더 강한 SDK global freshness의 구분, 필수 circuit/native/prepared/structured-signer enforcement layer, stable secret-free `DBS_*` error code를 동결합니다. 모든 vector의 `error_field`는 valid일 때 empty이고 invalid일 때 canonical offending request field를 지정합니다. `active_all_private_zero_randomness_valid`는 active output randomness가 zero일 수 있지만 all-private user blinding은 canonical zero sentinel이고 full-blinding separation은 계속 enabled임을 명시합니다.

Fixed binary contract는 exact합니다. Note plaintext는 350 bytes, disclosure plaintext는 392 bytes, typed encrypted-envelope header는 20 bytes입니다. JSON Schema는 주로 fixture shape를 검증합니다. SDK semantic test는 이 byte length, envelope kind/domain/version/reserved byte, trailing byte 금지, `AssetRegistryV1` resolve, full scan-cursor advancement, 선택한 root와 일치하는 Merkle path snapshot도 추가로 강제해야 합니다. Typed `privacy-scan-v2` record는 wrong exact event type, fixed envelope, digest, key, zero/disabled sentinel, orphan/non-adjacent output에서 fail closed합니다.

Current-root path query는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. 모든 non-current historical path는 persisted `(root, leaf_count, height)` metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical-path index를 사용합니다. Offline recovery/export는 별도 `MaxMerkleRebuildLeaves`(1,048,576) bound를 유지합니다. Complete persisted per-prefix snapshot metadata index가 있으면 모든 historical node를 rebuild하지 않고도 offline bound를 넘는 tree를 genesis export할 수 있습니다.

`BatchJoinSplit16x32`, `MsgBatchTransfer`, keeper handler, typed scan state, artifact descriptor는 production core contract입니다. `batch_feasibility.proto`는 measurement-only로 남습니다. Batch integration은 reference Go SDK/CLI, `POST /v1/proofs/batch-transfer`, Cosmos `MsgBatchTransfer`와 EVM `singleProofBatchTransfer`를 위한 ClairveilJS 0.3.1 contract를 추가했습니다. Example WebApp은 explicit server feature gate 뒤에서만 이 flow를 노출하며 encrypted recovery와 typed item/audit reconciliation을 요구합니다. 정정된 full-shape reference gate는 constraint `1,111,837`, peak RSS `3,339,862,016` bytes, max-shape warm proving `55.892 ms/output`, native 2x2 대비 per-output `2.789x` 개선을 측정했습니다. Artifact consumer는 `privacy-note-v1`을 pin해야 합니다. Validator는 exact consensus identity와 required VK를 사용하고 prover는 선택한 R1CS/PK pair를 lazy load합니다. Reference prover admission default는 circuit/service boundary별 in-flight 1개, queued 4개, positive 8 MiB request limit입니다.

Prepared transfer payload `v5`는 현재 outer prepared-payload version으로 그대로 유효합니다. Inner note/disclosure/envelope encoding `privacy-fixed-v1`과 별개이며 어느 version도 다른 version을 대체하지 않습니다. Compatibility fallback은 금지됩니다. ClairveilJS 0.3.1은 fixed fixture와 V5/V2 preparation/proof contract를 구현합니다. 아직 release되지 않은 WebApp은 current v0.3.1 namespace의 fresh initialization만 지원합니다. 이전 note-cache, reservation, operation, relay record를 migrate하지 않고 in-place downgrade를 지원하지 않습니다. Empty state에서 safe typed rescan을 완료합니다.
