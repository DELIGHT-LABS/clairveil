# JS SDK Prover HTTP Client Example

This small JS/TS example calls the current Clairveil audit-field V2 prover boundary with a finite timeout and bearer token. It runs an in-process mock, so no live clairveil-proverd is required.

It is a V2 wire/binding mock: POST /v2/prover/audit-field, envelope v1, circuit set privacy-note-v1-u128-audit-field-v1, base64 byte fields, and ordered PI23 binding. It does not generate a complete gnark witness or verify a Groth16 proof. Before consuming a live response, use the established exact-artifact verifier, such as Go ValidateAuditFieldProofResponse.

The client validates framing before sending, permits HTTP only for loopback, rejects redirects, limits the request with AbortController, and never includes a server response body in an error. It demonstrates both a normal response and rejection of a changed repeated binding field.

## Run

~~~
npm --prefix examples/js-sdk-prover-http-client run demo
~~~

For a real wallet, keep the transport boundary but obtain the witness locally, keep it and bearer credentials out of logs, and invoke the Go/WASM exact-artifact verifier with the local PI23 before constructing a transaction.
