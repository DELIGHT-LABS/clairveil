# Clairveil WebApp 배포

English version: [clairveil-web-app-deployment.md](clairveil-web-app-deployment.md)

이 문서는 지원되는 WebApp 흐름의 browser deployment boundary를 정의합니다. Wallet privacy 작업을 application server로 옮기는 것을 허용하지 않습니다.

## Deployment 형태

| 형태 | 허용 | 금지 |
| --- | --- | --- |
| Static public WebApp | Static asset, public REST/RPC, public prover endpoint, browser wallet, client-side privacy preparation, durable relay-payload copy/handoff, pinned product `depositProofUrl` 또는 browser/WASM deposit prover | Root signature, seed, decrypted note, prepared witness, disclosure plaintext를 app server로 전송 |
| Same-origin gateway | 아래 production control을 갖춘 configured read endpoint/prover request의 narrow proxy | Gateway를 trusted wallet로 취급, endpoint semantic을 조용히 변경, broad proxying |
| Local demo helper | Loopback의 faucet, local test signer, test deposit proof, local relayer, admin/auditor tool | Local signing/admin route나 prover proxy를 기본값으로 public/LAN origin에 노출 |

Deposit proof generation은 product-provided입니다. Browser/WASM prover 또는 검토된 proof service가 제공할 수 있습니다. Remote prover는 privacy-sensitive witness material을 받으므로 named privacy boundary이며 교환 가능한 commodity endpoint가 아닙니다.

## Browser endpoint 규칙

각 configured profile은 아래를 만족해야 합니다.

- Production에 HTTPS REST/RPC/prover URL을 publish하고 HTTPS page와 cleartext HTTP endpoint를 섞지 않습니다.
- Deposit에 remote proof service를 쓰면 profile에 정확한 HTTPS `depositProofUrl`을 pin합니다. `proverUrl`이나 local helper route를 조용히 재사용하면 안 됩니다.
- Exact deployed WebApp origin과 필요한 method `GET`, `POST`, `OPTIONS`에만 CORS를 허용합니다.
- 필요한 request header만 허용합니다. 일반적으로 `Content-Type`이며 prover contract가 사용할 때만 `Authorization`을 추가합니다.
- Credentialed request와 wildcard origin을 함께 허용하지 않습니다.
- Bounded request/response body size와 explicit timeout을 둡니다.
- Proof, payload, note, key, bearer token 내용을 echo하지 않는 `Content-Type: application/json`의 versioned JSON error를 반환합니다.
- Endpoint URL을 validated chain profile에 pin합니다. Public read failover는 bounded이고 nullifier/prover failover는 더 엄격한 privacy policy를 따릅니다.
- 모든 profile과 deployment-gate URL에서 URL userinfo(`https://user:password@host/...`), query string, fragment를 거부합니다. Configuration endpoint는 origin-plus-path만 허용합니다. Secret과 request parameter는 검토된 runtime header/body flow에 두며 static artifact, CSP generation, browser diagnostic, release log에 넣으면 안 됩니다.

WebApp은 prover bearer token을 HTTPS의 선택된 prover origin에만 전송합니다. Static JavaScript, query string, localStorage, analytics, error report에 두면 안 됩니다. Public static DApp은 user/session-scoped token acquisition design 또는 운영 abuse control이 있는 unauthenticated prover가 필요합니다.

## Same-origin gateway 요구사항

Production gateway는 optional입니다. 활성화한다면 아래를 만족해야 합니다.

1. Upstream origin과 exact path allowlist를 사용하고 browser가 arbitrary URL을 넘기게 하지 않습니다.
2. Cookie 사용 시 Origin check, CSRF protection, rate limit, request body limit, per-route timeout을 강제합니다.
3. Access log, trace, analytics, error에서 proof/payload body와 authorization value를 redact합니다.
4. Request/response version을 보존하고 typed scan failure를 legacy scan success로 바꾸지 않습니다.
5. Personalized nullifier query나 prover request/response body를 cache하지 않습니다.
6. Local test-only helper route를 public deployment와 분리합니다.

