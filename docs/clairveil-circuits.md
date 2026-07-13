# Clairveil Circuit Guide

This document explains what Clairveil's current ZK circuits prove and what they do not prove. The intended readers are core chain developers, prover operators, JS/TS SDK developers, and security reviewers.

The circuits use `gnark` + Groth16 + BN254. Circuit-internal hashing uses MiMC, and note ownership signatures are verified with the gnark twisted-Edwards EdDSA verifier.

Korean version: [clairveil-circuits-kr.md](clairveil-circuits-kr.md)

## 1. Circuit Files

| File | Circuit | Usage |
| --- | --- | --- |
| `x/privacy/circuit/deposit.go` | `DepositCircuit` | Used by deposit to bind a transparent coin amount/asset to the shielded note commitment |
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
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness
)
```

The commitment is stored as an on-chain leaf. Amount, asset, randomness, spend public key, and view public key are not directly revealed; they are bound into the commitment.

All shielded amounts are constrained as non-negative 64-bit integers. Keeper, SDK, payload, and circuit checks use the same bound.

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

1. `Commitment = MiMC(spend_pubkey, view_pubkey, Amount, AssetID, Randomness)`.
2. The shielded public keys are valid circuit points.
3. `Amount` fits the 64-bit shielded amount bound.

### What It Does Not Prove

- The circuit does not perform the bank transfer. The keeper locks transparent funds first, verifies the proof, records reserve accounting, and appends the commitment inside one transaction.
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
5. `Amount` fits the 64-bit shielded amount bound.
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
7. Each input and output amount fits the 64-bit shielded amount bound.
8. When user disclosure is enabled, the fields selected by policy and a non-zero blinding are bound into `UserDisclosureDigest`.
9. Audit/self-view full disclosure uses a non-zero blinding and is bound into `FullDisclosureDigest`.
10. Ordered nullifiers, commitments, ciphertexts, view tags, all disclosure envelopes, and expiry are finalized before signing and are bound through the canonical payload digest. `creator`, proof bytes, fee, gas, memo, sequence, and tx signature are excluded so a relayer may replace only `creator`.

`S4-B02` freezes the additional `DISCLOSURE-BLINDING-SEPARATION` invariant for recipient output `0`: enabled user blinding differs from `OutputRandomness[0]`; full blinding differs from `OutputRandomness[0]`; and full blinding differs from user blinding. Policy `all-private` canonicalizes user blinding to zero and gates off only the first relation. JoinSplit2x2 has no disabled output slot, and output `1` is an active change note without a disclosure witness. The production `JoinSplitCircuit`, shared native/prepared validator, and structured pre-sign boundary now enforce this exact contract at `99,775` constraints. `S4-B02` and fresh Gates 1/2/3A are closed, and Session 3B re-entry is unblocked; Gate 3B, Session 4, and publication remain blocked.

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

## 6. Artifacts

`clairveil-setup` generates the following development artifacts. The active circuit set is `privacy-note-v1`.

| File | Meaning |
| --- | --- |
| `privacy_deposit_r1cs.bin` | DepositCircuit constraint system |
| `privacy_deposit_pk.bin` | DepositCircuit proving key |
| `privacy_deposit_vk.bin` | DepositCircuit verifying key |
| `privacy_spend_r1cs.bin` | SpendCircuit constraint system |
| `privacy_spend_pk.bin` | SpendCircuit proving key |
| `privacy_spend_vk.bin` | SpendCircuit verifying key |
| `privacy_joinsplit_r1cs.bin` | JoinSplitCircuit constraint system |
| `privacy_joinsplit_pk.bin` | JoinSplitCircuit proving key |
| `privacy_joinsplit_vk.bin` | JoinSplitCircuit verifying key |
| `privacy_batch_joinsplit_16x32_r1cs.bin` | BatchJoinSplit16x32 constraint system |
| `privacy_batch_joinsplit_16x32_pk.bin` | BatchJoinSplit16x32 proving key |
| `privacy_batch_joinsplit_16x32_vk.bin` | BatchJoinSplit16x32 verifying key |
| `privacy_zk_checksums.env` | runtime checksum env |
| `privacy_zk_manifest.json` | JSON artifact manifest |

Generate example:

```bash
go build -o clairveil-setup ./cmd/clairveil-setup
./clairveil-setup --out artifacts/privacy
```

To rotate only JoinSplit in an already complete development artifact set:

```bash
./clairveil-setup --out artifacts/privacy --circuit joinsplit --overwrite
```

Runtime uses:

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

`privacy_zk_manifest.json` schema `v2` records the exact ordered circuit descriptors, VK SHA-256 values, and public-input schema SHA-256 values. Genesis and consensus state pin the corresponding `CircuitSetIdentity` schema `v1`; local checksum environment variables cannot override that identity. A node compares its local verifier identity with consensus before serving: validators need only the four required VK files and load them lazily, while a prover loads R1CS/PK only when proving. A mismatch blocks startup/readiness. Generated R1CS/PK/VK binaries and secrets are not committed.

The Session 3A development artifact gate recorded the following reproducible local identities for the batch circuit. These values identify the measured development artifacts; they are not a formal setup or a production artifact release.

| Item | Recorded value |
| --- | --- |
| Constraints | `1,111,837` |
| R1CS | `122,813,535 B`, SHA-256 `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210` |
| Proving key | `209,218,621 B`, SHA-256 `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470` |
| Verifying key | `716 B`, SHA-256 `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621` |
| Public-input schema | SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333` |
| Generation peak RSS | `3,308,797,952 B` |
| Validator/prover readiness peak RSS | `1,295,482,880 B` |

