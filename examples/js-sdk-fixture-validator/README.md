# JS SDK Fixture Validator Example

This example is a tiny reference consumer for JS/TS SDK developers. It does not talk to a live node and it does not prove anything. Instead, it reads the Clairveil conformance fixtures and validates the contracts a wallet SDK must match first:

- Clairveil account addresses use `clair1...`.
- Clairveil shielded addresses use `clairs1...`.
- Transfer prepared payload hashes match the Go SDK hash contract.
- Withdraw prover payload hashes match the Go SDK hash contract.
- Final relayed withdraw payload hashes match the Go SDK hash contract.
- Relay withdraw handoff keeps the relayer address as `MsgWithdraw.creator` and the user target as the payload `recipient`.
- Wallet-facing fixtures match `docs/schemas/clairveil-js-wallet-contract.schema.json`.
- Prover HTTP fixtures are language-neutral contracts owned by `docs/schemas/clairveil-proverd-http-api.schema.json`, not by this wallet validator.

## Run

From this directory:

```bash
npm run validate
```

From the repository root:

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

The example uses Node's built-in TypeScript type stripping, so it requires Node 22 or newer and has no npm dependencies.
It includes a small JSON Schema subset validator for Clairveil's local fixture contract. A production JS/TS SDK can use a full JSON Schema validator such as AJV against the same schema file.

## What To Copy Into A Real JS SDK

This example is intentionally narrow. A production JS SDK should not copy the file layout directly, but it can reuse the same implementation checks:

- Implement the same prepared payload hash functions.
- Load the same fixture files in CI.
- Validate fixture shape against `docs/schemas/clairveil-js-wallet-contract.schema.json`.
- Validate the relay withdraw handoff fixture from final payload fields into the relayer-submitted message.
- Verify generated proto/types against `clairveil.privacy.v1`.
- Fail fast if wallet-facing fixtures contain an address prefix other than `clair1...` or `clairs1...`.

Validate the canonical prover HTTP and deposit fixtures with their Go conformance test instead:

```bash
go test ./x/privacy/client/sdk/conformance -count=1
```
