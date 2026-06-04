# Audit Disclosure Key Example

This example shows how a JS/TS wallet or operations tool can derive the Clairveil audit disclosure public key format used by `app_state.privacy.audit_master_pubkey`.

Korean version: [README-kr.md](README-kr.md)

## What It Demonstrates

- Derive a deterministic keypair from an explicit private scalar.
- Derive a deterministic keypair from seed bytes.
- Derive the disclosure keypair from Clairveil privacy root signer material.
- Generate a fresh random keypair.
- Encode the public key as the genesis-compatible base64 value.

The public key is a 32-byte compressed BN254 twisted-Edwards point. The genesis JSON stores those bytes as base64. The private key is a scalar in hex and must remain secret.

## Run

From this directory:

```bash
npm test
npm run demo
```

From the repository root:

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/audit-disclosure-keys run demo
```

The example uses only Node built-ins and has no npm dependencies.

## Usage Notes

Use `keypairFromPrivateKeyHex` or `keypairFromSeedHex` only when you intentionally need reproducible test vectors. The same input always produces the same private key and public key.

Use `randomKeypair` for a new standalone audit disclosure keypair. Store the private scalar securely and put `publicKeyGenesisBase64` into `app_state.privacy.audit_master_pubkey`.

Use `keypairFromPrivacyRootSigner` when matching the CLI-style identity derivation flow. The signer adapter must expose an address, transparent public key bytes, and `signPrivacyRoot(messageBytes)`.