The setup generated here is development-only. The repository does not perform or claim a formal trusted setup, artifact signing ceremony, production artifact release, or external audit.

The `S4-B02` Session 3A implementation keeps circuit set `privacy-note-v1`, the JoinSplit 13-input order/schema hash `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, payload `v5`, and proof/HTTP `v2` unchanged. It rotates only `privacy_joinsplit_{r1cs,pk,vk}.bin`; their development SHA-256 values are respectively `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, and `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`. Discard old JoinSplit proof jobs and use fresh genesis/reset. Batch artifacts remain unchanged.

## 7. Reserve Accounting Query

Circuit soundness is paired with keeper-level reserve accounting. The keeper records denom-level `total_deposited` and `total_withdrawn`, then compares the expected reserve (`total_deposited - total_withdrawn`) to the actual privacy module-account balance.

```text
GET /clairveil/privacy/v1/reserve/{denom}
```

Clients and operators should treat `invariant_holds=false` as an incident signal, especially after direct bank sends, manual top-ups, or migration work.

## 8. What To Do When Changing Circuits

When changing circuits, update these in one commit or a short commit series:

1. Update `x/privacy/circuit` tests.
2. Check whether prover payload builders and verifier input shape changed.
3. Update proto, CLI JSON, fixture schema if affected.
4. Regenerate and validate JS/web wallet conformance fixtures.
5. Run the shared native/prepared/structured-signer invariant vectors and the 2x2 old-circuit-control versus hardened-circuit feasibility test.
6. Update `docs/clairveil-circuits.md`, `docs/clairveil-js-sdk-handoff.md`, and release note impact.
7. Pass `make test`, `make ci`, `make privacy-e2e-smoke`, and `make release-pack-verify`.

## 9. Important Limits

- The native `JoinSplitCircuit` remains fixed at 2 inputs and 2 outputs; `BatchJoinSplit16x32` is a separate 1..16 input / 1..32 output circuit and artifact.
- Ciphertext delivery itself is not proven directly by the circuit; it is verified with digest binding and off-chain verification.
- Production deployment still needs artifact signing, reproducible generation, and release provenance.
- Proof verification is precharged by the keeper after cheap canonical Groth16 framing succeeds and before decoding, VK loading, or pairing work. Deposit, spend, and joinsplit each currently charge `1,000,000` gas per verification attempt; invalid cryptographic proofs still consume the full precharge, while malformed framing does not.

## 10. NoteV1 And Session 3A Batch Core

The active circuit set is `privacy-note-v1`. Its required descriptor order is `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`. All four circuits, the keeper tree, and typed scan state share one domain-separated NoteV1 commitment/nullifier/tree contract, canonical field/key checks, and exact depth-specific empty roots. Denoms never enter a circuit as strings: `AssetRegistryV1` is the authoritative one-to-one mapping to a 32-byte `asset_id`. This is a breaking state and artifact transition that requires fresh genesis, regenerated artifacts, deleted proof/note/scan caches, and a full rescan.

