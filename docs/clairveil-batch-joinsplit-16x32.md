# Clairveil NoteV1 and BatchJoinSplit16x32 Protocol Contract

> Legacy archive: this NoteV1/16x32 specification and its fixed fixtures are preserved conformance history. They are not the current V2 runtime or a live prover contract. For the current shared route, use [Proverd HTTP API](clairveil-proverd-http-api.md).

## 1. Status and scope

This document is normative only for the frozen legacy NoteV1 fixtures and inner 16-input/32-output relation: domain separation, fixed encodings, aggregate vector roots, disclosure digests, scan state, artifact identity, and resource accounting. Current runtime messages use `clairveil.privacy.v2.MsgBatchTransfer` and the audit-field wrapper/PI23 contract.

The repository implements the circuit/keeper and reference Go planner, prover, scanner, payroll workers and CLI. Its status is `PUBLICATION_READY_EXPERIMENTAL`; downstream JS/TS products, formal trusted setup, external audit, signed production artifacts and production operations remain separate requirements.

The frozen legacy circuit set is `privacy-note-v1`, in Deposit, Spend, JoinSplit2x2, `batch-joinsplit-16x32-v1` order. Development artifact identities are not production trust anchors. **MUST**, **MUST NOT**, **SHOULD**, and **MAY** carry their usual protocol meaning inside that frozen contract.

## 2. Frozen versions and capacities

| Contract | Frozen value |
| --- | --- |
| Frozen legacy circuit set | `privacy-note-v1` |
| Module consensus version | `2` |
| Privacy state version | `2` |
| Fixed payload version | `privacy-fixed-v1` / binary version `1` |
| Asset registry version | `privacy-asset-registry-v1` |
| Scan schema version | `privacy-scan-v2` |
| Global scan sequence version | `privacy-sequence-v1` |
| Audit key ID V1 | `1..64` lowercase ASCII bytes, `[a-z0-9][a-z0-9._-]*` |
| Note tree | BN254 Fr, MiMC, depth `32` |
| Batch capacity | inputs `1..16`, outputs `1..32` |
| Batch circuit ID | `batch-joinsplit-16x32-v1` |
| Batch public-input schema SHA-256 | `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333` |
| Legacy batch proto/API | `clairveil.privacy.v1.Msg/BatchTransfer`; canonical payload format `1` |
| Disclosure-blinding separation | `v1`; `DBS-01..03` plus exact all-private/disabled sentinels |
| JoinSplit2x2 public-input schema SHA-256 | `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82` (unchanged) |

These version changes require a fresh reset/genesis. Mixed old/new note state is rejected. Wallet note caches, reservations, prepared witnesses, and prepared proofs from an older circuit/payload/state version MUST be discarded.

## 3. NoteV1

### 3.1 Field-domain derivation

Every field-domain constant in this document is derived as:

```text
domain_field(label) =
  SHA-256(
    "clairveil.field-domain.v1" ||
    u32be(byte_length(label)) ||
    label
  ) mod Fr
```

Labels are exact ASCII/UTF-8 bytes. Lengths and integers are unsigned big-endian. A label identifies one semantic type and MUST NOT be reused for another hash primitive or meaning.

The NoteV1 labels and canonical 32-byte field encodings are:

| Use | Label | `domain_field(label)` |
| --- | --- | --- |
| note commitment | `clairveil.note-commitment.v1` | `0927abf70e775c0f9fd7db79a93b7f8e94621f15921f6b7077407ec5210cfb1c` |
| note nullifier | `clairveil.note-nullifier.v1` | `1a49a4bf6a216ef5dba9311200be7b1374794ba1ca759a7761e11ac6d774e0b9` |
| note-tree node | `clairveil.note-tree-node.v1` | `0e7215b6529f83eaf86ae8e5ad92eb2ec9f61f1dbd7077c54ff0fdd0e7bfd620` |

### 3.2 Commitment, nullifier, and tree

MiMC arguments are field elements in exactly the displayed order:

```text
note_commitment = MiMC(
  domain_field("clairveil.note-commitment.v1"),
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness,
)

note_nullifier = MiMC(
  domain_field("clairveil.note-nullifier.v1"),
  note_commitment,
  randomness,
  spend_pubkey_x,
  spend_pubkey_y,
)

merkle_parent(level, left, right) = MiMC(
  domain_field("clairveil.note-tree-node.v1"),
  level,
  left,
  right,
)
```

The commitment in the nullifier prevents accidental reuse of one randomness value across different notes from collapsing directly to the same nullifier.

The exact empty-tree contract is:

```text
empty_root[0] = 0
empty_root[level + 1] = merkle_parent(
  level,
  empty_root[level],
  empty_root[level],
)
```

The tree depth is `32`; level `0` combines leaves. Canonical empty roots include:

| Depth | Root |
| --- | --- |
| `1` | `2a9932954f9328683b24310f96581603f12544f6da3910aeefebbfa84789b296` |
| `2` | `29bae378ecc69a3c6e1c861407bd57c9c8cd34d37ebc2d4fe8c205952f62793a` |
| `32` | `057551a52590c07629bf07fa2b61832f852fb69ff8472bb21c30e5675ae8e8c1` |

An active note commitment and active nullifier MUST be non-zero. Zero is reserved for the empty leaf and disabled vector values.

Deposit, Spend, JoinSplit2x2, native note helpers, the keeper tree, fixed-payload scanner decoding, and the BatchJoinSplit feasibility circuit use these same formulas. No legacy JSON or legacy hash fallback is accepted.

### 3.3 Asset ID and AssetRegistryV1

An asset is represented inside circuits and notes by one field element:

```text
asset_id = SHA-256(
  "clairveil.asset-id.v1" ||
  u32be(byte_length(canonical_denom)) ||
  canonical_denom
) mod Fr
```

`canonical_denom` MUST pass the Cosmos denom rules and MUST have no surrounding whitespace. The reduction is intentional because circuits use a single BN254 field. Collision resistance therefore relies on SHA-256 plus the field-sized output; a collision MUST be treated as a registry conflict, never as an alias.

