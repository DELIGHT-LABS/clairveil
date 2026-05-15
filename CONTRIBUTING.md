# Contributing to Clairveil

Clairveil is a reusable Cosmos SDK privacy core, reference daemon, prover service, and wallet-facing conformance fixture repository.

Korean version: [CONTRIBUTING-kr.md](CONTRIBUTING-kr.md)

## Development Baseline

Before opening a pull request, run:

```bash
make ci
make vulncheck
```

For release candidates or release-critical changes, run the heavier local chain gate:

```bash
make release-check
```

`make release-check` starts local nodes and runs the full privacy smoke tests, so it is intentionally slower than the default CI path.

## Commit Scope

Keep commits small and reviewable.

- Module/runtime changes should include tests.
- CLI or workflow changes should update docs.
- Wallet-facing fixture changes should update JSON Schema and examples.
- Release process changes should update the release handoff pack.
- Security-sensitive changes that affect a trust boundary should update the threat model or security review document.

## Documentation

Important integration documents live under `docs/`:

- `docs/clairveil-downstream-cosmos-integration-guide.md`
- `docs/clairveil-js-sdk-handoff.md`
- `docs/clairveil-circuits.md`
- `docs/clairveil-cli-reference.md`
- `docs/clairveil-testing-guide.md`
- `docs/clairveil-operations-guide.md`
- `docs/clairveil-maintainer-instructions.md`
- `docs/clairveil-release-handoff-pack.md`
- `docs/clairveil-proverd-remote-production-profile.md`
- `docs/clairveil-threat-model.md`
- `docs/clairveil-security-best-practices-review.md`

When changing behavior that downstream teams depend on, update the relevant document in the same pull request.

## License

By submitting a contribution, you agree that your contribution is licensed under the Apache License, Version 2.0.
