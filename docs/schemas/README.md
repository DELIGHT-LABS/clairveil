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
- prepared transfer prover payload `v3` shape, `view_tag_hexes`, and sender self-view disclosure fields
- prepared withdraw prover payload shape
- final prepared withdraw payload shape
- relay withdraw handoff request and relayer `MsgWithdraw` mapping shape
- prover HTTP route, request, response, and error contract shape
- note reservation status, transition, active uniqueness, lease precondition, lookup-key vector, and operation success evidence contract shape
- `scan_events` request/response fixture shape, including cursor fields, projection outputs, `scan_format_version`, and `view_tag_version`
- batch `check_nullifiers` request/response fixture shape
- send-capable reference flow fixture shape

This schema checks field presence, basic types, version constants, address prefixes, fixed-size hashes, 2-byte view tag hex strings, current transfer payload array sizes, scan cursor/version fields, note reservation enum/transition arrays, HMAC lookup-key vectors, Merkle path helper bits, canonical non-negative uint64 amount strings, and Cosmos SDK coin strings.

It does not replace semantic verification. Payload hash recomputation, disclosure digest verification, sender self-view payload decryption/verification, Merkle path recomputation, scan cursor advancement behavior, safe view-tag mismatch fallback, and proof verification must be implemented separately by SDK/tests.
