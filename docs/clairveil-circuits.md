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

Common constant:

```text
MerkleDepth = 32
```

Clairveil uses a single depth-32 Merkle tree as a fixed-capacity pool.

## 2. Note Commitment Model

All three circuits compute note commitments with the following meaning:

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

| Input | Meaning |
| --- | --- |
| `MerkleRoot` | Historical Merkle root containing the spent note |
| `Nullifier` | Public nullifier that prevents reuse of the same note |
| `Amount` | Amount to withdraw |
| `Recipient` | Transparent recipient bound as a field element |
| `AssetID` | Asset id derived by hashing the denom |

### Secret Witness

| Witness | Meaning |
| --- | --- |
| `ReceiverSpendPubKey` | Shielded spend public key representing note ownership |
| `ReceiverViewPubKey` | View public key used for note recovery/scanning |
| `Signature` | Evidence that the note owner signed the withdraw message |
| `Randomness` | Note randomness used to build the commitment and nullifier |
| `Path`, `PathHelper` | Merkle path from commitment leaf to root |

### What It Proves

1. The commitment computed from the secret note data is included in `MerkleRoot`.
2. `Signature` is valid for `ReceiverSpendPubKey`.
3. The signature message is bound to `Amount`, `AssetID`, `Randomness`, and `Recipient`.
4. `Nullifier = MiMC(Randomness, spend_pubkey_x, spend_pubkey_y)`.
5. `Amount` fits the 64-bit shielded amount bound.
6. Reusing the same note yields the same nullifier, which lets the keeper reject double spend.

### What It Does Not Prove

- The circuit does not understand the transparent recipient string itself.
- Recipient address decoding, denom string handling, and tx signer checks are keeper/SDK/CLI responsibilities outside the circuit.
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

| Input | Meaning |
| --- | --- |
| `MerkleRoot` | Historical Merkle root containing the input notes |
| `Nullifiers[2]` | Nullifiers for the two input notes |
| `Commitments[2]` | Commitments for the two output notes |
| `UserPrivacyPolicy` | User selective disclosure policy mask |
| `UserDisclosureDigest` | Digest binding user disclosure payload to the output note |
| `AuditDisclosureDigest` | Digest binding mandatory audit disclosure payload to the output note |

### Secret Witness

| Witness | Meaning |
| --- | --- |
| `AssetID` | Transfer asset id |
| `InputAmounts[2]`, `InputRandomness[2]` | Input note amount/randomness |
| `InputPaths[2]`, `InputPathHelpers[2]` | Merkle path for each input note |
| `InputSignatures[2]` | Ownership signature for each input note |
| `InputSpendPubKeys[2]`, `InputViewPubKeys[2]` | Input note owner keys |
| `OutputAmounts[2]`, `OutputRandomness[2]` | Output note amount/randomness |
| `OutputSpendPubKeys[2]`, `OutputViewPubKeys[2]` | Recipient/change note keys |

### What It Proves

1. Both input note commitments are included in the same `MerkleRoot`.
2. Both input signatures are valid.
3. Both nullifiers are computed from the corresponding input note randomness and spend public key.
4. Both input notes belong to the same shielded owner.
5. Both output commitments match the secret output data.
6. `sum(input amounts) = sum(output amounts)`.
7. Each input and output amount fits the 64-bit shielded amount bound.
8. When user disclosure is enabled, the fields selected by policy are bound into `UserDisclosureDigest`.
9. Audit disclosure is always computed with the full disclosure mask and bound into `AuditDisclosureDigest`.

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

### Audit Disclosure

Every transfer must include mandatory audit disclosure. The circuit computes a full audit disclosure digest, and the keeper checks that the message's audit disclosure target pubkey matches the chain-configured audit key.

This means:

- A normal observer cannot directly see amount/from/to.
- A disclosure recipient or master auditor can decrypt the payload with its disclosure key.
- The decrypted payload is connected to the on-chain transfer output through digest verification.

## 6. Artifacts

`clairveil-setup` generates the following artifacts. The active circuit set is `privacy-accounting-v2`.

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
| `privacy_zk_checksums.env` | runtime checksum env |
| `privacy_zk_manifest.json` | JSON artifact manifest |

Generate example:

```bash
go build -o clairveil-setup ./cmd/clairveil-setup
./clairveil-setup --out artifacts/privacy
```

Runtime uses:

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

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
5. Update `docs/clairveil-circuits.md`, `docs/clairveil-js-sdk-handoff.md`, and release note impact.
6. Pass `make test`, `make ci`, `make privacy-e2e-smoke`, and `make release-pack-verify`.

## 9. Important Limits

- The circuit uses a fixed 2-input/2-output transfer model.
- Ciphertext delivery itself is not proven directly by the circuit; it is verified with digest binding and off-chain verification.
- Production deployment still needs artifact signing, reproducible generation, and release provenance.
