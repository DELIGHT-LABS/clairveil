# Clairveil JSON Schema

Korean version: [README-kr.md](README-kr.md)

These machine-readable contracts describe wallet-facing fixtures and the canonical prover HTTP API; they do not replace protocol verification.

| Schema | Purpose |
| --- | --- |
| `clairveil-js-wallet-contract.schema.json` | Wallet conformance fixture shape in `x/privacy/client/sdk/conformance/testdata`. |
| `clairveil-proverd-http-api.schema.json` | Draft 2020-12 shape for prover routes, envelopes, errors, policy, and general/deposit fixtures. |

Validate wallet fixtures:

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

Validate canonical HTTP and deposit fixtures:

```bash
make docs-check
go test ./x/privacy/client/sdk/conformance -count=1
```

`make docs-check` runs the repository-owned Draft 2020-12 validator against positive
fixtures and negative mutations without third-party Python packages. Go conformance
tests bind fixture values to production constants and semantic rules.

Schemas validate shape: fields, types, versions, address/hash formats, fixed arrays,
cursor/version fields, amount/coin strings, and route/envelope/error structure. They
do not prove payload or disclosure-digest recomputation, decryption, Merkle paths,
cursor advancement, reservation behavior, or proofs; SDK and Go tests enforce those.

Route and deposit semantics: [proverd HTTP API](../clairveil-proverd-http-api.md#deposit).
NoteV1, batch, disclosure-blinding, and fixed-payload normative vectors are in `x/privacy/client/sdk/conformance/testdata`; choose their gates in the [testing guide](../clairveil-testing-guide.md).
