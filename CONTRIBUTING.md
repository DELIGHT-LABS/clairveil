# Contributing to Clairveil

Clairveil is a reusable Cosmos SDK privacy core, reference daemon, prover service, and wallet-facing conformance fixture repository.

Korean version: [CONTRIBUTING-kr.md](CONTRIBUTING-kr.md)

## Development Baseline

Use the toolchain and resource baseline in [docs/clairveil-getting-started.md](docs/clairveil-getting-started.md). Documentation must describe the same checkout being changed.

Before opening a pull request, run:

```bash
make docs-check
make ci
make vulncheck
```

`make ci` already includes `docs-check`; the explicit first command gives faster feedback for documentation-only work.

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
- Release process or handoff membership changes should update the release policy and both release-pack manifests.
- Security-sensitive changes that affect a trust boundary should update the threat model or security review document.

## Documentation

Current durable knowledge lives under `docs/`; implementation intent and completion ledgers live under `plans/`; duplicate, superseded, and local working material belongs in ignored `tmpdocs/`. Start with the [complete documentation index](docs/README.md) and [plan status index](plans/README.md).

When changing behavior that downstream teams depend on, update the English/Korean document pair in the same pull request. Add every new top-level knowledge document to both documentation indexes. Do not add Markdown to runtime `tmp/` or link tracked docs to ignored `tmpdocs/`.

Every release tag must have matching dated headings in `CHANGELOG.md` and `CHANGELOG-kr.md`. If handoff membership changes, update `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt` together.

## License

By submitting a contribution, you agree that your contribution is licensed under the Apache License, Version 2.0.
