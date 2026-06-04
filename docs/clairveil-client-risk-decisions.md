# Clairveil Client Risk Decisions

This document summarizes the risk decisions that product, security, and operations teams must make when building a Clairveil client.

Korean version: [clairveil-client-risk-decisions-kr.md](clairveil-client-risk-decisions-kr.md)

This document does not replace a full threat model. The downstream project must still write a chain-, wallet-, prover-, and custody-specific threat model.

## 1. Sensitive Data Classification

The client must classify the following data as sensitive.

| Data | Risk | Recommended Policy |
| --- | --- | --- |
| root signer material/root seed | compromise of the whole shielded identity | secure storage, export limits, no logs |
| spend key | authority to spend notes | consider hardware-backed storage |
| view key | note/balance privacy exposure | encrypted local storage |
| disclosure key | authority to decrypt disclosure payloads | role-specific custody policy |
| note cache | amount, asset, ownership metadata exposure | encrypted DB, rescan/reset support |
| prepared payload/proof | prover metadata exposure | expiry, deletion, redaction |
| disclosure plaintext/report | private transfer metadata exposure | verified display, no logs |
| prover bearer token | remote proof API access | secure storage, rotation, no logs |

## 2. Storage Policy By Client Profile

| Profile | Baseline Direction |
| --- | --- |
| Mobile wallet | Consider iOS Keychain, Android Keystore, encrypted local DB, and background task limits. |
| Web wallet | Decide browser storage encryption, session lifetime, and extension/dapp isolation. |
| Desktop wallet | Local file permissions are not enough; consider OS keychain or encrypted DB. |
| Custodial/backend wallet | HSM/KMS, role separation, approval flow, and audit logs are required. |

Questions product teams must answer:

- Can the user export view keys and note cache?
- Are privacy payloads redacted from crash reports?
- What risk and time cost does the user see during cache reset/rescan?
- Is there a restore UX for device loss or account recovery?

## 3. Prover Topology

| Topology | Benefits | Risks/Constraints | Suitable Clients |
| --- | --- | --- | --- |
| Browser/WASM prover | minimizes external metadata transfer | performance, memory, artifact delivery | web, some mobile web |
| Mobile local prover | strong user privacy | app size, CPU, battery, platform constraints | high-privacy mobile |
| Desktop local daemon | balance of performance and privacy | daemon lifecycle, local auth | desktop wallet |
| Private remote sidecar | operational control | operator can see payload metadata | company wallet/backend |
| Public remote prover | simpler UX | auth, DoS, metadata exposure | consumer wallet with trust disclosure |

The client must define:

- prover request timeout
- retry and cancellation
- auth token storage
- request/response body logging ban
- remote prover data retention
- user-facing copy that explains remote prover trust

## 4. Audit Disclosure

Every shielded transfer includes audit disclosure targeted to the chain-configured audit master pubkey.

Client responsibilities:

- Fetch the current audit master pubkey through the audit config query.
- Ensure transfer payloads include audit target pubkey and digest.
- If the product needs auditor UX, provide audit disclosure decrypt and verification reports.

Auditor private key custody is a downstream operations responsibility. Production deployments should define HSM/KMS, role separation, access approval, key rotation, and incident response.

## 5. Disclosure Verification Policy

Successful decryption is different from successful verification.

Client policy:

- Disclosure plaintext must not be shown as factual unless `verification.verified=true`.
- Distinguish public disclosure from recipient/audit disclosure.
- Treat digest mismatch as a trust failure, not a minor warning.
- Do not write disclosure plaintext or reports to logs, analytics, or crash reports.

## 6. Logging And Telemetry

Clients and provers must not record these values.

- root seed/root signer material
- spend/view/disclosure private key
- note amount/randomness/nullifier
- Merkle path
- prepared payload body
- proof request/response body
- disclosure plaintext
- bearer token

When needed, keep only lower-sensitivity identifiers such as tx hash, height, or high-level error code.

## 7. Product Risk Decisions

Before release, product/security/operations teams should answer at least these questions.

- Must users trust a remote prover?
- Is there a local fallback when the remote prover is unavailable?
- How does the product explain exact-match withdraw failure?
- How does the product display balance when note scan fails?
- What does the screen hide or warn about when disclosure verification fails?
- Who owns auditor key custody and access approval?
- Where are prepared payload/proof JSON files stored and when are they deleted?
- Do secure storage requirements differ by mobile/web/desktop profile?

## 8. Related Documents

- [Threat model](clairveil-threat-model.md)
- [Security best practices review](clairveil-security-best-practices-review.md)
- [Remote prover production profile](clairveil-proverd-remote-production-profile.md)
- [Client product brief](clairveil-client-product-brief.md)
- [Client UX flows](clairveil-client-ux-flows.md)
