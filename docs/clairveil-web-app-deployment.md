# Clairveil WebApp Deployment

Korean version: [clairveil-web-app-deployment-kr.md](clairveil-web-app-deployment-kr.md)

This document defines the browser deployment boundary for the supported WebApp
flows. It does not authorize moving wallet privacy work into an application
server.

## Deployment Shapes

| Shape | Allowed | Not allowed |
| --- | --- | --- |
| Static public WebApp | Static assets, public REST/RPC, public prover endpoint, browser wallet, client-side privacy preparation, durable relay-payload copy/handoff, and a pinned product `depositProofUrl` or browser/WASM deposit prover. | Sending root signatures, seeds, decrypted notes, prepared witnesses, or disclosure plaintext to the app server. |
| Same-origin gateway | A narrowly scoped proxy for configured read endpoints or prover requests, with production controls below. | Treating the gateway as a trusted wallet, silently changing endpoint semantics, or broad proxying. |
| Local demo helper | Faucet, local test signers, test deposit proof, local relayer, and admin/auditor tooling on loopback. | Exposing local signing/admin routes or the prover proxy to a public/LAN origin by default. |

Deposit proof generation is product-provided. A browser/WASM prover or a
reviewed proof service may supply it. A remote prover receives privacy-sensitive
witness material, so it is a named privacy boundary and not an interchangeable
commodity endpoint.

## Browser Endpoint Rules

For every configured profile:

- publish HTTPS REST/RPC/prover URLs for production; do not mix an HTTPS page
  with cleartext HTTP endpoints;
- if Deposit uses a remote proof service, pin its exact HTTPS `depositProofUrl`
  in the profile; do not silently reuse `proverUrl` or a local helper route;
- allow CORS only for the exact deployed WebApp origins and required methods:
  `GET`, `POST`, and `OPTIONS`;
- allow only the needed request headers, normally `Content-Type` and,
  when the prover contract uses it, `Authorization`;
- do not allow wildcard origins with credentialed requests;
- set bounded request/response body sizes and explicit timeouts;
- return versioned JSON errors with `Content-Type: application/json` without echoing proof, payload, note, key, or
  bearer-token content;
- pin endpoint URLs in the validated chain profile. Public read failover is
  bounded; nullifier and prover failover follows the stricter privacy policy.
- reject URL userinfo (`https://user:password@host/...`), query strings, and
  fragments in every profile and deployment-gate URL. Configuration endpoints
  are origin-plus-path only; secrets and request parameters belong in a
  reviewed runtime header/body flow, never static artifacts, CSP generation,
  browser diagnostics, or release logs.

The WebApp must send a prover bearer token only to the selected prover origin
over HTTPS. Do not put it in static JavaScript, query strings, localStorage,
analytics, or error reports. A public static DApp normally needs a
user/session-scoped token acquisition design or an unauthenticated prover with
operational abuse controls.

## Same-Origin Gateway Requirements

A production gateway is optional. If one is enabled, it must:

1. use an explicit allowlist of upstream origins and exact paths; never accept
   an arbitrary URL from the browser;
2. enforce Origin checks, CSRF protection where cookies are used, rate limits,
   request body limits, and per-route timeouts;
3. redact proof/payload bodies and authorization values from access logs,
   traces, analytics, and errors;
4. preserve request/response versions and must not convert a typed scan failure
   into legacy scan success;
5. avoid caching personalized nullifier queries or prover request/response
   bodies;
6. separate local test-only helper routes from every public deployment.

`CLAIRVEIL_PROVER_PROXY_ENABLED` in the example is a local-test control,
not a production deployment recipe. The checked-in server permits it only
when local-test mode, the explicit flag, and a direct loopback request are all
true. Public mode rejects both this flag and a server-held prover bearer token;
deploy a separately reviewed gateway when a product needs one.
The local proxy rejects upstream redirects and final-URL changes, requires a
versioned JSON success shape, bounds the response before parsing it, and replaces
every upstream error body with a stable non-sensitive JSON error. The
server-backed `/api/health` read gateway likewise uses bounded upstream JSON,
per-request timeouts, disconnect cancellation, and a process-wide admission
limit. The example exposes these limits as
`CLAIRVEIL_PROVER_PROXY_MAX_RESPONSE_BYTES`,
`CLAIRVEIL_DAPP_UPSTREAM_TIMEOUT_MS`,
`CLAIRVEIL_DAPP_UPSTREAM_MAX_RESPONSE_BYTES`, and
`CLAIRVEIL_DAPP_HEALTH_MAX_IN_FLIGHT`.

The checked-in v0.3.1 example does not provide a batch-transfer exposure flag.
Its server always reports `serverFeatures.batchTransfer=false`, the UI does not
call `prepareTransferBatch`, and `make dapp-local` does not enable a batch menu.
Do not treat the presence of ClairveilJS batch APIs or a reachable
`/v1/proofs/batch-transfer` endpoint as capability discovery. A future product
that exposes one-proof batch transfer must add and review its own explicit gate,
encrypted recovery checkpoint, wallet confirmation, typed reconciliation, and
end-to-end deployment tests before advertising or enabling the product flow.

## Browser Security Headers And Telemetry

