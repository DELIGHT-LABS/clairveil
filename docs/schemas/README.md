# Clairveil JSON Schema

This directory contains machine-readable contracts for Clairveil JS/TS SDK and web wallet integration.

Korean version: [README-kr.md](README-kr.md)

## Schema

- `clairveil-js-wallet-contract.schema.json`: JSON Schema for wallet-facing conformance fixtures under `x/privacy/client/sdk/conformance/testdata`.

## Usage

External SDKs should validate fixtures in CI before starting live network integration.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

The repository validator uses a dependency-free subset validator to keep the sample easy to run. Production JS/TS SDKs can validate the same schema with a full JSON Schema validator such as AJV.

## What The Schema Covers

- browser signer/root seed derivation fixture shape
- wallet readonly address, view key, disclosure, and scan fixtures
- prepared transfer prover payload shape
- prepared withdraw prover payload shape
- prover HTTP route, request, response, and error contract shape
- send-capable reference flow fixture shape

This schema checks field presence, basic types, version constants, address prefixes, fixed-size hashes, and current transfer payload array sizes.

It does not replace semantic verification. Payload hash recomputation, disclosure digest verification, Merkle path recomputation, and proof verification must be implemented separately by SDK/tests.
