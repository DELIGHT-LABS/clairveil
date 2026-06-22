# Clairveil Examples

This directory contains small reference consumers for teams integrating Clairveil from non-Go stacks.

Korean version: [README-kr.md](README-kr.md)

## Available Examples

- `audit-disclosure-keys`: a dependency-free Node example that derives audit disclosure keypairs and prints the genesis-compatible public key encoding.
- `js-sdk-fixture-validator`: a dependency-free Node/TypeScript example that reads Clairveil conformance fixtures and validates address prefixes, payload hashes, and prover-contract expectations.
- `js-sdk-prover-http-client`: a dependency-free Node/TypeScript example that calls the prover HTTP contract through a timeout-bound bearer-auth client against a fixture-backed mock prover.
- `clairveil-dapp`: a browser DApp that connects MetaMask/Keplr and uses a local `clairveild` dev relay to test localnet queries, faucet funding, Keplr direct-sign bank sends, Keplr direct-sign privacy deposits, and note scans.

These examples are not production SDKs. They are reference points showing JS/TS SDK and web wallet teams which Clairveil contracts should be validated first.