예제의 `CLAIRVEIL_PROVER_PROXY_ENABLED`는 local-test control이지 production deployment recipe가 아닙니다. Checked-in server는 local-test mode, 명시적 flag, direct loopback request가 모두 참일 때만 이를 허용합니다. Public mode는 이 flag와 server가 보유한 prover bearer token을 모두 거부합니다. Product에 gateway가 필요하면 별도로 검토한 gateway를 배포해야 합니다. Local proxy는 upstream redirect와 final URL 변경을 거부하고 versioned JSON success shape를 검사하며 parsing 전에 response 크기를 제한합니다. 모든 upstream error body는 stable하고 민감하지 않은 JSON error로 교체합니다. Server-backed `/api/health` read gateway도 bounded upstream JSON, request별 timeout, client disconnect cancellation, process 전체 admission 상한을 사용합니다. 예제는 이를 `CLAIRVEIL_PROVER_PROXY_MAX_RESPONSE_BYTES`, `CLAIRVEIL_DAPP_UPSTREAM_TIMEOUT_MS`, `CLAIRVEIL_DAPP_UPSTREAM_MAX_RESPONSE_BYTES`, `CLAIRVEIL_DAPP_HEALTH_MAX_IN_FLIGHT`로 설정합니다.

Checked-in v0.3.1 example은 batch-transfer exposure flag를 제공하지 않습니다.
Server는 항상 `serverFeatures.batchTransfer=false`를 반환하고 UI는
`prepareTransferBatch`를 호출하지 않으며 `make dapp-local`도 batch 메뉴를
활성화하지 않습니다. ClairveilJS batch API가 있거나
`/v1/proofs/batch-transfer` endpoint에 접속할 수 있다는 이유로 capability
discovery로 취급하면 안 됩니다. One-proof batch transfer를 노출할 향후
product는 product flow를 advertise하거나 활성화하기 전에 별도의 explicit gate,
encrypted recovery checkpoint, wallet confirmation, typed reconciliation, end-to-end deployment test를
추가하고 검토해야 합니다.

## Browser security header와 telemetry

Configured origin에 맞는 restrictive CSP로 WebApp을 제공합니다. 최소한 `default-src 'self'`를 설정하고 `connect-src`에 wallet, REST/RPC, prover, `depositProofUrl`, optional gateway origin을 열거하며 broad `connect-src *`를 쓰지 않습니다. Checked-in WebApp에는 inline/third-party script 요구사항이 없으므로 `script-src 'self'`만 정확히 설정합니다. 별도 검토한 product design 없이 CDN, nonce, hash, `unsafe-inline`으로 넓히면 안 됩니다. HTTPS, 제품에 맞는 `frame-ancestors`, `X-Content-Type-Options: nosniff`, restrictive referrer policy를 사용합니다.

Telemetry는 high-level operation category, endpoint class, tx hash, height, stable error code만 보존할 수 있습니다. Root material, private key, note data, Merkle path, raw proof/payload byte, disclosure plaintext, private operation의 recipient/amount, prover bearer token은 절대 보존하면 안 됩니다.

Upstream prover의 error text도 sensitive input으로 취급합니다. 이를 render하거나 gateway로 전달하거나 diagnostic에 붙이기 전에 stable code와 일반적인 display message로 바꿔야 합니다. Checked-in example은 loopback prover proxy와 private operation이 render하는 모든 proof-error path에 이 규칙을 적용합니다.

예제 static server는 profile에서 유도한 `connect-src` CSP와 `script-src 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`을 보냅니다. 기본 bind는 loopback입니다. Public mode에서는 `CLAIRVEIL_DAPP_PUBLIC_ORIGIN`이 없거나 HTTPS가 아니면, 또는 profile endpoint가 HTTPS가 아니면 시작을 거부합니다. Node server 자체는 TLS terminator가 아니므로 TLS reverse proxy 뒤에 배포하거나 static bundle을 HTTPS origin에서 제공하고 최종 origin의 header를 test해야 합니다.

## Release origin 검증