`AssetRegistryV1` is consensus state and is the authoritative bidirectional `canonical_denom <-> canonical 32-byte asset_id` mapping. Both directions MUST exist and agree. Registration rejects:

- an invalid denom or non-canonical field encoding;
- an ID that does not equal the formula above;
- any denom re-registration, including an identical-looking registration;
- any ID collision or inconsistent/corrupt reverse mapping.

Deposit and Withdraw require a registered denom. SDKs and UIs restore a display denom only through registry queries; ciphertext no longer carries a trusted denom, and local configuration MUST NOT silently override the registry. Fresh default genesis registers `uclair`, whose asset ID is:

```text
238d5f23e4d918d40b0982ce3aef16a75c4d1760193d1c3b30b9f5df681903ca
```

Adding a governance message for new assets remains outside the batch protocol contract.

### 3.4 Public-key and signature validation

Every shielded spend, view, disclosure, and EdDSA `R` point accepted from a wire boundary MUST satisfy all of the following:

1. exact 32-byte compressed encoding;
2. decode and byte-for-byte canonical re-encoding;
3. on-curve;
4. not the identity;
5. prime subgroup membership, `[SubgroupOrder]P = identity`.

An EdDSA signature is exact 64-byte `R || S`; `R` follows the point rules and `0 < S < SubgroupOrder`.

The circuits independently enforce on-curve, non-identity, and subgroup membership. The production batch circuit validates the owner spend/view points, signature `R`, and all 32 output spend/view pairs. Every input key equals the single owner key; disabled key slots use the same owner key sentinel. These circuit constraints MUST NOT be weakened to host-only checks as a performance shortcut.

### 3.5 Independent golden NoteV1 vector

For spend scalar `17`, view scalar `19`, amount `7`, denom `uclair`, and randomness `13`:

| Value | Canonical hex |
| --- | --- |
| commitment | `023aab554dcb995210888fa4e28c3d718568c1de0623578c690a2b6ca9d3610a` |
| nullifier | `13b50fceae57ce77eee3f686abc1563aadc27ff6d1e32ce2fcc599463d28585b` |

The independent fixture is `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`.

## 4. Frozen BatchJoinSplit16x32 statement

### 4.1 Public inputs

The public witness has exactly 12 elements in this exact order and encoding:

| # | Name | Encoding |
| ---: | --- | --- |
| 1 | `MerkleRoot` | `bn254-fr` |
| 2 | `ChainDomainHi` | `uint128` |
| 3 | `ChainDomainLo` | `uint128` |
| 4 | `ExpiresAtUnix` | `uint64`, non-zero |
| 5 | `InputCount` | `uint5`, constrained to `1..16` |
| 6 | `OutputCount` | `uint6`, constrained to `1..32` |
| 7 | `NullifierRoot` | `bn254-fr` |
| 8 | `CommitmentRoot` | `bn254-fr` |
| 9 | `UserDisclosureRoot` | `bn254-fr` |
| 10 | `FullDisclosureRoot` | `bn254-fr` |
| 11 | `PayloadDigestHi` | `uint128` |
| 12 | `PayloadDigestLo` | `uint128` |

The individual nullifiers, commitments, and disclosure digests are message data. A keeper MUST recompute their ordered aggregate roots and compare them to this public witness before verification.

### 4.2 Owner intent

All active input notes have one owner, so the batch has exactly one owner EdDSA signature. The signed field is:

```text
batch_intent = MiMC(
  domain_field("clairveil.batch-transfer-intent.v1"),
  chain_domain_hi,
  chain_domain_lo,
  domain_field("clairveil.batch-joinsplit-16x32.v1"),
  merkle_root,
  input_count,
  output_count,
  asset_id,
  nullifier_root,
  commitment_root,
  user_disclosure_root,
  full_disclosure_root,
  payload_digest_hi,
  payload_digest_lo,
  expires_at_unix,
)
```

`chain_domain_hi/lo` comes from the existing chain-domain contract and binds the chain ID and active circuit-set ID. The circuit constrains both chain limbs and both payload limbs to 128 bits and expiry to a non-zero 64-bit value. Keeper validation in the batch chain core MUST also reject an expired message.

### 4.3 Exact active prefix and disabled sentinels

For capacity `C` and public `count`, the circuit constructs a one-hot vector over values `1..C`, asserts its sum is one, and derives:

```text
enabled[i] = 1 iff i < count
```

Thus active slots are exactly one contiguous prefix; holes and count `0` are impossible.

Input sentinels for every disabled slot are:

| Field | Disabled value |
| --- | --- |
| spend public key | owner spend public key |
| view public key | owner view public key |
| amount | `0` |
| randomness | `0` |
| each of 32 Merkle siblings | `0` |
| each of 32 path-helper bits | `0` |
| exported nullifier value | `0` with `enabled=0` |

Membership final-root equality is gated by `enabled`, but path-helper booleanity and all disabled sentinels remain constrained. Every active input computes both a non-zero note commitment and a non-zero nullifier. Distinctness applies to every pair for which both inputs are active.

Output sentinels for every disabled slot are:

| Field | Disabled value |
| --- | --- |
| spend public key | owner spend public key |
| view public key | owner view public key |
| amount | `0` |
| randomness | `0` |
| privacy policy/bitmap | `0` (`all-private`) |
| user disclosure blinding | `0` |
| full disclosure blinding | `0` |
| commitment/user/full vector value | `0` with `enabled=0` |
| payload, target, and other helpers | absent/zero in the canonical message view |

Every output point, including the canonical key sentinel, is checked in-circuit. Every active output computes a non-zero commitment. Commitment distinctness applies to every pair for which both outputs are active. Amounts are unsigned 64-bit values, and the active input sum MUST equal the active output sum. An active zero-amount output remains distinguishable from a disabled slot through `enabled=1`; a non-zero commitment and full-disclosure blinding are still required. Randomness remains a canonical field value and is distinguished from a disabled slot by the enabled bit rather than by a non-zero rule.

### 4.4 Ordered vector roots

