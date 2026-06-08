# Changelog

All notable changes to Clairveil will be documented in this file.

This project follows the release policy documented in:

- `docs/clairveil-release-versioning-policy.md`
- `docs/clairveil-release-handoff-pack.md`

## Unreleased

- Added standalone Clairveil privacy core, reference daemon, prover service, fixtures, schemas, CI, and release handoff documentation.
- Added Apache-2.0 open-source hygiene files.
- Added release versioning and release note policy.
- Added release handoff pack verification command.
- Updated public documentation for release verification, restore SOP, security reporting, reference app status, and portable walkthrough paths.
- Expanded Korean public documentation for circuits, CLI, testing, operations, maintainer instructions, release notes, community templates, and the project README.
- Added `make install` and `make init` helpers for installing Clairveil binaries and preparing a default local `~/.clairveil` chain home.
- Clarified quick-start and testing docs to avoid redundant Make target sequences and document the manual walkthrough versus `make init` shortcut.
- Added a dependency-free Node audit disclosure key example under `examples/audit-disclosure-keys`.
- Removed legacy output-note fields from `MsgWithdraw`; withdraw remains exact-match and clients should regenerate proto bindings without dummy output-note values.
- Added general client handoff documents for wallet/app product planning, UX flows, security decisions, and API integration.
- Added privacy accounting hardening updates: bounded shielded amounts, deposit binding proofs, reserve accounting queries, and updated ZK artifact contracts.