TLS proxy/CDN 설정을 적용한 뒤 최종 production value로 repository gate를 실행합니다.
`CLAIRVEIL_WEBAPP_CONFIG_URL`은 browser가 bootstrap에 실제 사용하는 same-origin
JSON response를 정확히 가리켜야 합니다. Server-backed 예제에서는 Web client
config를 `config` 아래에 담은 `/api/health`이며, `/api/config`은 정보 제공용일 뿐
bootstrap source를 대체할 수 없습니다. Server-backed deployment에서 gate는 같은
origin의 `/api/config`이 반환하는 bare Web client config도 별도로 가져옵니다. 두
payload를 완전한 ClairveilJS schema로 각각 검증한 뒤 canonical equality를
요구합니다. Object key 순서는 무시하지만 array 순서와 모든 field 값은
일치해야 합니다. 따라서 partial rollout 또는 proxy 분리 때문에 두 route가 서로
다른 profile을 반환하면 release gate가 실패합니다. Checked-in static WebApp에서는
`/dapp-config.json`입니다.
두 response 모두 public REST failover 전체를 포함해야 하므로 gate는 별도로 복사한
profile 목록이 아니라 browser가 실제로 받는 config를 검증합니다. Artifact는
redirect 없이 직접 응답해야 하며 browser와 release gate는 redirect되었거나 final
URL이 다른 configuration response를 거부합니다.

Endpoint를 probe하기 전에 gate는 browser와 동일한 ClairveilJS
`validateClairveilWebClientConfig(...)` contract를 실행합니다. 알 수 없는 schema
version 또는 field, 찾을 수 없는 active profile, transport/wallet field 불일치,
불완전한 profile metadata, 오래된 flattened compatibility field를 거부합니다.

```bash
CLAIRVEIL_WEBAPP_ORIGIN=https://app.example.com \
CLAIRVEIL_WEBAPP_CONFIG_URL=https://app.example.com/dapp-config.json \
npm --prefix examples/clairveil-dapp run verify:production-deployment
```

Server-backed 예제를 검증할 때는 대신
`https://app.example.com/api/health`를 사용합니다. Static 검증은
`/dapp-config.json`만 읽고 server-backed config route는 호출하지 않습니다.

이 gate는 non-HTTPS value, broad `connect-src *`, external/inline script allowance,
누락되었거나 wildcard인 `frame-ancestors` directive를 거부합니다. 30초 request
bound와 1 MiB response limit 아래에서 배포 configuration을 읽고, 최종 WebApp
response header/restrictive CSP를 확인하며 모든 configured REST endpoint, RPC, prover,
deposit proof endpoint, Keplr endpoint, EVM RPC에 허용 origin과 untrusted origin CORS
preflight 및 non-sensitive actual request를 모두 보냅니다. 모든 endpoint probe는
redirect-error mode를 사용하고 response가 보고하는 final URL이 configured probe URL과
같아야 합니다. Preflight와 actual response
모두에서 exact-origin CORS를 요구하고 불필요한 CORS method/header를 거부합니다. 실제 browser wallet
extension을 emulation할 수는 없습니다. Release 승인 전에는 같은 origin을 대상으로 문서화된
Keplr 또는 MetaMask connect, sign, scan, recovery flow를 수동으로 완료해야 합니다.

## 운영과 failure policy

- Read query는 bounded retry를 사용할 수 있습니다. Wallet은 failover 중 typed scan format/cursor semantic을 조용히 바꾸면 안 됩니다.
- Nullifier query는 기본적으로 같은 endpoint에서 retry합니다. Cross-endpoint failover는 nullifier set을 노출하므로 명시적 privacy 선택입니다.
- Prover retry는 기본적으로 same-endpoint만 허용합니다. 두 번째 prover는 새 witness-sharing boundary이므로 명시적 user/product opt-in이 필요합니다.
- Broadcast retry/failover는 기본 off입니다. 새 external submission 전에 transaction identity/nullifier reconciliation으로 복구합니다.
- 모든 asynchronous wallet connect, chain switch, setup, signing 결과는 active
  profile과 privacy session에 묶습니다. Account, network, profile이 바뀌면 pending
  결과가 UI나 privacy state를 갱신하기 전에 버립니다. 이 in-memory identity reset에서
  이전 encrypted reservation namespace를 지우면 안 됩니다.
- Prover authorization/body-limit failure, latency, stable error code를 alert하되 sensitive request content를 포함하지 않습니다.

Release 전 configured Cosmos/EVM wallet extension, static asset origin, REST/RPC endpoint, prover의 실제 production CORS/CSP policy를 test합니다. Local browser에서 성공했다고 production origin의 access policy가 증명되는 것은 아닙니다.