Let `T` be one of `nullifier`, `commitment`, `user_disclosure`, or `full_disclosure`. Its capacity is `16` for nullifiers and `32` for all other vectors. `count` MUST be in `1..capacity`. Values contain exactly the full capacity; disabled suffix values are zero.

```text
leaf[i] = MiMC(
  domain_field("clairveil.batch-vector." || T || ".leaf.v1"),
  i,
  enabled[i],
  value[i],
)

node[level] = MiMC(
  domain_field("clairveil.batch-vector." || T || ".node.v1"),
  level,
  left,
  right,
)

root = MiMC(
  domain_field("clairveil.batch-vector." || T || ".root.v1"),
  capacity,
  count,
  fixed_tree_root,
)
```

Leaves are paired left-to-right. Level `0` combines leaves; the nullifier tree has depth `4`, and the three 32-capacity trees have depth `5`. Type, capacity, count, position, enabled state, and value are therefore all committed.

Every active outer vector value, including `user_disclosure`, MUST be non-zero. Disabled values for every type MUST be zero. The all-private special case applies to the inner raw user digest, not to the outer user value; §5.2 defines that two-stage contract.

The independent nullifier-vector fixture with count `3` and active values `11,13,17` has root:

```text
065354bf1bf6dd8719b40b4c4dc561f437845a426cc2c086a8676a725a13e593
```

## 5. Per-output disclosure contract

### 5.1 Policy bitmap

The three policy bits are:

| Bit | Value | Meaning |
| --- | ---: | --- |
| amount | `1` | disclose amount |
| to | `2` | disclose recipient spend/view keys |
| from | `4` | disclose sender spend/view keys |

Policies `1..7` use the corresponding bitwise combination. Policy `0` is all-private. In V1, `disclosed_field_bitmap` MUST equal `policy`. For every non-all-private user disclosure, the asset ID is always present; only amount, sender keys, and recipient keys are policy-selected.

### 5.2 User disclosure digest

User disclosure uses two explicit hashing stages. For an active output with policy `1..7`, the first stage is:

```text
raw_user_digest[i] = MiMC(
  domain_field("clairveil.user-disclosure.v2"),
  i,
  commitment[i],
  policy[i],
  disclosed_field_bitmap[i],
  selected_amount[i],
  selected_sender_spend_x[i],
  selected_sender_spend_y[i],
  selected_sender_view_x[i],
  selected_sender_view_y[i],
  selected_recipient_spend_x[i],
  selected_recipient_spend_y[i],
  selected_recipient_view_x[i],
  selected_recipient_view_y[i],
  asset_id,
  user_disclosure_blinding[i],
)
```

An unselected amount, sender key, or recipient key is the exact zero sentinel. `asset_id` is always the actual asset ID for every non-all-private plaintext/digest and is not policy-selected. `user_disclosure_blinding[i]` MUST be a fresh, non-zero, per-output CSPRNG field value. Policy `0` instead has raw digest `0`, bitmap `0`, all selected fields including the inner asset field `0`, blinding `0`, mode `NONE`, and no target or payload.

The second stage is the per-output user value:

```text
user_value[i] = MiMC(
  domain_field("clairveil.user-disclosure-leaf.v1"),
  i,
  enabled[i],
  policy[i],
  raw_user_digest[i],
)
```

For every active output, including all-private, `enabled=1` and `user_value[i]` MUST be non-zero. Thus an active all-private output has `(policy=0, raw_user_digest=0)` but a non-zero domain-separated outer value. A disabled output has policy `0`, raw digest `0`, and uses the literal outer value `0` rather than the hash above. The resulting `user_value[i]` is then committed again as `value[i]` by the generic `clairveil.batch-vector.user_disclosure.{leaf,node,root}.v1` tree in §4.4.

### 5.3 Full disclosure digest

Every active output has:

```text
full_digest[i] = MiMC(
  domain_field("clairveil.full-disclosure.v2"),
  i,
  commitment[i],
  amount[i],
  asset_id,
  sender_spend_x,
  sender_spend_y,
  sender_view_x,
  sender_view_y,
  recipient_spend_x[i],
  recipient_spend_y[i],
  recipient_view_x[i],
  recipient_view_y[i],
  full_disclosure_blinding[i],
)
```

`full_disclosure_blinding[i]` MUST be a fresh, non-zero, per-output CSPRNG value. For an active disclosed user plane, `user_disclosure_blinding[i] != output_randomness[i]`. For every active output, `full_disclosure_blinding[i] != output_randomness[i]` and `full_disclosure_blinding[i] != user_disclosure_blinding[i]`. These three per-slot relations are 96 explicit inequality checks at capacity; they prevent exact secret reuse but do not replace the CSPRNG independence requirement. The mandatory auditor envelope and optional self-view envelope carry evidence for this same digest; no separate self-view root exists. Self-view is batch-level all-or-none and defaults to enabled.

Per-output secret blindings prevent an observer from testing a small amount/address dictionary against public digests. Public disclosure intentionally reveals its selected plaintext and blinding. Encrypted recipient, auditor, and self-view plaintexts include the corresponding blinding so a recipient can recompute the proof-bound digest.

The proof binds digests and the canonical payload bytes, but it cannot prove that a ciphertext is decryptable by the claimed target key. Auditor decryption failure therefore requires an `AuditDeliveryFailed`/manual-review path; it MUST NOT be silently treated as valid delivery.

### 5.4 Disclosure-blinding separation across 2x2 and 16x32

`DISCLOSURE-BLINDING-SEPARATION` is a per-disclosure-output-slot invariant. For one enabled output slot `i`:

```text
user_enabled[i] = enabled[i] && (privacy_policy[i] != 0)

DBS-01: user_enabled[i] => user_disclosure_blinding[i] != output_randomness[i]
DBS-02: enabled[i]      => full_disclosure_blinding[i] != output_randomness[i]
DBS-03: enabled[i]      => full_disclosure_blinding[i] != user_disclosure_blinding[i]
```

