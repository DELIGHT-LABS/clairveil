# JS SDK Prover HTTP Client Example

This example shows how a JS/TS wallet SDK can call the Clairveil companion prover contract with an explicit timeout and bearer token. It starts an in-process mock prover from the repository fixtures, so it can run without a live `clairveil-proverd`.

The goal is not to implement a full wallet SDK. The goal is to make the safe client shape obvious:

- use `fetch` with `AbortController` timeout;
- send `Authorization: Bearer ...` when the prover requires auth;
- validate request/response versions;
- validate proof `payload_hash` against the prepared payload;
- avoid logging the bearer token or full prepared payload.

## Run

From this directory:

```bash
npm run demo
```

From the repository root:

```bash
npm --prefix examples/js-sdk-prover-http-client run demo
```

The example uses Node's built-in TypeScript type stripping, so it requires Node 22 or newer and has no npm dependencies.

## What To Copy Into A Real JS SDK

A production JS SDK should adapt the client boundary, not the mock server:

- keep one `ProverClient` interface behind local, remote, and browser/WASM prover adapters;
- require a finite timeout for remote proof requests;
- validate response version and proof payload hash before building transactions;
- treat remote prover payloads as privacy-sensitive data;
- keep remote prover tokens out of logs, source code, browser bundle constants, and public telemetry.