Serve the WebApp with a restrictive CSP tailored to its configured origins.
At minimum, set `default-src 'self'`, enumerate `connect-src` for wallet,
REST/RPC, prover, `depositProofUrl`, and optional gateway origins, and do not use broad
`connect-src *`. The checked-in WebApp has no inline or third-party script
requirement, so set `script-src 'self'` exactly; do not widen it with a CDN,
nonce, hash, or `unsafe-inline` without a separately reviewed product design.
Use HTTPS, `frame-ancestors` appropriate to the product,
`X-Content-Type-Options: nosniff`, and a restrictive referrer policy.

Telemetry may retain a high-level operation category, endpoint class, tx hash,
height, and stable error code. It must never retain root material, private
keys, note data, Merkle paths, raw proof/payload bytes, disclosure plaintext,
recipient/amount data from a private operation, or prover bearer tokens.

Treat upstream prover error text as sensitive input too. Map it to a stable
code and generic display message before rendering it, forwarding it through a
gateway, or attaching it to diagnostics. The checked-in example applies this
rule to the loopback prover proxy and every proof-error path rendered by a
private operation.

The example static server emits a profile-derived `connect-src` CSP and
`script-src 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and
`Cross-Origin-Opener-Policy: same-origin`. It binds to loopback by default.
In public mode it rejects missing/non-HTTPS `CLAIRVEIL_DAPP_PUBLIC_ORIGIN` and
non-HTTPS profile endpoints. The Node server itself is not a TLS terminator:
deploy it behind a TLS reverse proxy or serve the static bundle from an HTTPS
origin, then test the final headers at that origin.

## Release-Origin Verification

Run the repository gate against the final production values, after the TLS
proxy/CDN configuration is live. `CLAIRVEIL_WEBAPP_CONFIG_URL` must identify
the exact same-origin JSON response from which the browser bootstraps. For the
server-backed example this is `/api/health`, with the Web client config under
`config`; `/api/config` is informational and is not an acceptable bootstrap
substitute. For a server-backed deployment, the gate also fetches the bare Web
client config from the same-origin `/api/config` route. It validates both
payloads against the complete ClairveilJS schema and requires exact canonical
equality: object key order is ignored, array order and every field value are
significant. A partial rollout or proxy split that serves different profiles
from the two routes therefore fails the release gate.
For the checked-in static WebApp this is `/dapp-config.json`. Both responses
must include every public REST failover. This makes the gate validate the
configuration the browser actually receives rather than an independently
copied profile list. The artifact must respond
directly without a redirect; both the browser and the release gate reject a
redirected or final-URL-mismatched configuration response.

Before probing any endpoint, the gate runs the same ClairveilJS
`validateClairveilWebClientConfig(...)` contract as the browser. It rejects an
unknown schema version or field, an unresolved active profile, transport/wallet
field mismatches, incomplete profile metadata, and stale flattened compatibility
fields.

```bash
CLAIRVEIL_WEBAPP_ORIGIN=https://app.example.com \
CLAIRVEIL_WEBAPP_CONFIG_URL=https://app.example.com/dapp-config.json \
npm --prefix examples/clairveil-dapp run verify:production-deployment
```

Use `https://app.example.com/api/health` instead when verifying the
server-backed example. Static verification reads only `/dapp-config.json` and
does not probe either server-backed config route.

The gate rejects non-HTTPS values, broad `connect-src *`, external or inline
script allowances, and a missing or wildcard `frame-ancestors` directive. It
reads the deployed configuration under a 30-second request bound and a 1 MiB
response limit, verifies the final WebApp response
headers and restrictive CSP, and sends both allowed-origin and untrusted-origin
CORS preflights plus non-sensitive actual requests to every configured REST
endpoint, RPC, prover, deposit proof endpoint, Keplr endpoint, and EVM RPC.
Every endpoint probe uses redirect-error mode and requires any reported final
response URL to equal the configured probe URL.
It requires exact-origin CORS on both preflight and actual responses, and
rejects unnecessary CORS methods and headers. It intentionally cannot emulate a
real browser wallet extension.
Complete the documented Keplr or MetaMask connect, sign, scan, and recovery
flow manually against those same origins before approving the release.

## Operations And Failure Policy

- Read queries can use bounded retry. A wallet must not silently change typed
  scan format/cursor semantics while failing over.
- Nullifier queries retry the same endpoint by default. Cross-endpoint failover
  is an explicit privacy choice because it exposes the nullifier set.
- Prover retry is same-endpoint only by default. A second prover is a new
  witness-sharing boundary and requires explicit user/product opt-in.
- Broadcast retry/failover is off by default. Recover using transaction
  identity and nullifier reconciliation before a new external submission.
- Bind every asynchronous wallet connect, chain-switch, setup, and signing
  result to the active profile and privacy session. On account, network, or
  profile change, discard the pending result before it can update UI or
  privacy state; do not erase the previous encrypted reservation namespace as
  part of that in-memory identity reset.
- Alert on prover authorization failures, body-limit failures, latency, and
  stable error codes; do not alert with sensitive request content.

Before release, test the actual production CORS and CSP policy with the
configured Cosmos/EVM wallet extension, static asset origin, REST/RPC endpoint,
and prover. A local browser success is not evidence that a production origin
has the required access policy.