An enabled non-all-private slot requires both blindings to be canonical and non-zero. An enabled all-private slot canonicalizes `privacy_policy=0` and `user_disclosure_blinding=0`; only `DBS-01` is gated off, while full blinding remains non-zero and `DBS-02`/`DBS-03` remain active. A disabled capacity slot canonicalizes policy, output randomness, user blinding, and full blinding all to zero and gates off all three inequalities. Active output randomness itself is a canonical field value and MAY be zero.

BatchJoinSplit16x32 already enforces this exact contract independently for each of 32 capacity slots: 96 gated inequality sites plus sentinel/non-zero checks. JoinSplit2x2 has no disabled output capacity slot. Its disclosure witness applies only to recipient output `0`, so `i=0` and the compared note secret is exactly `OutputRandomness[0]`. Output `1` is an active change note with no user/full disclosure witness; it is not a disabled disclosure slot. Input randomness, `OutputRandomness[1]`, cross-output reuse, and cross-transaction reuse are outside `DBS-01..03`. The stronger SDK-wide `SECRET-FRESHNESS` policy is separate. `BATCH-SIGNER-SECRET-FRESHNESS` closed the structured batch signer boundary by enforcing that policy before signature release, and the client-integration work added rejection of non-canonical BN254 aliases before the same boundary, as confirmed by independent publication validation.

The shared native/prepared/structured-signer error contract uses the following stable, secret-free codes. Go callers preserve `*DisclosureBlindingErrorV1` through wrapping; external adapters map validation failure to their existing non-retryable invalid-request response without echoing secret values.

| Code | Meaning |
| --- | --- |
| `DBS_INVALID_POLICY` | policy is outside `0..7` |
| `DBS_NON_CANONICAL_FIELD` | randomness or blinding is not a canonical BN254 field element |
| `DBS_DISABLED_SENTINEL` | a disabled slot has a non-zero policy/randomness/blinding |
| `DBS_ALL_PRIVATE_USER_SENTINEL` | an active all-private slot has non-zero user blinding |
| `DBS_USER_BLINDING_REQUIRED` | an enabled user disclosure has zero blinding |
| `DBS_FULL_BLINDING_REQUIRED` | an active output has zero full blinding |
| `DBS_USER_RANDOMNESS_REUSE` | `DBS-01` failed |
| `DBS_FULL_RANDOMNESS_REUSE` | `DBS-02` failed |
| `DBS_USER_FULL_BLINDING_REUSE` | `DBS-03` failed |

The required enforcement layers are circuit, `ValidateDisclosureBlindingSeparationV1`, prepared-payload validation, and the structured signer before signature release. `JoinSplitOwnerIntentSigningRequestV1` carries the final intent/effect plus both input NoteV1 values, both output NoteV1 values, output-0 policy, sender public-key projection, randomness, and blindings. `ValidateJoinSplitOwnerIntentSigningRequestV1` rebuilds ordered input nullifiers, both output commitments, value conservation, change ownership, and user/audit disclosure digests, compares them with the final effect, recomputes the chain domain, payload digest, and intent, and applies the shared `DBS_*` relation. `SignValidatedJoinSplitOwnerIntentV1` does not invoke the signer callback when validation fails. This host boundary complements, and does not substitute for, the production circuit constraints.

## 6. Canonical fixed-size payloads

All multibyte integers and field values are unsigned big-endian. Every field element is exactly 32 canonical BN254-Fr bytes. Each 16-byte domain tag is the first 16 bytes of `SHA-256(label)`.

### 6.1 NotePlaintextV1 — exactly 350 bytes

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | domain tag for `clairveil.note-plaintext.v1` |
| 16 | 2 | binary version `1` |
| 18 | 2 | flags/reserved, both zero |
| 20 | 32 | recipient spend X |
| 52 | 32 | recipient spend Y |
| 84 | 32 | recipient view X |
| 116 | 32 | recipient view Y |
| 148 | 8 | amount (`uint64`) |
| 156 | 32 | asset ID |
| 188 | 32 | randomness |
| 220 | 2 | memo byte length (`0..128`) |
| 222 | 128 | UTF-8 memo followed by zero padding |

Memo is local metadata and is not part of `note_commitment`. `NewNote`, `Note.ValidateV1`, and `MarshalNotePlaintextV1` all reject invalid UTF-8 and memo values above 128 bytes. Go callers must use the fallible `MarshalNotePlaintextV1`; the silent `Note.Bytes()` helper is not part of the contract. A decoder rejects invalid UTF-8, non-zero padding, non-canonical fields, bad keys, zero active commitment/nullifier, wrong length/version/domain/reserved bytes, and trailing data.

### 6.2 DisclosurePlaintextV1 — exactly 392 bytes

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | domain tag for `clairveil.disclosure-plaintext.v1` |
| 16 | 2 | binary version `1` |
| 18 | 1 | plane: user `1`, full `2` |
| 19 | 1 | reserved zero |
| 20 | 4 | output index |
| 24 | 4 | policy; full plane uses `0xffffffff` |
| 28 | 4 | disclosed-field bitmap; full plane uses `7` |
| 32 | 32 | commitment |
| 64 | 8 | selected/full amount |
| 72 | 32 | asset ID |
| 104 | 32 | sender spend X |
| 136 | 32 | sender spend Y |
| 168 | 32 | sender view X |
| 200 | 32 | sender view Y |
| 232 | 32 | recipient spend X |
| 264 | 32 | recipient spend Y |
| 296 | 32 | recipient view X |
| 328 | 32 | recipient view Y |
| 360 | 32 | disclosure blinding |

User-plane policies are `1..7`. The asset ID MUST be the actual asset ID, while amount, sender keys, and recipient keys not selected by the bitmap MUST be zero. Full plane uses all fields and a non-zero blinding. Denom strings are absent; the registry is authoritative.

### 6.3 EncryptedEnvelopeV1

The envelope header is exactly 20 bytes:

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | domain tag for `clairveil.encrypted-envelope.v1` |
| 16 | 2 | binary version `1` |
| 18 | 1 | kind |
| 19 | 1 | reserved zero |

