# Clairveil JSON Schema

This directory contains machine-readable contracts for Clairveil wallet-facing integration and language-neutral prover HTTP APIs.

Korean version: [README-kr.md](README-kr.md)

## Schema

- `clairveil-js-wallet-contract.schema.json`: JSON Schema for wallet-facing conformance fixtures under `x/privacy/client/sdk/conformance/testdata`.
- `clairveil-proverd-http-api.schema.json`: Draft 2020-12 schema for the canonical prover HTTP route inventory, envelopes, errors, common HTTP policy, and the general/deposit conformance fixtures.

## Usage

Validate wallet-facing fixtures with the reference validator:

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

The repository validator uses a dependency-free subset validator to keep the sample easy to run. Production JS/TS SDKs can validate the same schema with a full JSON Schema validator such as AJV.

Validate the canonical prover HTTP and deposit fixtures with the repository-owned schema and Go conformance test:

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -count=1
```

`make docs-check` applies the Draft 2020-12 schema to both fixtures; the Go test binds their values to production constants and semantic validation. The prover schema has separate HTTP route, envelope, payload/proof, and error version layers. It is independent of wallet package shape; its root `oneOf` validates `privacy_prover_http_api_contract.json` and `privacy_deposit_prover_contract.json`.

## What The Schema Covers

- browser signer/root seed derivation fixture shape
- wallet readonly address, view key, disclosure, and scan fixtures
- prepared transfer prover payload `v5` shape, final owner intent, disclosure blindings, `view_tag_hexes`, and sender self-view disclosure fields
- prepared withdraw prover payload shape
- final prepared withdraw payload shape
- relay withdraw handoff request and relayer `MsgWithdraw` mapping shape
- note reservation status, transition, active uniqueness, lease precondition, lookup-key vector, and operation success evidence contract shape
- `scan_events` request/response fixture shape, including cursor fields, projection outputs, `scan_format_version`, and `view_tag_version`
- batch `check_nullifiers` request/response fixture shape
- send-capable reference flow fixture shape
- active circuit identity `privacy-note-v1`, authoritative `AssetRegistryV1` query shapes, and `privacy-scan-v2` records with global cursor `(height, global_sequence, output_index)`
- canonical `privacy-fixed-v1` note/disclosure plaintext hex and typed encrypted-envelope fields

This schema checks field presence, basic types, version constants, address prefixes, fixed-size hashes, 2-byte view tag hex strings, current transfer payload array sizes, scan cursor/version fields, note reservation enum/transition arrays, HMAC lookup-key vectors, Merkle path helper bits, canonical non-negative uint64 amount strings, and Cosmos SDK coin strings.

It does not replace semantic verification. Payload hash recomputation, disclosure digest verification, `DISCLOSURE-BLINDING-SEPARATION`, sender self-view payload decryption/verification, Merkle path recomputation, scan cursor advancement behavior, safe view-tag mismatch fallback, and proof verification must be implemented separately by SDK/tests.

## NoteV1 And BatchJoinSplit16x32 Independent Contracts

Three language-neutral fixtures supplement the wallet-shape schema:

- `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json` freezes the NoteV1 domains, domain constants, asset-ID/commitment/nullifier vectors, exact empty roots, and `privacy-fixed-v1` sizes.
- `x/privacy/client/sdk/conformance/testdata/privacy_batch_joinsplit_v1_contract.json` freezes the production 16/32 capacities, canonical 1..64-byte `audit_key_id` grammar `[a-z0-9][a-z0-9._-]*`, vector roots, effect ID, exact canonical owner-effect digest, corrected max-shape wire-state values, and the 12 public inputs in this exact order: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. The independent payload SHA-256 is `f2588c...24b0`; the max canonical payload is `65,384` bytes. The wire goldens are Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, and query response `74,551` bytes.
- `x/privacy/client/sdk/conformance/testdata/privacy_disclosure_blinding_v1_contract.json` freezes `DBS-01..03`, active all-private and disabled sentinels, the distinction between per-slot circuit separation and broader SDK global freshness, required circuit/native/prepared/structured-signer enforcement layers, and stable secret-free `DBS_*` error codes. Every vector includes `error_field`: it is empty for valid vectors and otherwise names the canonical offending request field. `active_all_private_zero_randomness_valid` explicitly freezes that active output randomness may be zero while the all-private user blinding remains the canonical zero sentinel and full-blinding separation stays enabled.

The fixed binary contract is exact: note plaintext is 350 bytes, disclosure plaintext is 392 bytes, and the typed encrypted-envelope header is 20 bytes. The JSON Schema primarily validates fixture shape; SDK semantic tests must additionally enforce those byte lengths, envelope kind/domain/version/reserved bytes, no trailing bytes, `AssetRegistryV1` resolution, full scan-cursor advancement, and a Merkle path snapshot matching the selected root. Typed `privacy-scan-v2` records fail closed on a wrong exact event type, fixed envelope, digest, key, zero/disabled sentinel, or orphan/non-adjacent output.

Current-root path queries use incremental nodes and do not consume the online historical-rebuild budget. Every non-current historical path requires persisted `(root, leaf_count, height)` metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Above the online bound, use the current root or a trusted local historical-path index. Offline recovery/export keeps the separate `MaxMerkleRebuildLeaves` (1,048,576) bound. A complete persisted per-prefix snapshot metadata index still permits genesis export above the offline bound without rebuilding all historical nodes.

`BatchJoinSplit16x32`, `MsgBatchTransfer`, its keeper handler, typed scan state, and artifact descriptors are production core contracts. `batch_feasibility.proto` remains measurement-only. The batch chain core did not add the public SDK or remote prover route; the batch integration subsequently added the reference Go SDK/CLI and `POST /v1/proofs/batch-transfer`, while external JS/web product delivery remains downstream work. The corrected full-shape reference gate measured `1,111,837` constraints, peak RSS `3,339,862,016` bytes, `55.892 ms/output` max-shape warm proving, and `2.789x` per-output improvement over native 2x2. Artifact consumers must pin `privacy-note-v1`; validators use exact consensus identity and required VKs, while provers lazily load selected R1CS/PK pairs. Reference prover admission defaults are one in-flight, four queued, and a positive 8 MiB request limit per circuit/service boundary.

Prepared transfer payload `v5` remains the current outer prepared-payload version. It is distinct from the inner note/disclosure/envelope encoding `privacy-fixed-v1`; neither version replaces the other. Compatibility fallback is prohibited.
