# Contributing to Clairveil

Clairveil is a reusable Cosmos SDK privacy core, reference daemon, prover service, and wallet-facing conformance fixture repository.

Korean version: [CONTRIBUTING-kr.md](CONTRIBUTING-kr.md)

## Development And Validation

Use the [getting started guide](docs/clairveil-getting-started.md) for toolchain, resources, and local workflows. Keep changes scoped to the reusable privacy core and reference host; downstream production deployment remains downstream-owned.

| Change | Required validation |
| --- | --- |
| Documentation only | `make docs-check` and `git diff --check` |
| General code | `make ci` and `make vulncheck` |
| Privacy flow or CLI workflow | Focused package/CLI tests plus a separately documented native V2 harness when live evidence is required |
| Release candidate | `make release-check`, plus any live/capacity evidence claimed by the release |
| Prover image | Add `make docker-proverd-build` |

`make ci` includes documentation checks. `make release-check` runs CI, vulnerability, static legacy batch conformance, and static/unit/synthetic readiness gates; it starts no node or prover. Use the [testing guide](docs/clairveil-testing-guide.md) to record any separately maintained live or capacity evidence explicitly.

## Change Checklist

Keep commits small and reviewable. Update downstream-facing contracts, fixtures, schemas, paired documentation, and release impact together.

| Surface | Files and required follow-through | Focused validation |
| --- | --- | --- |
| CLI | `x/privacy/client/cli`, `cmd/clairveild`: update CLI tests/reference and getting-started commands; check SDK/schema impact for JSON changes and document any live V2 harness used. | `go test ./x/privacy/client/cli` |
| Proto | `proto/clairveil/privacy/v1`, `proto/clairveil/privacy/v2`: regenerate `x/privacy/types` with `make proto`; update keeper/client/schema/tests and affected integration/SDK guides; record migration impact. | `make proto`, `make ci` |
| Circuit | `x/privacy/circuit`, proof builders/verifiers and artifact config: update circuit docs/tests, artifact filenames/checksums/env and wallet/prover contracts; record `ZK artifacts` impact. | `go test ./x/privacy/circuit ./x/privacy/zk` |
| Fixture/schema | `x/privacy/client/sdk/conformance/testdata`, `docs/schemas`, `examples`: update generation/validation tests, JSON Schema, SDK guide and affected JS consumers. | `make examples`, `go test ./x/privacy/client/sdk/conformance` |
| Payroll/control plane | Update the [payroll reference](examples/reference-payroll/README.md) and SDK guide for store, lease, CAS, retry, reconciliation or wallet changes; preserve one-proof versus legacy boundaries and update fixtures/tests. Gate changes must name static, live one-proof, legacy regression or capacity evidence in testing/operations docs. | `go test ./x/privacy/client/sdk/payroll ./x/privacy/client/sdk/reservation ./x/privacy/client/sdk/conformance` |
| Operations/security | Update the operations guide; update the threat model when trust boundaries change. Change packaging generator/verifier code only when packaging semantics change. | `make ci` |

Before committing, inspect `git status --short` for unintended files, keep maintainer-local paths out of public docs, and ensure private keys, witness payloads and tokens do not appear in logs.

## Documentation

Start with the [documentation index](docs/README.md). `docs/` holds current reusable contracts and guides; example-specific instructions belong with the example, and contributor/release rules belong here. Prefer updating the relevant section over adding a document. A new standalone document needs a distinct reader task and content that cannot be maintained clearly in an existing guide.

- Keep conventional `README.md` names; Korean counterparts use `README-kr.md`. Update English/Korean pairs together and list both versions of every top-level knowledge document in `docs/README*.md`.
- Read and edit docs from the same code ref; do not use `HEAD` guidance with an older tag or binary.
- Keep plans, completion ledgers, dated research, superseded material and local drafts in ignored `tmpdocs/`. Do not add Markdown under runtime `tmp/` or link tracked docs to ignored archives.
- Keep commands executable. Use `<...>` only for unavoidable placeholders and explain where values come from. Local tutorials use `keyring-backend test`.
- When handoff membership changes, update `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt` together. Include current protocol, integration, CLI, testing, SDK/prover, operations/security and contributor docs, plus schemas, fixtures and examples. Run `make docs-check` to validate coverage and links.

## Release Versioning Rules

Until the first stable release, use `v0.MINOR.PATCH`. Use `v0.x.0` for meaningful feature or contract additions and a patch increment for fixes, documentation, CI, packaging, or fixture hardening. `v1.0.0` is reserved for the first release that declares the downstream production integration contract stable. During `v0`, APIs, fixtures, proto, and schemas may change, but every migration impact must be explicit.

Release tags must be annotated exact SemVer with a `v` prefix, such as `v0.4.1` or `v0.5.0-rc.1`. Never move or reuse a published tag. The tag, paired changelog headings, manifest commit, archive, checksum, and public release must identify the same immutable source.

Treat a change as breaking or migration-relevant when it affects any of these surfaces:

- proto messages, services, fields, transaction framing, or typed state/query schemas;
- conformance fixture values/shapes or JSON Schema;
- prover paths, request/response versions, errors, or response binding;
- CLI commands, flags, JSON output, prefixes, denom, or chain defaults;
- circuit inputs, circuit-set order/identity, artifact manifest, VK/schema digest, or checksum policy;
- disclosure, scan cursor/projection, view-tag, gas, atomicity, or genesis semantics.

For each release:

1. Move both changelogs to the same `## vX.Y.Z - YYYY-MM-DD` heading and record compatibility, known risks, and downstream action in the external release notes.
2. Run `make release-check`, plus `make docker-proverd-build` when shipping a prover image.
3. Commit the release metadata, confirm a clean tree, and create one annotated exact-SemVer tag at that commit.
4. Run `make release-pack` and `make release-pack-verify` from the tagged commit. Record the exact commit, archive name, and external SHA-256 only after verification.
5. Push the commit/tag and publish the verified archive only after all identities agree. Corrections require a new patch release, never a moved tag.

An untagged clean commit produces `snapshot-<40-character-commit-sha>` only for packaging CI or internal completeness checks. A snapshot must not be published as a release.

`make release-pack-verify` requires a clean committed tree by default, or an explicit external archive with its out-of-band identity. While editing documentation, use `make docs-check`; only the final annotated exact-SemVer tag can produce a publishable release pack.

## License

By submitting a contribution, you agree that your contribution is licensed under the Apache License, Version 2.0.