| Kind | Value | Encryption | Exact envelope bytes |
| --- | ---: | --- | ---: |
| deposit note | `1` | symmetric nonce `12` + tag `16` | `398` |
| transfer note | `2` | ECIES point `32` + nonce `12` + tag `16` | `430` |
| user disclosure | `3` | ECIES point `32` + nonce `12` + tag `16` | `472` |
| audit disclosure | `4` | same ECIES framing | `472` |
| self-view disclosure | `5` | same ECIES framing | `472` |

The decoder validates kind-specific exact ciphertext length before allocation/use. Deposit, JoinSplit2x2 recipient/change, scanner parsing, CLI output, disclosure builders, and conformance fixtures use this encoding. Legacy JSON plaintext and raw unversioned ciphertext are rejected.

## 7. Structured batch wire and effect contract

### 7.1 Production message shape

`MsgBatchTransfer` uses this field set; `BatchTransferWirePrototypeV1` remains only the independent wire/resource measurement mirror:

```text
creator
proof
root
nullifiers[]
outputs[]
audit_key_id
audit_key_epoch
audit_disclosure_target_pubkey
expires_at_unix
```

Each `BatchTransferOutput` contains:

```text
commitment
ciphertext
view_tag
user_privacy_policy
user_disclosure_mode
user_disclosure_digest
user_disclosure_target_pubkey
user_disclosure_payload
full_disclosure_digest
audit_disclosure_payload
self_view_disclosure_payload
```

`input_count = len(nullifiers)` and `output_count = len(outputs)`; counts are not duplicated on the wire. The audit identity/target is batch-level. A self-view target is not published. The full digest is reused for auditor and self-view evidence.

`audit_key_id` is exact lowercase ASCII matching `[a-z0-9][a-z0-9._-]*` and is `1..64` bytes. The first byte cannot be punctuation. A batch audit epoch MUST be positive, and the audit target MUST be a canonical, non-identity, prime-subgroup compressed point. The max-wire measurement uses a valid 64-byte ID, not an unbounded placeholder.

The protobuf is registered as `clairveil.privacy.v1.Msg/BatchTransfer`. The keeper never hashes generated protobuf bytes: it reuses the frozen canonical owner-authorized effect view below. Let `lp(x) = u32be(len(x)) || x`; empty optional fields therefore have the unique encoding `u32be(0)`. For each output, `output_effect_i` is the listed output fields in exactly this order:

```text
output_effect_i =
  lp(commitment_i) || lp(ciphertext_i) || lp(view_tag_i) ||
  u32be(user_privacy_policy_i) || u32be(user_disclosure_mode_i) ||
  lp(user_disclosure_digest_i) || lp(user_disclosure_target_pubkey_i) ||
  lp(user_disclosure_payload_i) || lp(full_disclosure_digest_i) ||
  lp(audit_disclosure_payload_i) || lp(self_view_disclosure_payload_i)

canonical_batch_payload_v1 =
  u32be(1) ||
  lp(root) ||
  u32be(input_count) || lp(nullifier_0) || ... || lp(nullifier_{input_count-1}) ||
  u32be(output_count) || output_effect_0 || ... || output_effect_{output_count-1} ||
  lp(audit_key_id ASCII bytes) ||
  u64be(audit_key_epoch) ||
  lp(audit_disclosure_target_pubkey) ||
  u64be(expires_at_unix)

payload_sha256 = SHA-256(
  "clairveil.batch-transfer-payload.v1" || canonical_batch_payload_v1
)
PayloadDigestHi = uint128be(payload_sha256[0:16])
PayloadDigestLo = uint128be(payload_sha256[16:32])
```

`creator` and `proof` are the only message fields excluded. Ordered-vector lengths are the canonical counts, so there is no second count source. Validation rejects non-canonical fields, duplicate nullifiers/commitments, unknown disclosure enums, invalid policy/mode/target/payload combinations, malformed envelopes, mixed self-view presence, invalid audit identity, and non-positive epoch/expiry before encoding. The independent 2-input/2-output golden is `3,702` canonical bytes with SHA-256 `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`, high limb `322132945931579789235567236199104333743`, and low limb `14314064343031468430392382204273370288`. The max `16/32` shape is `65,384` canonical bytes.

The consensus per-message wire cap is `128 KiB`. `BatchTransferRawFramingDecorator` scans the signed raw `TxBody.messages[].Any` fields, including nested governance and authz wrappers, and requires exactly one type URL and value for every batch message. Decoded `MsgBatchTransfer.Size()` is only a secondary shape check. BaseApp calls `ValidateBasic` before ante handling, so batch `ValidateBasic` deliberately performs only bounded framing and creator validation; complete effect semantics run after the keeper's deterministic precharge. This rejects oversized or duplicate raw values without performing uncharged point/envelope validation.

### 7.2 Batch effect ID

The logical effect ID is independent of proof randomness, relayer, creator, tx hash, and block placement:

```text
batch_effect_id = SHA-256(
  "clairveil.batch-effect.v1" ||
  field32(chain_domain_hi) ||
  field32(chain_domain_lo) ||
  field32(merkle_root) ||
  u32be(input_count) ||
  u32be(output_count) ||
  field32(nullifier_root) ||
  field32(commitment_root) ||
  field32(user_disclosure_root) ||
  field32(full_disclosure_root) ||
  field32(payload_digest_hi) ||
  field32(payload_digest_lo) ||
  u64be(expires_at_unix)
)
```

The independent golden effect ID is:

```text
7f76d7744607e06dc0a22e4be5464e1a420c933cff5d060cc657ccfd4ec45979
```

### 7.3 Minimal event and typed state

The production batch ABCI event contains only effect ID, relayer, input/output counts, the four aggregate roots, expiry, circuit/payload/scan versions, and exact audit ID/epoch/target. It MUST NOT repeat ciphertexts, disclosure payloads, commitments, or full nullifier lists as hex attributes.

Typed state stores one summary and one raw-byte output record per output. Summary/output identity includes the global cursor, effect ID, circuit-set ID, payload version, scan schema version, audit key ID/epoch/target, tx hash, and event type. Output state additionally includes commitment, ciphertext, view tag, leaf index, policy/mode, disclosure digests/targets/payloads, and optional self-view payload.

