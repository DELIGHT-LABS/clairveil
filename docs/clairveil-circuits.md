# Clairveil Circuit Guide

> Current runtime: `x/privacy/circuit/audit_field.go` and four audit-field descriptors use the development-only V2 identity. Each request binds its exact artifact hash and final PI23; see [Proverd HTTP API](clairveil-proverd-http-api.md#current-route). NoteV1/BatchJoinSplit relations below are legacy specifications, not live-route guidance.

This document explains the retained inner NoteV1 relations that the audit-field wrappers build on and what those relations do not prove. The current external contract is the audit-field descriptor set and PI23 framing above. The intended readers are core chain developers, prover operators, JS/TS SDK developers, and security reviewers.

The circuits use `gnark` + Groth16 + BN254. Circuit-internal hashing uses MiMC, and note ownership signatures are verified with the gnark twisted-Edwards EdDSA verifier.

Korean version: [clairveil-circuits-kr.md](clairveil-circuits-kr.md)

## 1. Circuit Files

| File | Circuit | Usage |
| --- | --- | --- |
| `x/privacy/circuit/audit_field.go` | four audit-field wrappers | Current V2 descriptor/PI23 boundary for deposit, spend, 2x2, and 16x32 |
| `x/privacy/circuit/deposit.go` | `DepositCircuit` | Retained inner relation binding a transparent coin amount/asset to a shielded note commitment |
| `x/privacy/circuit/spend.go` | `SpendCircuit` | Used when withdrawing a shielded note to a transparent account |
| `x/privacy/circuit/joinsplit.go` | `JoinSplitCircuit` | Used by shielded transfer to turn 2 input notes into 2 output notes |
| `x/privacy/circuit/batch_joinsplit_16x32.go` | `BatchJoinSplit16x32` | Used by `MsgBatchTransfer` to atomically consume 1..16 notes and create 1..32 notes |

Common constant:

```text
MerkleDepth = 32
```

Clairveil uses a single depth-32 Merkle tree as a fixed-capacity pool.

## 2. Note Commitment Model

All four circuits compute note commitments with the following meaning:

```text
commitment = MiMC(
  domain_field("clairveil.note-commitment.v1"),
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness
)
```

The domain label derivation and exact argument order are normative in the [BatchJoinSplit16x32 contract](clairveil-batch-joinsplit-16x32.md#32-commitment-nullifier-and-tree); this guide mirrors that NoteV1 contract.

The commitment is stored as an on-chain leaf. Amount, asset, randomness, spend public key, and view public key are not directly revealed; they are bound into the commitment.

All shielded amounts are constrained as non-negative 128-bit integers. Keeper, SDK, payload, and circuit checks apply the same `M = 2^128-1` bound to each amount and each operation's input/output totals. Per-asset wallet, payroll, audit, and cumulative deposit/withdraw totals use arbitrary precision; current pool liability and bank balances retain the existing 256-bit bound.

## 3. DepositCircuit

`DepositCircuit` is used for deposit. It proves that the on-chain commitment being appended is for the same amount and asset denom that the keeper locks in the privacy module account.

### Public Input

| Input | Meaning |
| --- | --- |
| `Commitment` | Shielded note commitment appended to the Merkle tree |
| `Amount` | Transparent amount locked by `MsgDeposit` |
| `AssetID` | Asset id derived by hashing the denom |

### Secret Witness

| Witness | Meaning |
| --- | --- |
| `ReceiverSpendPubKey` | Shielded spend public key for the new note |
| `ReceiverViewPubKey` | View public key for note recovery/scanning |
| `Randomness` | Note randomness used to build the commitment |

### What It Proves

1. `Commitment = MiMC(domain_field("clairveil.note-commitment.v1"), spend_pubkey_x, spend_pubkey_y, view_pubkey_x, view_pubkey_y, Amount, AssetID, Randomness)`, exactly as in the canonical NoteV1 formula above.
2. The shielded public keys are valid circuit points.
3. `Amount` fits the 128-bit shielded amount bound.

### What It Does Not Prove

- The circuit does not perform the bank transfer. The keeper verifies the proof before creating its cache context or mutating bank, reserve, tree, event, or index state. After verification succeeds, the cache context performs bank transfer → optional module-balance delta check → reserve record → commitment and indexed-event append → `writeCache()` atomic commit.
- The circuit does not encrypt the note. `encrypted_note` delivery remains an SDK/CLI responsibility.

## 4. SpendCircuit

`SpendCircuit` is used for withdraw. It proves that one shielded note exists and that the note owner authorized a withdraw to a specific transparent recipient.

### Public Input

The `SpendIntentV2` public-input order is consensus-critical:

| Order | Input | Meaning |
| --- | --- | --- |
| 1 | `MerkleRoot` | Historical Merkle root containing the spent note |
| 2 | `ChainDomainHi` | High 128 bits of the chain-domain SHA-256 digest |
| 3 | `ChainDomainLo` | Low 128 bits of the chain-domain SHA-256 digest |
| 4 | `ExpiresAtUnix` | Absolute proof expiry |
| 5 | `Nullifier` | Public nullifier that prevents reuse of the same note |
| 6 | `Amount` | Amount to withdraw |
| 7 | `RecipientDigestHi` | High 128 bits of the raw recipient-byte digest |
| 8 | `RecipientDigestLo` | Low 128 bits of the raw recipient-byte digest |
| 9 | `AssetID` | Asset id derived by hashing the denom |

### Secret Witness

| Witness | Meaning |
| --- | --- |
| `ReceiverSpendPubKey` | Shielded spend public key representing note ownership |
| `ReceiverViewPubKey` | View public key used for note recovery/scanning |
| `Signature` | Evidence that the note owner signed `SpendIntentV2` |
| `Randomness` | Note randomness used to build the commitment and nullifier |
| `Path`, `PathHelper` | Merkle path from commitment leaf to root |

### What It Proves

1. The commitment computed from the secret note data is included in `MerkleRoot`.
2. `Signature` is valid for `ReceiverSpendPubKey` and authenticates the chain domain, root, nullifier, amount, asset, recipient digest, and expiry in `SpendIntentV2`.
3. The recipient digest is `SHA-256("clairveil.withdraw-recipient.v1" || u32be(len(raw_recipient_bytes)) || raw_recipient_bytes)`, split into two non-reduced big-endian 128-bit limbs. Leading-zero byte strings therefore cannot alias another recipient.
4. `Nullifier = MiMC(Randomness, spend_pubkey_x, spend_pubkey_y)`.
5. `Amount` fits the 128-bit shielded amount bound.
6. Reusing the same note yields the same nullifier, which lets the keeper reject double spend.

### What It Does Not Prove

- The circuit does not understand the transparent recipient string itself.
- Recipient address decoding, raw-byte preservation, denom string handling, tx signer checks, and the expiry boundary are keeper/SDK/CLI responsibilities outside the circuit. The keeper rejects at `block_time >= expires_at_unix`.
- `creator` is the fee-paying transaction signer/relayer and is intentionally not part of `SpendIntentV2`; `recipient` is proof-bound and cannot be replaced.
- Withdraw does not create a direct change note. It uses an exact-match note, or an exact-match note created by the planner.
- Withdraw has no output commitment public input. The keeper marks the input nullifier as spent and releases transparent funds, but it does not append a new note leaf.

## 5. JoinSplitCircuit

`JoinSplitCircuit` is used for shielded transfer. It consumes 2 input notes and creates 2 output notes.

Shape:

```text
inputs  = 2
outputs = 2
```

Usually output 0 is the recipient note and output 1 is the sender change note. A zero-value dummy note can be used to fill an input slot when needed.

### Public Input

The `TransferIntentV2` public-input order is consensus-critical:

| Order | Input | Meaning |
| --- | --- | --- |
| 1 | `MerkleRoot` | Historical Merkle root containing the input notes |
| 2 | `ChainDomainHi` | High 128 bits of the chain-domain SHA-256 digest |
| 3 | `ChainDomainLo` | Low 128 bits of the chain-domain SHA-256 digest |
| 4 | `ExpiresAtUnix` | Absolute proof expiry |
| 5 | `Nullifier0` | First ordered input nullifier |
| 6 | `Nullifier1` | Second ordered input nullifier |
| 7 | `Commitment0` | First ordered output commitment |
| 8 | `Commitment1` | Second ordered output commitment |
| 9 | `UserPrivacyPolicy` | User selective-disclosure policy mask |
| 10 | `UserDisclosureDigest` | Independently blinded selective-disclosure digest |
| 11 | `FullDisclosureDigest` | Independently blinded full digest shared by audit and self-view verification |
| 12 | `PayloadDigestHi` | High 128 bits of the canonical transfer-effect SHA-256 digest |
| 13 | `PayloadDigestLo` | Low 128 bits of the canonical transfer-effect SHA-256 digest |

### Secret Witness

| Witness | Meaning |
| --- | --- |
| `AssetID` | Transfer asset id |
| `InputAmounts[2]`, `InputRandomness[2]` | Input note amount/randomness |
| `InputPaths[2]`, `InputPathHelpers[2]` | Merkle path for each input note |
| `OwnerSignature` | One signature over the final `TransferIntentV2` |
| `InputSpendPubKeys[2]`, `InputViewPubKeys[2]` | Input note owner keys |
| `OutputAmounts[2]`, `OutputRandomness[2]` | Output note amount/randomness |
| `OutputSpendPubKeys[2]`, `OutputViewPubKeys[2]` | Recipient/change note keys |
| `UserDisclosureBlinding` | Independent non-zero blinding for enabled user disclosure |
| `FullDisclosureBlinding` | Independent non-zero blinding for audit/self-view full disclosure |

### What It Proves

1. Both input note commitments are included in the same `MerkleRoot`.
2. Both inputs have the same spend and view owner keys, and one `OwnerSignature` is valid for that owner over the final `TransferIntentV2`.
3. Both nullifiers are computed from the corresponding input note randomness and spend public key.
4. The two nullifiers are distinct, and both output commitments are distinct.
5. Both output commitments match the secret output data.
6. `sum(input amounts) = sum(output amounts)`.
7. Each input and output amount fits the 128-bit shielded amount bound.
8. When user disclosure is enabled, the fields selected by policy and a non-zero blinding are bound into `UserDisclosureDigest`.
9. Audit/self-view full disclosure uses a non-zero blinding and is bound into `FullDisclosureDigest`.
10. Ordered nullifiers, commitments, ciphertexts, view tags, all disclosure envelopes, and expiry are finalized before signing and are bound through the canonical payload digest. `creator`, proof bytes, fee, gas, memo, sequence, and tx signature are excluded so a relayer may replace only `creator`.

The `DISCLOSURE-BLINDING-SEPARATION` invariant requires the following for recipient output `0`: enabled user blinding differs from `OutputRandomness[0]`; full blinding differs from `OutputRandomness[0]`; and full blinding differs from user blinding. Policy `all-private` canonicalizes user blinding to zero and gates off only the first relation. JoinSplit2x2 has no disabled output slot, and output `1` is an active change note without a disclosure witness. The production `JoinSplitCircuit`, shared native/prepared validator, and structured pre-sign boundary now enforce this exact contract at `99,775` constraints. Implementation of `DISCLOSURE-BLINDING-SEPARATION` is complete, and the security, protocol, chain-core, client-integration, and independent-publication-validation gates are closed; the resulting source is `PUBLICATION_READY_EXPERIMENTAL` but not production-release ready.

Transfer view tags are not separate `JoinSplitCircuit` public inputs. They are ordered public scan hints carried by `MsgTransfer` and events and are included in the canonical payload digest, but must still not be treated as note-ownership signals.

The chain domain is `SHA-256("clairveil.chain-domain.v1" || u32be(len(chain_id)) || chain_id || u32be(len(circuit_set_id)) || circuit_set_id)`. SHA-256 digests are split into two big-endian 128-bit limbs without field-modulus reduction. The SDK derives the domain from its configured chain, while the keeper recomputes it from the current chain context. The keeper rejects transfer and withdraw at `block_time >= expires_at_unix`.

### User Disclosure Policy

`UserPrivacyPolicy` is interpreted as 3 bits.

| Policy | Revealed scope |
| --- | --- |
| `all-private` | no user disclosure |
| `amount` | amount, asset |
| `to` | recipient shielded address keys |
| `amount-to` | amount, asset, recipient |
| `from` | sender shielded address keys |
| `amount-from` | amount, asset, sender |
| `from-to` | sender, recipient |
| `amount-from-to` | amount, asset, sender, recipient |

The circuit does not encrypt disclosure plaintext. What it guarantees is that the selected disclosure fields are bound to the digest. Actual encryption, public/recipient/audit delivery, and decode UX are handled by SDK/CLI and event payloads.

Sender self-view disclosure is separate encrypted metadata. Its payload is included in the signed canonical transfer effect and uses the same blinded `FullDisclosureDigest` as audit disclosure. Wallets must decrypt it, recover the blinding from the versioned plaintext, recompute the full digest, and compare it with the on-chain digest.

### Audit Disclosure

Every transfer must include mandatory audit disclosure. The circuit computes the independently blinded full disclosure digest, and the keeper checks that the message's audit disclosure target pubkey matches the chain-configured audit key.

This means:

- A normal observer cannot directly see amount/from/to.
- A disclosure recipient or master auditor can decrypt the payload with its disclosure key.
- The decrypted payload is connected to the on-chain transfer output through digest verification.

## 6. Artifacts And Maintenance

`clairveil-setup` produces R1CS, proving-key and verifying-key files for each of `deposit`, `spend`, `joinsplit` and `batch-joinsplit-16x32-v1`, plus `privacy_zk_manifest.json` and `privacy_zk_checksums.env`. The manifest's ordered identity must match consensus; checksum environment variables cannot override it. Validators load required VKs, while provers lazily load selected R1CS/PK pairs. Generated binaries and secrets are not committed.

Use the [operations guide](clairveil-operations-guide.md) for generation, strict preflight, selective development rotation and deployment. The [protocol contract](clairveil-batch-joinsplit-16x32.md) records artifact identity, compatibility and development hashes. These are development artifacts, not a formal trusted setup or signed production release.

Circuit changes must also update proof builders/verifiers, affected proto/CLI/schema, conformance fixtures and release impact. Follow the [contributor checklist](../CONTRIBUTING.md) and [testing guide](clairveil-testing-guide.md), including shared native/prepared/structured-signer invariant tests.

## 7. Important Limits

- The native `JoinSplitCircuit` remains fixed at 2 inputs and 2 outputs; `BatchJoinSplit16x32` is a separate 1..16 input / 1..32 output circuit and artifact.
- Ciphertext delivery itself is not proven directly by the circuit; it is verified with digest binding and off-chain verification.
- Production deployment still needs artifact signing, reproducible generation, and release provenance.
- Proof verification is precharged by the keeper after cheap canonical Groth16 framing succeeds and before decoding, VK loading, or pairing work. Deposit, spend, and joinsplit each currently charge `1,000,000` gas per verification attempt; invalid cryptographic proofs still consume the full precharge, while malformed framing does not.

## 8. BatchJoinSplit16x32

The batch circuit proves 1..16 inputs and 1..32 outputs using exact active prefixes and zero disabled sentinels, independent depth-32 membership paths, canonical subgroup keys, active input/output distinctness, 128-bit value conservation, per-output user/full disclosure digests and one owner signature. It shares the NoteV1 relation with Deposit, Spend and JoinSplit2x2.

`DISCLOSURE-BLINDING-SEPARATION` enforces per-output user-vs-note, full-vs-note and full-vs-user inequalities with exact all-private/disabled gating. Production 2x2 enforces the output-0 relation in the circuit and shared native/prepared/structured signing validation. SDK-wide secret freshness is a separate stronger policy.

The [protocol contract](clairveil-batch-joinsplit-16x32.md) is the single reference for the 12 public inputs and their order, vector domains, canonical owner-effect bytes, fixed payloads, gas coefficients, typed scan state and atomic keeper order. Changes require updated circuit identity, golden vectors and compatibility review. The [threat model](clairveil-threat-model.md) covers residual disclosure and operational risks.