Canonical plaintext and encrypted payloads use `privacy-fixed-v1`: fixed 350-byte note plaintext, fixed 392-byte disclosure plaintext, and a 20-byte typed envelope header before the exact encryption payload. Raw ciphertext and cross-kind or trailing-byte decode are invalid. `DISCLOSURE-BLINDING-SEPARATION` requires, per disclosure output, user-vs-note, full-vs-note, and full-vs-user inequality with exact all-private/disabled gating. Batch enforces it per active output slot. Production 2x2 now enforces the output-0 relation in the circuit, shared native/prepared validator, and structured pre-sign boundary, and its JoinSplit development identity has been rotated. `S4-B02` and fresh Gates 1/2/3A are closed; Session 3B re-entry is unblocked while Gate 3B remains failed. This prevents low-entropy disclosed values from becoming a practical dictionary oracle without overstating cross-output global freshness.

`BatchJoinSplit16x32` is now the fourth production circuit. It preserves the Session 2 contract: capacities 16/32, exact active prefixes, zero disabled sentinels, 16 independent depth-32 paths, subgroup/key constraints, active-only distinctness, value conservation, per-output NoteV1/user/full-disclosure checks, and one owner signature. Its consensus public-input order is:

1. `MerkleRoot`
2. `ChainDomainHi`
3. `ChainDomainLo`
4. `ExpiresAtUnix`
5. `InputCount`
6. `OutputCount`
7. `NullifierRoot`
8. `CommitmentRoot`
9. `UserDisclosureRoot`
10. `FullDisclosureRoot`
11. `PayloadDigestHi`
12. `PayloadDigestLo`

For each vector kind, the fixed-capacity leaf, node, and final root are domain-separated; leaves bind `(index, enabled, value)`, nodes bind `(level, left, right)`, and the final root binds `(capacity, count, tree_root)`. The formulas and order are live consensus contracts, with schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`.

`MsgBatchTransfer` and structured `BatchTransferOutput` are registered production proto/types. Owner-effect format `1` uses `u32be` counts and `u32be(length) || bytes` framing over root, ordered nullifiers, every ordered output effect field, audit ID/epoch/target, and expiry. SHA-256 domain `clairveil.batch-transfer-payload.v1` produces public inputs 11–12 as non-reduced big-endian 128-bit limbs. `creator` and `proof` alone are excluded. The keeper derives all 12 public values, performs canonical and global nullifier/commitment checks, precharges deterministic gas after cheap framing and before semantic/cryptographic work, verifies the proof, then commits nullifiers, commitments, root snapshot, typed scan records, and the minimal ABCI/event-index summary through one cache-context write.

`BatchGasModelV1` charges `1,000,000` verification base gas, `25,000` per input, `50,000` per output, `4` per canonical payload byte, `8` per typed-state byte, `5,000` per tree-node write, and `10,000` per global lookup. Bounds are `65,384` canonical payload bytes, `256 KiB` typed scan state, `output_count * 33` tree writes, and `input_count + output_count` global lookups. Out-of-gas stops before semantic or state work.

Deposit, native 2x2 JoinSplit, and batch transfer share `privacy-sequence-v1` ordering and `privacy-scan-v2` typed state. Ciphertext and disclosure bytes are stored once in `PrivacyScanOutputV2`; the batch event carries only the effect ID, counts, roots, versions, expiry, relayer, and audit identity. `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, and `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache` exercise the direct core success path, atomic failure, and 2x2+batch/batch+batch rollback behavior.

Session 3A did not include the public batch-transfer Go SDK, the `clairveil-proverd` batch route, wallet scanner/decrypt UX, one-proof payroll planner/worker/reconcile integration, or the batch CLI/tutorial. Those reference Go surfaces were subsequently implemented in Session 3B. The latest independent review reopens Gate 3B for the recorded live payroll/disclosure, SQL, and batch signer gaps, so Session 3B re-entry is unblocked but not complete and publication remains blocked. Formal trusted setup and production artifact distribution have not been performed.

The artifact registry is role-aware: a validator verifies exact consensus identity and loads required VKs only, while a prover lazily loads selected R1CS/PK pairs. The reference prover bounds each current circuit to one in-flight and four queued jobs and uses a positive 8 MiB body limit. Canceling a request cannot terminate an already running in-process gnark solver; hard cancellation and memory isolation require a worker-process boundary. Automatic prover failover remains disabled.