### 7.4 Exact typed scan event contract

Typed scan state is validated by event type, not only by protobuf shape:

| Event | Summary contract | Output contract |
| --- | --- | --- |
| Deposit | one output; no nullifier/effect ID; audit ID/epoch/target are zero sentinels | exact deposit-note envelope in `encrypted_note`; empty `ciphertext`/view tag; every disclosure field is the zero sentinel |
| Withdraw | one nullifier; zero outputs; no effect ID; zero audit sentinel | no output record; its summary is still returned by scan |
| JoinSplit2x2 | exactly two nullifiers and outputs; no effect ID; audit ID/epoch are zero, audit target is a canonical point | exact transfer-note envelope and 2-byte view tag; change output uses exact zero disclosure sentinels; disclosed output follows the user/full rules below |
| Batch V1 production event | `1..16` nullifiers, `1..32` outputs, non-zero 32-byte effect ID, canonical audit ID, positive epoch, canonical target point | exact transfer-note envelope and 2-byte view tag; every output follows the user/full rules below |

For all-private user disclosure, mode is `NONE` and digest/target/payload are empty. For public disclosure, target is empty, `DisclosurePlaintextV1` must match output index/policy/commitment, and its digest is recomputed. For recipient-encrypted disclosure, target is a canonical point and the payload is an exact user-disclosure envelope. A disclosure-bearing output has a non-zero canonical full digest, exact audit envelope, and either an empty or exact self-view envelope. Every output commitment and leaf index must match commitment state, and summary/output identity and audit fields must be byte-for-byte equal.

Output keys for one `(height, global_sequence)` MUST form exactly the contiguous prefix `0..output_count-1`. Store and query reject missing records, non-adjacent/extra/orphan records anywhere under the event prefix, malformed envelopes, invalid points/digests, and non-canonical sentinels.

## 8. Consensus state and scanning

### 8.1 Global commitment uniqueness

The commitment index is global across Deposit, JoinSplit2x2, BatchJoinSplit16x32, and genesis. A commitment MUST be canonical, non-zero, and absent before proof/state execution. After deterministic precharge, `ValidateMsgBatchTransferEffectsV1` rejects local duplicates; the keeper checks the global index before proof verification; and `AppendCommitment` repeats the check, records one immutable leaf index, and rejects a duplicate. Index lookup propagates store errors and rejects malformed stored indices instead of treating them as absent. Genesis also requires globally distinct commitments. Batch outputs are appended only after proof success.

### 8.2 Unified sequence and cursor

Deposit, JoinSplit2x2, and BatchJoinSplit16x32 operations share one monotonically increasing global privacy sequence. The scan cursor is lexicographically ordered by:

```text
(height, global_sequence, output_index)
```

There is no separate batch cursor. The query is summary-driven: it iterates summaries first, validates exactly `summary.output_count` ordered output records, and rejects stray or missing records. A zero-output Withdraw still returns its summary and advances the cursor. A page may resume inside a multi-output operation without duplicates or omissions; `summary.output_count` together with `has_more=true` explicitly marks an output prefix as incomplete, so a partial batch MUST NOT be reported as item success. Typed-record corruption, an identity mismatch, or a query byte-budget failure is fail-closed; clients retry the typed query and MUST NOT fall back to a payload-less event as if it were complete.

Frozen query bounds are:

| Bound | Default | Maximum |
| --- | ---: | ---: |
| outputs | `128` | `512` |
| events | `64` | `256` |
| encoded response bytes | `1 MiB` | `4 MiB` |
| one stored summary/output record | — | `1 MiB` |

### 8.3 Same-root path snapshots

`CommitmentPathsAtRoot` accepts `1..16` distinct canonical commitments, an exact historical root, and optional snapshot height. It returns all 32-level paths with leaf indices from one immutable `(root, height, leaf_count)` prefix. Root, height, and leaf count MUST agree; each returned path MUST reconstruct the requested root.

