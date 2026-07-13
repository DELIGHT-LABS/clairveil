# Security Policy

Clairveil is a reusable privacy-core repository. Do not report suspected vulnerabilities through public issues.

Korean version: [SECURITY-kr.md](SECURITY-kr.md)

## Supported Versions

| Line | Status |
| --- | --- |
| `main` | Supported development line; fixes land here first |
| latest `v0.1.x` tag | Supported for security assessment; backports are case-by-case and announced explicitly |
| `v0.0.x` and older commits | Unsupported |

Clairveil is pre-v1 experimental software. A tag is not a promise of production readiness or indefinite maintenance. Pin an exact tag/commit, read the documentation at that ref, and check release notes for any explicitly supported backport.

## Reporting a Vulnerability

Send a private report through GitHub private vulnerability reporting:

```text
https://github.com/DELIGHT-LABS/clairveil/security/advisories/new
```

If the private advisory form is unavailable, do not open a public issue with exploit details. Contact maintainers through a private channel first and wait for a coordinated disclosure path.

There is no contractual response-time SLA for this experimental project. Maintainers will use the private advisory to coordinate triage, remediation, credit, and disclosure timing; do not publish details before that coordination is complete.

Include as much detail as possible:

- affected commit, tag, or branch
- impacted component, such as `x/privacy`, `clairveil-proverd`, ZK artifact tooling, CLI, fixture/schema, or Docker packaging
- reproduction steps or proof of concept
- expected impact
- whether the issue affects downstream production deployments, this standalone reference repo, or both

## Scope

In scope:

- consensus or state-machine safety issues in `x/privacy`
- proof verification, payload binding, disclosure, or nullifier correctness issues
- private key, viewing key, disclosure key, wallet note, prepared payload, or prover metadata exposure caused by Clairveil code
- remote prover authentication, timeout, body-limit, or response-binding issues
- supply-chain issues in release artifacts, Docker packaging, schemas, or conformance fixtures

Out of scope for this standalone repository:

- downstream validator operations
- downstream EVM, policy module, precompile, wasm, or IBC integrations
- downstream audit private key custody
- production web wallet encrypted storage choices
- production artifact signing or ceremony policy not implemented in this repo

These out-of-scope areas are still important production risks. They should be handled in downstream project threat models and release checklists.

## Baseline Security Checks

Maintainers should run:

```bash
make vulncheck
make release-check
```

`make vulncheck` currently includes narrow documented policy exceptions for `GO-2024-2584`, the `pion/dtls` v2 path of `GO-2026-4479`, and `GO-2026-5932` (`x/crypto/openpgp`, reachable only through Cosmos SDK local ASCII key armor in this repository) while those upstream dependency paths have no fixed versions for this reference app. Clairveil does not use OpenPGP for application signing or encryption. These are not blanket vulnerability waivers: downstream production projects must reassess reachability and impact in their own risk register and remove each exception when a fixed dependency path exists.
