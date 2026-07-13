# Clairveil Maintainer Instructions

This document defines the standards Clairveil maintainers should follow when making changes.

Korean version: [clairveil-maintainer-instructions-kr.md](clairveil-maintainer-instructions-kr.md)

## 1. Basic Principles

- Clairveil is a reusable privacy core and reference host.
- Do not mix downstream production app operations into this repository.
- If a downstream-facing contract changes, update fixtures, schema, docs, and release note impact together.
- If a security or trust boundary changes, update the threat model and security review documents together.

## 2. Checklist By Change Type

### CLI Changes

Likely files:

- `x/privacy/client/cli`
- `cmd/clairveild`

Required work:

1. Update CLI tests.
2. Update `docs/clairveil-cli-reference.md` and `docs/clairveil-cli-reference-kr.md`.
3. If tutorial commands changed, also review `docs/clairveil-local-privacy-walkthrough*.md` and `scripts/privacy-e2e-smoke.sh`.
4. If JSON output fields changed, check JS SDK handoff and schema impact.

Validation:

```bash
go test ./x/privacy/client/cli
make privacy-e2e-smoke
```

### Proto Changes

Likely files:

- `proto/clairveil/privacy/v1`
- generated `x/privacy/types/*.pb.go`

Required work:

1. Modify proto.
2. Run `make proto`.
3. Update keeper/client/schema/tests.
4. Update downstream integration, client handoff, and JS SDK handoff docs when affected.
5. Record breaking or migration impact in release notes.

Validation:

```bash
make proto
make ci
```

### Circuit Changes

Likely files:

- `x/privacy/circuit`
- proof builders/verifiers
- ZK artifact config

Required work:

1. Update circuit docs first.
2. Update circuit tests and proof builder tests.
3. Check artifact filenames/checksum/env impact.
4. Check JS/web wallet fixture and prover contract impact.
5. Fill `ZK artifacts` in release notes.

Validation:

```bash
go test ./x/privacy/circuit ./x/privacy/zk
make privacy-e2e-smoke
```

### Fixture/Schema Changes

Likely files:

- `x/privacy/client/sdk/conformance/testdata`
- `docs/schemas/clairveil-js-wallet-contract.schema.json`
- `examples/*`

Required work:

1. Update fixture generation/validation tests.
2. Update JSON Schema.
3. Check relevant JS examples, including fixture validator and prover HTTP client examples.
4. Update JS SDK handoff docs.

Validation:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
```

### Operations/Security Changes

Examples:

- prover service
- artifact preflight
- Merkle restore/capacity
- audit disclosure policy
- release process

Required work:

1. Update operations guide.
2. If trust boundary changed, update threat model.
3. If production gate changed, update security best-practices review.
4. If release artifact contents changed, update `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt` together. Change generator/verifier code only when packaging semantics change.

Validation:

```bash
make ci
```

After committing, use `make release-pack-verify` to validate the canonical snapshot. For a publishable release, rerun `make release-pack` and `make release-pack-verify` only after creating the annotated exact-SemVer tag at the final commit.

## 3. Documentation Rules

- Root and directory-level `README.md` keep the conventional filename; Korean versions use `README-kr.md`.
- Durable current knowledge stays in `docs/`; implementation intent and gate ledgers stay in `plans/`; duplicate, superseded, or local working documents move to ignored `tmpdocs/`. Do not keep Markdown under runtime `tmp/`.
- New top-level knowledge documents must add `docs/<name>.md` and `docs/<name>-kr.md` in the same change and list both in `docs/README*.md`.
- Historical plans may remain Korean-only when `plans/README*.md` records their status. Every tracked top-level plan must be linked from both plan indexes.
- Read/update documentation at the same code ref. Do not use `HEAD` docs as compatibility guidance for an older tag or binary.
- If a document is needed for downstream handoff, include it in the release pack.
- Command examples should be executable whenever possible.
- Unavoidable placeholders use `<...>` and explain where to get the value.
- Tutorial documents should minimize placeholders and be reproducible with `keyring-backend test`.

## 4. Release Pack Inclusion Rules

Include these documents in the handoff pack:

- downstream integration documents
- client product/UX/risk/API handoff documents
- JS/web wallet implementation contract
- prover operations contract
- circuit/proof/artifact explanation
- security/threat/operations documents
- release/versioning documents
- schema/fixture/examples

When adding a new handoff document, update:

```text
scripts/release-pack-paths.txt
scripts/release-pack-required-files.txt
```

## 5. Recommended Validation Order

Small doc change:

```bash
make docs-check
git diff --check
```

Run `make release-pack-verify` from a clean committed tree (or against an explicit external archive). Its default path rejects a dirty worktree and may produce a canonical commit-bound snapshot; only a pack verified from the final annotated exact-SemVer tagged commit is a release.

General code change:

```bash
make ci
make vulncheck
```

Privacy flow change:

```bash
make privacy-e2e-smoke
```

Release candidate before the release commit/tag:

```bash
make release-check
```

Final tagged release:

```bash
make release-pack
make release-pack-verify
```

Prover image change:

```bash
make docker-proverd-build
```

## 6. Pre-Commit Self Check

1. Run `git status --short` and check for unintended files.
2. Ensure public docs do not contain maintainer-local paths.
3. Ensure CLI/output/schema/proto changes are reflected in docs.
4. Run `make docs-check` and ensure new handoff files are selected and required by the two release manifests.
5. If the change is security-sensitive, ensure private keys, payloads, and tokens are not logged.