Every commitment append persists the resulting root together with its exact leaf count and block height. This snapshot is authoritative metadata, but historical internal tree nodes are not persisted. Current-root requests read only the persisted incremental nodes. If the cached current root or a required incremental node is missing, the query never rebuilds or writes state: it fails closed (`FailedPrecondition` for a missing cached root) and requires explicit offline repair. Every non-current historical path performs a deterministic prefix rebuild using the same NoteV1 node/empty-root helpers. The public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`; larger online requests require the current root or an archival/local tree provider. Offline recovery/export retains the separate `MaxMerkleRebuildLeaves` (1,048,576) bound. A remote multi-note path query reveals input-note linkage to that provider and SHOULD be replaced by a trusted local provider where privacy requirements demand it.

### 8.4 Genesis/reset

Genesis export/import preserves commitments and indices, historical roots and one persisted root/count/height snapshot for every commitment prefix, nullifiers, asset registry, global sequence, privacy scan summaries/outputs, privacy event records, reserve counters, and exact circuit identity. Asset-registry export walks and cross-validates both forward and reverse namespaces, so an orphan or malformed reverse key fails closed instead of disappearing from the export. Trees larger than `1,048,576` leaves export from the complete persisted snapshot index and do not depend on bounded rebuilding. Smaller exports may rebuild only to cross-check persisted metadata. Historical roots are checked against commitment prefixes. Corruption, a missing large-tree prefix snapshot, mixed state version, duplicate fields, or mismatched circuit identity fails closed.

## 9. Circuit identity, artifact loading, and prover admission

### 9.1 Consensus artifact identity

Consensus stores, in fixed Deposit/Spend/JoinSplit/BatchJoinSplit16x32 order:

```text
circuit_set_id
circuit_id
verifying_key_sha256
public_input_schema_sha256
```

Validator readiness requires the complete local manifest identity to equal consensus and loads only the requested verifying keys. An environment checksum cannot override consensus, even in development. Prover readiness loads only requested R1CS/proving-key pairs and may use an explicit checksum override only in a development runtime. Production override is rejected. Files are SHA-256 checked, fully decoded with no trailing bytes, and verifying keys must round-trip canonically.

The registry is injectable, thread-safe, lazy, and cached separately by circuit and artifact type. `batch-joinsplit-16x32-v1` is the fourth entry in `RequiredCircuitIDs` and contributes three descriptors to the canonical 12-descriptor manifest. Validators load only requested VKs; provers load only selected R1CS/PK pairs.

`DISCLOSURE-BLINDING-SEPARATION` narrows the accepted 2x2 witness set but does not change NoteV1, the 13 public inputs/order, the JoinSplit public-input schema digest, `TransferIntentV2`, disclosure digest formulas/domains, `privacy-fixed-v1`, canonical message payload bytes, protobuf, prepared transfer payload `v5`, proof/HTTP contract `v2`, manifest schema `v2`, identity schema `v1`, or circuit-set ID `privacy-note-v1`. The batch chain core regenerated only the JoinSplit R1CS/PK/VK, replaced its manifest checksums and consensus `verifying_key_sha256`, and verified old/new proof and consensus/file mismatch plus fresh-genesis/reset readiness. Cached old JoinSplit proofs/jobs must be discarded; existing prepared payloads may be re-proved only after the new semantic validator accepts them. BatchJoinSplit16x32 source/artifacts and the other nine artifact files remain byte-identical.

The batch chain core development identity was generated from source commit `381c984189e823e5797104eb7cd2beb2386eaf80` at `2026-07-11T09:32:32Z`. It is reproducibility evidence only:

| Batch artifact | Size | SHA-256 |
| --- | ---: | --- |
| `privacy_batch_joinsplit_16x32_r1cs.bin` | `122,813,535 B` | `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210` |
| `privacy_batch_joinsplit_16x32_pk.bin` | `209,218,621 B` | `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470` |
| `privacy_batch_joinsplit_16x32_vk.bin` | `716 B` | `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621` |

Generation peaked at `3,308,797,952 B` RSS. The opt-in role-readiness gate peaked at `1,295,482,880 B`, decoded only the batch VK for the validator role, only batch R1CS/PK for the prover role, and confirmed `1,111,837` constraints plus public-schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`. No generated binary is tracked.

The `DISCLOSURE-BLINDING-SEPARATION` JoinSplit-only development rotation was generated from implementation commit `25c17ef5249703822455273a7c683694e70aabf4` at `2026-07-12T12:51:14Z` with gnark `v0.14.0`, Groth16/BN254 development `groth16.Setup`, and `clairveil-setup -circuit joinsplit -overwrite`:

| JoinSplit artifact | Size | SHA-256 |
| --- | ---: | --- |
| `privacy_joinsplit_r1cs.bin` | `10,824,169 B` | `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4` |
| `privacy_joinsplit_pk.bin` | `16,766,489 B` | `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7` |
| `privacy_joinsplit_vk.bin` | `748 B` | `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112` |

The VK hash is the consensus JoinSplit `verifying_key_sha256`; the public-input schema remains `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`. This is reproducibility evidence for local development only, not a formal trusted setup, and the generated binaries and secrets are not tracked.

### 9.2 Prover admission

Default admission is independent per production circuit:

| Setting | Default |
| --- | ---: |
| max in-flight per circuit | `1` |
| max queued per circuit | `4` |
| request body hard limit | `8 MiB` |

Admission is acquired after cheap HTTP/framing checks and before typed semantic/cryptographic decode. A full queue returns HTTP `429` with `code="busy"` and `retryable=true`; only this capacity error is retryable. Metrics expose in-flight, queued, proving, admitted/rejected/canceled/released totals, queue wait, and prove duration.

The permit is held until the actual gnark prove returns. Client cancellation does not release capacity while a non-cancelable prove is still executing. Sensitive gnark solver output is discarded, and automatic multi-prover failover remains off. Process isolation is still required for production hard cancellation/resource containment.

### 9.3 Frozen gas/resource formula

```text
batch_gas = verify_base
          + per_input * input_count
          + per_output * output_count
          + per_canonical_payload_byte * canonical_payload_bytes
          + per_typed_scan_state_byte * typed_scan_state_bytes
          + per_tree_write * tree_node_writes
          + per_global_lookup * global_lookups
```

All coefficients and all resource bounds MUST be positive. Usage must be within `1..16` inputs and `1..32` outputs; tree writes cannot be fewer than outputs, and global lookups cannot be fewer than inputs plus outputs. Multiplication and summation are checked for `uint64` overflow.

The batch chain core freezes these conservative V1 coefficients and bounds:

| Component | V1 value |
| --- | ---: |
| verify base | `1,000,000` gas |
| per input | `25,000` gas |
| per output | `50,000` gas |
| per canonical payload byte | `4` gas |
| per typed state byte | `8` gas |
| per tree node write | `5,000` gas |
| per global lookup | `10,000` gas |
| canonical payload bound | `65,384 B` |
| typed state bound | `256 KiB` |
| tree-write bound | `1,056` nodes |
| global-lookup bound | `48` |

The explicit surcharge pays for privacy-specific proof verification, canonical hashing/encoding, scan-state growth, Merkle computation/bookkeeping, and global uniqueness checks. Cosmos KV gas still pays for the underlying store reads and writes; the coefficients do not replace it. Thus the two meters cover different layers even when one logical operation causes both computation and physical I/O. The exact category breakdown and precharge-before-semantics/out-of-gas behavior are regression-tested. A real `1/1` handler success and a max `16/32` post-proof scan projection exercise the explicit descriptor separately from every Cosmos KV descriptor, preventing either layer from silently absorbing or duplicating the other's responsibility. Independent publication validation confirmed the experimental reference bounds; target-chain production coefficient governance and calibration remain a production-owner TODO.

## 10. Max wire/state feasibility result

The measured max shape used 16 nullifiers, 32 outputs, a maximum valid 64-byte audit key ID and canonical target point, mandatory auditor payloads, maximum recipient disclosure and self-view envelopes, proof/root/tags/keys/digests, an actual Cosmos `TxRaw`, typed scan summary/output protobufs and KV keys, a 96 KiB tree-write allowance, and the minimal ABCI event.

| Item | Measured | Frozen reference limit | Result |
| --- | ---: | ---: | --- |
| canonical owner-effect payload | `65,384 B` | `4 MiB` gRPC body | PASS |
| prototype message | `65,060 B` | `4 MiB` gRPC body | PASS |
| signed `TxRaw` | `65,294 B` | `1 MiB` tx; `21 MiB` block bytes | PASS |
| typed scan summary | `788 B` | included below | PASS |
| 32 typed scan outputs | `73,628 B` | included below | PASS |
| typed scan KV including keys | `75,105 B` | `256 KiB` | PASS |
| tree-write upper allowance | `98,304 B` | included below | PASS |
| total KV write | `173,409 B` | `256 KiB` | PASS |
| minimal ABCI event | `584 B` | `16 KiB` | PASS |
| max query response | `74,551 B` | `4 MiB` | PASS |

**Wire/state gate conclusion: PASS. Combined protocol gate conclusion: PASS.** The batch chain core may retain the 16/32 capacity and security constraints. The values are feasibility limits, not a license to omit per-message hard limits, explicit gas, or state-growth monitoring.

## 11. Implemented keeper order

The production handler executes in this order:

1. cheap bounded count/length/message-size framing;
2. fixed-size canonical proof framing;
3. deterministic explicit gas precharge;
4. full canonical field/point/envelope/disclosure validation and local duplicate rejection;
5. exact chain audit ID/epoch/target match;
6. global spent-nullifier and commitment-uniqueness checks;
7. strict historical-root lookup and whole-batch Merkle-capacity check;
8. aggregate roots, canonical payload limbs, chain domain, expiry, and all 12 public values derived from message/context;
9. proof/public-witness verification;
10. a nested cache writes nullifiers, commitments, root snapshots, typed scan summary/outputs, and the minimal event;
11. the nested cache is committed only when every write succeeds.

No batch state write may occur before proof success. The message is all-or-nothing.

## 12. Residual risks and explicit non-goals

- The batch chain core and batch reference Go client/prover/scanner/payroll/CLI surfaces passed the independent publication validation and are approved for experimental source publication. `PUBLICATION_READY_EXPERIMENTAL` is not `PRODUCTION_RELEASE_READY`; downstream JS/TS or product integration, a production audit, source/constraint freeze, formal trusted setup, signed production artifact provenance, and production rollout remain outstanding.
- Development setup artifacts are not production trust anchors and are never committed. Their recorded checksums identify only this chain-core gate run.
- A client cancellation cannot stop gnark proving. Production process isolation, worker recycling, memory limits, and overload operations remain required.
- Ciphertext decryptability is not proven. Auditor-key compromise, key-epoch rotation, and manual review of failed delivery remain operational risks.
- Public input/output counts, timing, roots, batch grouping, and the minimal summary remain public metadata.
- A remote prover sees the complete witness/payment batch; deployment must treat it as highly sensitive and keep automatic failover disabled.
- Every normal append persists authoritative root/count/height metadata, but not historical internal nodes. Current-root paths use incremental nodes. The public non-current historical query admits at most 1,024 leaves and two concurrent rebuilds per keeper, then returns `ResourceExhausted`; larger online requests require the current root or a trusted local historical index. Offline recovery/export retains the separate `MaxMerkleRebuildLeaves` (1,048,576) bound, and large-tree export requires the complete persisted metadata index.
- Current Deposit and JoinSplit2x2 still retain their existing event compatibility behavior. The production batch path alone uses the minimal-event rule; this is not a claim that every legacy event was redesigned.
- The batch chain core supplies conservative gas coefficients. Per-chain calibration/governance limits, new-asset registration governance, and long-run state-pruning policy remain deferred, but no represented work category is unmetered.


## 13. Authoritative code and fixtures

- Note/domain/tree helpers: `x/privacy/types/note_v1.go`
- Batch statement/vector/disclosure/effect helpers: `x/privacy/types/batch_contract.go`; exact effect encoding/digest: `x/privacy/types/batch_payload.go`
- Fixed payloads: `x/privacy/types/fixed_payload.go`
- Disclosure separation/error contract: `x/privacy/types/disclosure_blinding.go`; 2x2 prepared guard/generator: `x/privacy/client/sdk/transfer/payload.go`; structured signing boundary: `x/privacy/client/sdk/transfer/signing.go`
- Production circuit/matrix: `x/privacy/circuit/batch_joinsplit_16x32.go`, `batch_joinsplit_16x32_test.go`; feasibility resource gate remains in `batch_joinsplit_16x32_feasibility_test.go`
- Production 2x2 relation/regression: `x/privacy/circuit/joinsplit.go`, `x/privacy/circuit/joinsplit_disclosure_blinding_regression_test.go`; artifact cross-identity regression: `x/privacy/circuit/joinsplit_artifact_rotation_test.go`
- Production message/canonical effect: `proto/clairveil/privacy/v1/tx.proto`, `x/privacy/types/batch_payload.go`; wire measurement mirror: `batch_feasibility.proto`
- Keeper/gas/scan/core integration: `x/privacy/keeper/msg_server_batch_transfer.go`, `batch_gas.go`, `batch_scan_index.go`, `batch_transfer_core_integration_test.go`
- Asset registry, common scan, and path snapshot: `x/privacy/keeper/asset_registry.go`, `privacy_scan.go`, `path_snapshot.go`
- Artifact registry/readiness: `x/privacy/zk/registry.go`, `identity.go`, `schema.go`, `resource_model.go`, `development_artifact_gate_test.go`
- Prover admission: `x/privacy/client/sdk/proverservice/admission.go`
- Independent fixtures: `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`, `privacy_batch_joinsplit_v1_contract.json`, and `privacy_disclosure_blinding_v1_contract.json`

If this document and implementation disagree, integration MUST stop. Any change to the frozen protocol requires an explicit decision-change proposal covering soundness, public inputs/goldens, downstream APIs, and resource impact.
