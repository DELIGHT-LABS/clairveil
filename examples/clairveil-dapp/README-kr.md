# Clairveil DApp

Clairveil DApp은 브라우저에서 Keplr 또는 MetaMask를 연결해 Clairveil privacy 기능을 테스트하는 예제 웹앱입니다.

이 예제의 핵심 경계는 단순합니다.

- **DApp**: 입력값과 UI 흐름만 관리하고, ClairveilJS high-level API를 호출합니다.
- **ClairveilJS**: note 생성, commitment/encrypted note, scan, note planning, prover payload, disclosure encode/decode, deposit/transfer/withdraw 준비를 처리합니다.
- **Optional local server**: 로컬 테스트용 static server, faucet, local signer, deposit proof 생성, relayer broadcast, auditor/admin helper만 제공합니다.

즉 일반적인 public node 환경에서는 DApp + ClairveilJS + 공개 RPC/REST + prover URL로 실행할 수 있습니다. 다만 Deposit은 `DepositCircuit` proof provider가 추가로 필요하므로, production static 배포에서는 browser/WASM prover 또는 신뢰할 수 있는 proof endpoint를 별도로 연결해야 합니다.

## 파일 구조

| 파일 | 역할 |
| --- | --- |
| `public/dapp-config.json` | 브라우저와 production gate가 읽는 same-origin static chain profile artifact |
| `public/dapp-config.js` | static config artifact를 fail-closed로 읽는 loader |
| `public/app.js` | DApp UI와 wallet event 흐름 |
| `public/app.bundle.js` | `npm run build:dapp`로 생성되는 브라우저 번들 |
| `server.js` | local test helper 서버 (faucet, local signer...) |
| `.env.example` | 로컬/server-backed 실행에서 사용할 수 있는 환경 변수 override 템플릿 |
| `test/dapp-smoke.test.js` | DApp 구조와 privacy boundary smoke test |

CI, release build, `make dapp-local`은 모두 `vendor/` 아래에 체크인된 content-addressed ClairveilJS archive를 설치합니다. 따라서 로컬 실행도 재현 가능하며 sibling ClairveilJS checkout에 의존하지 않습니다. 최소 SDK 사용 흐름 예제는 ClairveilJS 쪽 `examples/minimal-keplr-flow.js`, `examples/minimal-metamask-flow.js`에 둡니다.

## 주요 기능

- Chain profile dropdown
  - 기본 static config는 Cosmos/Keplr profile을 노출합니다.
  - Target chain이 호환되는 privacy precompile을 제공하면 EVM/MetaMask profile을 선택적으로 추가할 수 있습니다.
  - 선택한 profile에 따라 connect 버튼도 하나만 보입니다.
- Wallet Session
  - 현재 연결된 wallet 종류, account, signer check, copy account를 표시합니다.
- Clair
  - transparent balance 조회
  - public send
  - Deposit, 즉 transparent balance를 veiled note로 이동
- Veiled
  - note scan
  - spendable-only toggle
  - Transfer, 즉 veiled send
  - Feature gate된 Cosmos one-proof 원자적 일괄 지급
  - Withdraw, 즉 veiled balance를 transparent account로 이동
  - Relay withdraw payload를 local relayer helper로 제출하는 테스트 흐름
  - self transaction/planner 단계 안내
- Disclosure
  - none
  - public
  - recipient-encrypted
  - user disclosure decode
  - local/admin 전용 audit disclosure decode
- Events
  - Privacy Events
  - Event Block
  - disclosure 가능한 transfer detail
- Local test helpers
  - faucet
  - alice/bob/relayer/auditor local signer
  - local CLI deposit/note scan
  - auditor test scalar/decode

Spendable balance를 보여주거나 privacy action을 준비하기 전 DApp은 active node/tree, consensus circuit, asset registry, audit/disclosure config, reserve를 검증합니다. Preflight가 실패하거나 오래되면 다음 성공 전까지 spendable inventory를 숨기고 deposit, transfer, withdraw, relay withdraw를 막습니다.

현재 WebApp은 일반 2x2 Transfer를 기본 진입점으로 유지합니다. Cosmos one-proof 원자적 일괄 지급은 `serverFeatures.batchTransfer`가 활성화된 경우에만 노출하며 payroll, recipient-file import, unattended bulk orchestration은 계속 범위 밖입니다. 지원 제품 경계는 [WebApp scope](../../docs/clairveil-web-app-scope-kr.md)에 고정되어 있습니다.

## Production integration 문서

이 예제를 제품으로 전환할 때 아래 문서를 사용합니다.

- [WebApp scope](../../docs/clairveil-web-app-scope-kr.md): 지원 흐름, batch feature gate, bulk orchestration 제외 범위
- [WebApp integration](../../docs/clairveil-web-app-integration-kr.md): Browser client API, lifecycle, broadcast 순서, test 요구사항
- [WebApp 저장소와 복구](../../docs/clairveil-web-app-storage-recovery-kr.md): Encrypted persistence, tab lease, reconciliation, 0.2 migration
- [WebApp 배포](../../docs/clairveil-web-app-deployment-kr.md): CORS, prover authentication, CSP, optional proxy, telemetry 경계
- [Web client config schema](../../docs/schemas/clairveil-web-client-config.schema.json): Versioned static/server profile contract

## 아키텍처

```text
Browser DApp
  -> ClairveilJS browser client
    -> Cosmos REST/RPC
    -> EVM JSON-RPC
    -> Prover HTTP
    -> Keplr / MetaMask

Optional local server
  -> static files
  -> /api/config, /api/health
  -> local faucet
  -> local signer CLI helpers
  -> local/admin auditor helpers
```

DApp은 사용자 wallet privacy flow를 서버로 보내지 않습니다. `deposit`, `transfer`, `withdraw`, `scan`, `user disclosure decode`, `broadcast`, `wait`는 브라우저 ClairveilJS가 처리합니다.

서버는 로컬 테스트 모드에서만 권한성 helper를 제공합니다. Public node mode에서는 local signer/faucet/admin route가 숨겨지고 403으로 막힙니다.

## DApp이 사용하는 엔드포인트

### Optional DApp server endpoints

이 엔드포인트들은 `server.js`가 켜져 있을 때만 있습니다. Public node 환경에서는 필요한 production 서비스를 따로 연결한 뒤 local helper 없이 static DApp + ClairveilJS로 wallet privacy flow를 수행할 수 있습니다. Deposit은 active profile에 검토된 `depositProofUrl`이 있으면 이를 사용하고, local test mode에서는 loopback 전용 `/api/deposit/proof` helper를 fallback으로 사용합니다. 둘 다 없으면 비활성화합니다.

| Endpoint | Mode | 용도 |
| --- | --- | --- |
| `GET /api/config` | all | server-backed config와 chain profile 전달 |
| `GET /api/health` | all | local node 상태, tree/audit config, local accounts 확인 |
| `POST /api/local-signers/ensure` | local only | alice/bob/relayer/auditor 등 local signer 생성 |
| `GET /api/wallet/:name/show-address` | local only | local signer의 transparent/shielded 주소 조회 |
| `GET /api/wallet/:name/notes` | local only | local signer note scan |
| `POST /api/faucet` | local only | alice/dev account에서 연결된 wallet으로 faucet 송금 |
| `POST /api/deposit/proof` | local only | 브라우저가 준비한 wallet deposit material의 deposit proof 생성 |
| `POST /api/relayer/withdraw` | local only | 브라우저가 준비한 withdraw payload를 local `relayer` key로 제출 |
| `POST /api/deposit` | local only | local CLI signer deposit 테스트 |
| `GET /api/auditor/test-scalar` | local/admin only | 테스트 auditor scalar 조회 |
| `POST /api/auditor/decode` | local/admin only | audit disclosure private scalar로 disclosure decode |

### Browser ClairveilJS high-level calls

DApp UI는 privacy 준비 로직을 직접 구현하지 않고 `clairveiljs/browser-dapp`의 high-level API를 호출합니다. 아래 call들이 선택된 chain profile의 REST/RPC/prover/wallet API를 사용합니다.

| ClairveilJS call | 사용하는 네트워크/API |
| --- | --- |
| `health()` | RPC `/status`, REST `/tree_state`, REST `/audit_config` |
| `assertTransferProtocolConfig(denom)` | consensus circuit identity, asset registry, audit/disclosure config 검증 |
| `queryReserve(denom)` | 현재 denom reserve response 검증 |
| `getBalances(address)` | REST `/cosmos/bank/v1beta1/balances/{address}` |
| `buildBankSendSignDoc(...)` | REST account info, Keplr `signDirect` |
| `evmNativeSendTransaction(...)` | MetaMask `eth_sendTransaction` |
| `prepareDeposit(...)` | active profile의 `depositProofUrl` provider(또는 local test helper)로 note/commitment/encrypted note를 만든 뒤 Cosmos sign doc 또는 EVM precompile tx 생성 |
| `scanWalletNotes(...)` | privacy events/commitments/nullifiers 조회 후 wallet root seed로 note scan |
| `prepareTransfer(...)` | note scan, planner, audit config, prover `/v1/prover/transfer`, disclosure payload, Cosmos sign doc 또는 EVM precompile tx 생성 |
| `prepareTransferBatch(...)` | Cosmos batch gate가 켜졌을 때 1–16 input을 원자적으로 reserve하고 payment별 disclosure를 포함한 1–32 output을 만들며 private artifact를 checkpoint하고 `/v1/proofs/batch-transfer`를 한 번 호출해 `MsgBatchTransfer` sign doc 하나를 반환 |
| `prepareWithdraw(...)` | note scan, planner, prover `/v1/prover/withdraw`, Cosmos sign doc 또는 EVM precompile tx 생성 |
| `prepareRelayWithdraw(...)` | note scan, planner, prover `/v1/prover/withdraw`, relayer가 제출할 수 있는 withdraw payload와 reservation metadata 생성 |
| `broadcastSignedTx(...)` | Cosmos signed tx broadcast/wait |
| `waitForEvmTransaction(...)` | EVM receipt wait |
| `fetchPrivacyEvents(...)` | REST privacy event feed 조회 |
| `fetchBlockEvents(...)` | RPC tx search 기반 block/event 조회 |
| `fetchAuditableTransfers(...)` | REST privacy events 중 audit 가능한 transfer 목록 조회 |
| `fetchAuditableBatchTransfers(...)` | 검증된 typed `batch_transfer` summary/output record 조회 |
| `decodeUserDisclosure(...)` | tx/event disclosure payload 조회 후 wallet privacy material로 decode |
| `decodeBatchSelfViewDisclosure(...)` | typed batch output 하나의 sender self-view disclosure를 검증하며, DApp은 complete typed scan 결과 뒤에만 모든 output을 decode |

### ClairveilJS browser client -> Cosmos REST/RPC endpoints

선택한 chain profile의 `rest`와 `rpc`를 사용합니다.

| Endpoint | 용도 |
| --- | --- |
| RPC `/status` | node health |
| RPC `/tx_search` | Event Block / tx inclusion lookup |
| REST `/cosmos/auth/v1beta1/account_info/{address}` | sign doc account number/sequence |
| REST `/cosmos/bank/v1beta1/balances/{address}` | transparent balance |
| REST `/clairveil/privacy/v1/tree_state` | Merkle tree state |
| REST `/clairveil/privacy/v1/events` | privacy event feed / note scan source |
| REST `/clairveil/privacy/v1/commitment/{commitment_hex}` | commitment metadata |
| REST `/clairveil/privacy/v1/nullifier/{nullifier_hex}` | nullifier status |
| REST `/clairveil/privacy/v1/audit_config` | audit master pubkey |
| REST `/clairveil/privacy/v1/disclosure_config` | disclosure config |
| REST `/clairveil/privacy/v1/circuit_config` | circuit config |

Cosmos transaction broadcast는 ClairveilJS가 CosmJS/CometBFT RPC를 통해 처리합니다.

### ClairveilJS browser client -> Prover endpoints

선택한 chain profile의 `proverUrl`을 사용합니다.

| Endpoint | 용도 |
| --- | --- |
| `POST /v1/prover/transfer` | transfer proof 생성 |
| `POST /v1/prover/withdraw` | withdraw proof 생성 |

Deposit은 `DepositCircuit` proof가 필요합니다. Active profile에 설정된 정확한 검토 HTTPS `depositProofUrl`이 있으면 browser가 deposit material을 그 endpoint에 보내며, local test mode에서는 local-only `/api/deposit/proof` helper를 대신 사용해 `MsgDeposit`을 준비할 수 있습니다.

### Browser wallet APIs -> Keplr

Cosmos profile에서만 사용합니다.

| API | 용도 |
| --- | --- |
| `experimentalSuggestChain(chainInfo)` | chain 등록/제안 |
| `getKey(chainId)` | account/pubkey 조회 |
| `signArbitrary(chainId, address, message)` | Clairveil root message 서명 |
| `signDirect(chainId, address, signDoc)` | bank/privacy tx 서명 |

### Browser wallet APIs -> MetaMask

EVM profile에서만 사용합니다.

| API | 용도 |
| --- | --- |
| `eth_chainId` | 현재 EVM chain 확인 |
| `wallet_switchEthereumChain` | 설정된 EVM chain으로 전환 |
| `wallet_addEthereumChain` | 필요한 경우 chain 추가 |
| `eth_requestAccounts` | account 연결 |
| `personal_sign` | Clairveil root message 서명 |
| `eth_estimateGas` | MetaMask confirm 전에 gas estimate |
| `eth_sendTransaction` | public send / privacy precompile tx 전송 |
| `eth_getTransactionReceipt` | tx receipt wait |

## 체인 추가 방법

### Static/public DApp

Static chain profile은 `public/dapp-config.json`에 추가합니다. 브라우저는
`/api/health`가 없거나 unreachable일 때만 이 same-origin artifact를 fetch하며,
static 배포의 production gate도 `CLAIRVEIL_WEBAPP_CONFIG_URL`이 정확히
`/dapp-config.json`을 가리키도록 요구합니다. JavaScript로 다른 runtime profile을
주입하면 안 됩니다. 현재 commit된 static 기본값은 Cosmos/Keplr profile만 노출합니다.
Local server mode에서는 서버가 `/api/config`로 같은 형태의 profile을 내려줄 수도 있습니다.

아래는 profile 필드 관계를 보여주는 의사 코드입니다. 실제 배포 파일은
`dapp-config.json`의 유효한 JSON이어야 하며, checked-in artifact의 전체
`keplrChainInfo` 구조를 유지해야 합니다.

Cosmos 필드 의사 코드:

```js
const myCosmosProfile = {
  id: "my-cosmos",
  label: "My Cosmos Privacy Chain",
  chainName: "My Cosmos Privacy Chain",
  transport: "cosmos",
  wallet: "keplr",
  chainId: "my-chain-1",
  rpc: "https://rpc.example.com",
  rest: "https://rest.example.com",
  proverUrl: "https://prover.example.com",
  depositProofUrl: "https://deposit-proof.example.com/v1/prove",
  accountPrefix: "my",
  shieldedPrefix: "mys",
  denom: "umy",
  displayDenom: "MY",
  coinDecimals: 18,
  keplrCoinType: 118,
  gasPriceStep: { low: 1, average: 1, high: 1 }
};
myCosmosProfile.keplrChainInfo = keplrChainInfo({
  chainId: myCosmosProfile.chainId,
  chainName: myCosmosProfile.chainName,
  rpc: myCosmosProfile.rpc,
  rest: myCosmosProfile.rest,
  accountPrefix: myCosmosProfile.accountPrefix,
  displayDenom: myCosmosProfile.displayDenom,
  denom: myCosmosProfile.denom,
  coinDecimals: myCosmosProfile.coinDecimals,
  gasPriceStep: myCosmosProfile.gasPriceStep
});
```

EVM 필드 의사 코드:

```js
const myEvmProfile = {
  id: "my-evm",
  label: "My EVM Privacy Chain",
  chainName: "My EVM Privacy Chain",
  transport: "evm",
  wallet: "metamask",
  chainId: "my-evm-host-1",
  rpc: "https://cosmos-rpc.example.com",
  rest: "https://cosmos-rest.example.com",
  proverUrl: "https://prover.example.com",
  accountPrefix: "my",
  shieldedPrefix: "mys",
  denom: "umy",
  displayDenom: "MY",
  coinDecimals: 18,
  evmRpc: "https://evm-rpc.example.com",
  evmChainId: "0x1234",
  evmChainName: "My EVM Privacy Chain",
  evmPrivacyPrecompileAddress: "0x100000000000000000000000000000000000000b",
  evmGasLimit: "0x989680",
  evmSendGasLimit: "0x5208"
};
```

Config 형태 의사 코드:

```js
export const defaultDappConfig = {
  // ...
  activeChainProfileId: myCosmosProfile.id,
  chainProfiles: [clairveilProfile, myCosmosProfile, myEvmProfile]
};
```

### Local server-backed DApp

로컬 테스트 서버를 쓰는 경우 `server.js`의 `dappChainProfiles()`가 `/api/config`로 profile을 내려줍니다. 환경 변수로 기본 profile 값을 바꿀 수 있습니다.

`server.js`에는 로컬 기본값이 들어 있어서 env 파일 없이도 `npm start`가 동작합니다. 로컬 node, prover, chain profile, LAN helper 설정을 바꾸고 싶을 때는 `.env.example`을 기준으로 `.env`를 만들어 shell env로 로드하세요.

```bash
cd examples/clairveil-dapp
cp .env.example .env
set -a; source .env; set +a
npm start
```

주요 환경 변수:

| 변수 | 용도 |
| --- | --- |
| `CLAIRVEIL_DAPP_HOST` / `CLAIRVEIL_DAPP_PORT` | DApp server bind 주소와 포트 |
| `CLAIRVEIL_DAPP_LOCAL_TEST_MODE` | `1`이면 local helper 활성화, `0`이면 public node mode |
| `CLAIRVEIL_HOME` / `CHAIN_ID` / `CLAIRVEILD_BIN` | local node home, chain id, CLI binary |
| `CLAIRVEIL_RPC` | 서버가 붙는 Cosmos/CometBFT RPC |
| `CLAIRVEIL_REST` | 서버가 붙는 Cosmos REST |
| `CLAIRVEIL_PUBLIC_RPC` | 브라우저/Keplr에 노출할 RPC |
| `CLAIRVEIL_PUBLIC_REST` | 브라우저/Keplr에 노출할 REST |
| `CLAIRVEIL_PUBLIC_REST_ENDPOINTS` | 선택적 comma-separated browser REST failover endpoint입니다. Primary public REST URL은 자동으로 포함되며, 모든 endpoint는 배포 origin의 CORS를 허용해야 합니다. |
| `CLAIRVEIL_COSMOS_REST_ENDPOINTS` / `CLAIRVEIL_EVM_HOST_REST_ENDPOINTS` | 선택적 profile별 REST failover override입니다. 활성 profile에서는 generic 목록을 대체합니다. |
| `CLAIRVEIL_PROVER_URL` | prover URL |
| `CLAIRVEIL_PUBLIC_PROVER_URL` | 브라우저에 노출할 prover URL |
| `CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL` | 정확히 검토된 browser-visible `POST` deposit proof-service URL이며, 없으면 public mode에서 Deposit을 비활성화 |
| `CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL` / `CLAIRVEIL_EVM_DEPOSIT_PROOF_URL` | Optional per-profile deposit proof-service override |
| `CLAIRVEIL_DAPP_ENABLE_PROVER_PROXY` | same-origin transfer/withdraw와 `/v1/proofs/batch-transfer` proxy route의 명시적 local-test opt-in입니다. direct loopback request에만 제공되고 public mode에서는 완전히 거부됩니다. Production에는 별도 hardening된 gateway를 사용하세요. |
| `CLAIRVEIL_DAPP_ENABLE_BATCH_TRANSFER` | Cosmos 전용 experimental atomic batch UI와 ClairveilJS gate를 활성화합니다. `make dapp-local`은 활성화하지만 배포 환경은 batch conformance/localnet 검증 전까지 끕니다. |
| `CLAIRVEIL_DAPP_PUBLIC_ORIGIN` | `CLAIRVEIL_DAPP_LOCAL_TEST_MODE=0`일 때 필요한 HTTPS public WebApp origin입니다. |
| `CLAIRVEIL_DENOM` / `CLAIRVEIL_DISPLAY_DENOM` / `CLAIRVEIL_COIN_DECIMALS` | coin metadata |
| `CLAIRVEIL_ACCOUNT_PREFIX` | transparent account prefix |
| `CLAIRVEIL_SHIELDED_PREFIX` | shielded address prefix |
| `CLAIRVEIL_EVM_RPC` | MetaMask/EVM JSON-RPC |
| `CLAIRVEIL_EVM_CHAIN_ID` | MetaMask chain id, hex/decimal 가능 |
| `CLAIRVEIL_EVM_PRIVACY_PRECOMPILE` | EVM privacy precompile address |
| `CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING` / `CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN` | local signing/admin helper를 LAN에 노출할 때만 명시적으로 `1` |

## 호환 조건

### Cosmos 호환

Cosmos profile은 아래 조건을 만족해야 합니다.

- Clairveil privacy module이 chain에 포함되어 있어야 합니다.
- REST query가 `/clairveil/privacy/v1/*` 경로를 제공합니다.
- tx type이 `/clairveil.privacy.v1.MsgDeposit`, `/clairveil.privacy.v1.MsgTransfer`, `/clairveil.privacy.v1.MsgBatchTransfer`, `/clairveil.privacy.v1.MsgWithdraw`와 호환됩니다.
- account prefix, shielded prefix, denom, decimals가 profile과 일치해야 합니다.
- Keplr `signDirect`로 protobuf sign doc을 서명할 수 있어야 합니다.
- 브라우저에서 REST/RPC/prover에 접근할 수 있도록 CORS가 열려 있어야 합니다.

### EVM 호환

EVM profile은 아래 조건을 만족해야 합니다.

- EVM JSON-RPC가 MetaMask에서 접근 가능해야 합니다.
- Host chain 쪽 Cosmos REST/RPC가 Clairveil privacy event/query surface를 제공해야 합니다.
- proof를 포함한 deposit, self-view disclosure와 absolute expiry를 포함한 transfer, legacy withdraw output-note field가 없는 Clairveil 0.2 canonical `IPrivacy` precompile ABI와 호환되어야 합니다.
- Profile의 `evmPrivacyPrecompileAddress`가 target chain이 공개한 fixed precompile address와 일치해야 합니다.
- EVM-derived identity material에 사용할 Clairveil privacy account prefix가 chain과 일치해야 합니다.
- 필수 `DepositCircuit` proof용 exact reviewed `depositProofUrl`(또는 local-only test helper)과 `/v1/prover/transfer`, `/v1/prover/withdraw` prover contract가 필요합니다.

현재 DApp은 임의 또는 legacy EVM privacy ABI shape를 지원하지 않습니다. EVM Clairveil 지원 chain은 같은 0.2 privacy precompile ABI와 payload semantics를 사용해야 하며 dummy withdraw output을 채우거나 필수 proof, self-view disclosure, expiry를 조용히 버리지 않습니다.

## Privacy flow

### Setup Clairveil

1. Wallet address와 transparent pubkey를 준비합니다.
2. Clairveil root message를 만듭니다.
3. Keplr `signArbitrary` 또는 MetaMask `personal_sign`으로 root message를 서명합니다.
4. ClairveilJS가 root signature에서 wallet privacy material을 파생합니다.
5. shielded address와 disclosure pubkey가 계산됩니다.

Wallet connect, chain switch, root setup, session signing은 active privacy
session과 선택한 chain profile에 묶입니다. Extension prompt가 열린 동안 wallet
account/network 또는 선택 profile이 바뀌면 DApp은 이전 결과를 버리고 다시
connect하도록 합니다. 현재 탭의 in-memory identity state만 지우며, 이전 wallet
namespace의 encrypted reservation evidence는 recovery를 위해 유지합니다.

### Deposit

1. DApp은 amount만 ClairveilJS에 넘깁니다.
2. ClairveilJS가 note, commitment, encrypted note를 만듭니다.
3. Cosmos면 `MsgDeposit` sign doc을 만들고 Keplr가 서명합니다.
4. EVM이면 privacy precompile calldata를 만들고 MetaMask가 tx를 보냅니다.

### Transfer

1. ClairveilJS가 events를 scan해서 spendable notes를 찾습니다.
2. 요청 금액에 맞는 note planning을 수행합니다.
3. 필요한 경우 self transaction step을 반환합니다.
4. Final transfer 단계에서 prover payload를 만들고 prover에 proof를 요청합니다.
5. Disclosure mode에 따라 user disclosure payload를 만듭니다.
6. Cosmos sign doc 또는 EVM precompile tx를 준비합니다.

### Batch Transfer

일반 Transfer의 `Add recipient`에서 진입하며 Cosmos batch feature gate가
꺼져 있으면 메뉴 자체를 숨깁니다. Recipient별 amount/address/disclosure 행을
편집하고 total, estimated change, input/output capacity를 preview합니다.
`prepareTransferBatch(...)`가 proof 하나와 `MsgBatchTransfer` 하나를 준비하며
batch 내부 payment는 전체 성공 또는 전체 실패입니다. Capacity 초과를 자동으로
나누지 않고 사용자가 여러 독립 원자 batch를 명시적으로 선택하게 하며, 후속
batch 실패 시 앞 batch는 이미 commit되어 있다는 점을 표시합니다. SDK의 fresh
authoritative scan이 proof를 준비한 뒤 실제 total, change, input/output 수와 각
전체 recipient, disclosure mode, recipient-encrypted target fingerprint가 요청
행과 일치하는지 최종 확인하고 Keplr를 엽니다. 현재 16-input capacity보다 큰
단일 payment는 split 승인 전에 차단합니다. Split batch 하나가 검증되면 완료된
행을 잠그고 이후 retry 대상에서 제외해 후속 취소나 실패가 같은 payment를 다시
제출하지 못하게 합니다.
Payload/proof checkpoint는 AES-GCM으로 암호화하고 이전 checkpoint가 미해결이면
새 batch를 막습니다. Broadcast 뒤 결과가 불명확하면 실패가 아니라 reconcile
대기로 표시합니다. 포함 후에는 typed output evidence와 모든 input nullifier가
reconcile된 payment만 성공으로 표시합니다. 암호화된 checkpoint를 만든 뒤
Keplr를 열기 전에 proof 준비가 중단되면 batch transaction은 제출되지 않습니다.
이때 DApp은 해당 reservation review를 바로 열어 모든 input nullifier를 다시
확인한 뒤 그 로컬 checkpoint만 폐기하고 batch를 다시 준비할 수 있게 합니다.
이 pre-wallet 실패를 모호한 chain 제출로 표시하지 않습니다.

### Withdraw

1. ClairveilJS가 spendable notes를 scan합니다.
2. withdraw 가능한 note를 planning합니다.
3. 필요하면 helper/self transaction step을 안내합니다.
4. prover payload와 proof를 준비합니다.
5. Cosmos `MsgWithdraw` 또는 EVM precompile tx를 준비합니다.

### Relay withdraw

Relay withdraw는 ClairveilJS `prepareRelayWithdraw(...)`로 relayer에게 넘길 proof-backed withdraw payload를 만듭니다. Static deployment에서도 browser가 이 payload를 준비하고 Copy할 수 있으며 `Relay` button은 optional loopback local-helper submission adapter일 뿐입니다. Local button은 외부 relay handoff를 기록하지 않습니다. Current chain/nullifier 확인 뒤 정확히 한 번의 same-origin request 직전에 `BroadcastAttempting`을 durable하게 기록합니다. 이렇게 하면 request 결과가 모호할 때 reservation을 복구할 수 있고, 외부 handoff state가 된 payload를 다시 local submit하지 못하도록 한 safety rule도 지킵니다. Copy, download, QR, upload 등 실제 payload egress 전에는 `recordRelayHandoff`을 기록하고 expiry-bound recovery lock을 유지합니다. Payload/proof JSON은 민감하므로 메모리에만 두고, refresh persistence에는 payload hash, reservation id/status, expiry 같은 reconcile metadata만 저장합니다. relay reservation metadata에도 원문 amount나 recipient을 남기지 않습니다. 따라서 페이지를 새로고침해도 payload/proof JSON, amount, recipient은 복구되지 않습니다. 시작 시에는 IndexedDB의 active reservation에서 pending relay 항목을 다시 만들므로 탭을 닫거나 handoff가 5개를 넘어도 expiry/reconcile 정보가 사라지지 않습니다. 이미 payload를 복사했거나 relayer에게 넘겼다면 expiry 전까지 relayer가 제출할 수 있습니다. `ProofReady` reservation은 단순 TTL 만료나 일반 UI cancel로 풀면 안 되며, handoff 전이면 DApp이 reservation을 replan하고, handoff 뒤에는 pending payload를 relay/refresh/reconcile 흐름으로 처리해야 합니다. Pending handoff의 `Check recovery`는 cancel button이 아니라 검증 action입니다. Authoritative expiry, input nullifier, broadcast evidence를 모두 확인한 경우에만 만료 handoff를 해제합니다. Query 가능한 broadcast identity와 durable no-broadcast evidence가 모두 없으면 DApp은 잠금을 유지하고, 조용히 아무 일도 하지 않는 대신 그 이유를 안내합니다.

Non-relay `ManualReview` reservation에 sign-doc identity만 있고 query 가능한 submitted/signed-transaction identity가 없으면 `검토`는 wallet request가 prompt 전에 멈췄을 수 있음, 사용자가 거절했을 수 있음, 승인 뒤 result가 유실됐을 수 있음을 설명합니다. `예약 취소 및 다시 준비`는 사용자가 wallet activity와 explorer에서 제출이 없음을 acknowledgement한 뒤에만 활성화됩니다. 모든 input nullifier를 다시 확인한 뒤 local reservation만 `ReplanRequired`로 전이하며, on-chain transaction을 취소하거나 note를 직접 spendable로 만들지 않습니다. 이 action은 relay payload와 handoff에는 의도적으로 제공하지 않습니다.

DApp은 note, scan cursor, reservation, relay recovery metadata를 namespace별 AES-GCM ciphertext로만 IndexedDB에 저장합니다. Key는 in-memory root signature에서 Web Crypto로 derive하며 non-extractable이고 persist하지 않습니다. Reservation write에는 Web Locks가 필요합니다. IndexedDB, Web Crypto, Web Locks 중 하나라도 없으면 privacy setup은 fail closed하며 `localStorage`, plaintext IndexedDB, memory fallback은 없습니다. Raw relay payload, proof, amount, recipient는 memory에만 둡니다.

ClairveilJS 0.2.0은 fresh privacy-note-v1 persistence boundary입니다. 이 예제는 note cache, IndexedDB reservation, relay recovery metadata를 별도 `v2` namespace에 저장하므로 0.1 note, lease, prepared relay snapshot을 재사용하지 않습니다. Upgrade 뒤 기존 사용자는 빈 cache에서 전체 안전 재스캔을 수행하며 compatibility decode path는 없습니다. 이 예제는 legacy browser record를 읽거나 변경하지 않습니다.

## Disclosure mode

| Mode | 설명 |
| --- | --- |
| `none` | user disclosure를 만들지 않습니다. Explorer/이벤트에서는 private payload만 보입니다. |
| `public` | 허용한 field를 public report로 이벤트에 남깁니다. 대상 pubkey가 필요 없습니다. |
| `recipient-encrypted` | 특정 disclosure pubkey 소유자만 허용 field를 decode할 수 있게 암호화합니다. |

### 내 disclosure pubkey로 테스트하는 흐름

1. Wallet을 연결하고 `Setup Clairveil`을 눌러 shielded address와 disclosure pubkey를 만듭니다.
2. Wallet Session 카드의 `Disclosure pubkey` 옆 `Copy` 버튼을 눌러 내 disclosure pubkey를 복사합니다.
3. `Transfer (Veiled Send)`에서 `Advanced`를 켭니다.
4. `Disclosure mode`를 `recipient-encrypted`로 선택합니다.
5. `Disclosure target`에 방금 복사한 내 disclosure pubkey를 붙여넣습니다.
6. 공개를 허용할 항목, 예를 들어 `Amount + asset`, `From shielded address`, `To shielded address`를 체크합니다.
7. transfer를 보낸 뒤 `Privacy Events`에서 해당 `shielded_transfer`를 선택합니다.
8. event detail의 `조회`를 누르면 내 wallet root signature에서 파생된 privacy material로 user disclosure가 decode됩니다.

다른 사람에게 보여주고 싶으면 그 사람의 disclosure pubkey를 받아서 `Disclosure target`에 넣어야 합니다. 주소만으로는 recipient-encrypted disclosure를 대신 만들 수 없습니다.

감사자용 audit disclosure는 chain `audit_config`의 audit master pubkey 대상으로 별도 생성됩니다. 이 DApp의 audit scalar 입력은 local/admin test 전용입니다.

## 실행

노드, prover, DApp을 한 번에 띄워 로컬에서 테스트:

```bash
# repository root에서 실행:
make dapp-local
```

이미 `26657`, `1317`, `8080`, `5173` 포트를 쓰고 있다면 기존 프로세스를 먼저 종료한 뒤 실행하세요. 종료는 이 터미널에서 `Ctrl+C`를 누르면 됩니다.

로컬 Clairveil node:

```bash
# repository root에서 실행:
export CLAIRVEIL_HOME=/tmp/clairveil-dapp-local
export CHAIN_ID=clairveil-local-2
make init
source "$CLAIRVEIL_HOME/clairveil.env"
clairveild start \
  --home "$CLAIRVEIL_HOME" \
  --minimum-gas-prices 0uclair \
  --api.enable \
  --api.address tcp://127.0.0.1:1317
```

DApp:

```bash
cd examples/clairveil-dapp
npm ci
CLAIRVEIL_HOME=/tmp/clairveil-dapp-local CHAIN_ID=clairveil-local-2 npm start -- --host 0.0.0.0
```

브라우저:

```text
http://127.0.0.1:5173
```

같은 네트워크의 다른 기기:

```text
http://192.168.0.10:5173
```

다른 기기에서 wallet까지 테스트하려면 RPC/REST/prover URL도 그 기기에서 접근 가능한 주소여야 합니다. `0.0.0.0`으로 DApp을 열어도 local signer/admin LAN 접근은 `CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1` 또는 `CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN=1`이 필요합니다. Same-origin prover proxy는 `CLAIRVEIL_DAPP_ENABLE_PROVER_PROXY=1`이 아니면 비활성화되고, 설정해도 direct loopback request에만 제공합니다.

## Public node mode

Public/open node에 붙일 때:

```bash
CLAIRVEIL_DAPP_LOCAL_TEST_MODE=0 \
CLAIRVEIL_DAPP_PUBLIC_ORIGIN=https://dapp.example \
CLAIRVEIL_RPC=https://rpc.example \
CLAIRVEIL_REST=https://rest.example \
CLAIRVEIL_PUBLIC_REST_ENDPOINTS=https://rest.example,https://rest-backup.example \
CLAIRVEIL_PROVER_URL=https://prover.example \
npm start
```

이 모드에서는 local signer, faucet, auditor test scalar/decode, local CLI deposit route, same-origin prover proxy가 비활성화됩니다. Prover proxy, server-held prover bearer token, 없거나 HTTPS가 아닌 public origin, HTTPS가 아닌 profile endpoint가 설정되면 server는 public-mode 시작을 거부합니다. Wallet-driven send, transfer, withdraw, scan, decode는 브라우저 ClairveilJS가 계속 처리합니다. Deposit은 production `DepositCircuit` proof provider가 필요합니다. Active profile에 정확히 검토된 HTTPS `depositProofUrl`을 설정하거나 이 server-backed 예제에서는 `CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL`을 설정하세요. Loopback `/api/deposit/proof` helper는 local-test 전용입니다.

## 최소 SDK 흐름

최소 SDK 사용 흐름은 DApp example이 아니라 ClairveilJS package의 `examples/minimal-keplr-flow.js`, `examples/minimal-metamask-flow.js`에 있습니다. DApp은 UI/chain profile/wallet flow 예제이고, SDK API 자체의 작은 사용 예제는 SDK repo가 소유합니다.

```text
connect wallet
-> derive privacy material
-> prepare deposit
-> scan notes
-> prepare transfer
-> wallet sign/broadcast
```

## 테스트

```bash
npm run check:dapp
npm run test:dapp
npm run check:bundle:fresh
npm run check:clairveiljs
npm run test:clairveiljs
npm run check:clairveiljs:types
```

Public deployment에서는 manual wallet-extension flow 전에 최종 HTTPS WebApp origin으로 `npm run verify:production-deployment`를 실행합니다. Static deployment의 `CLAIRVEIL_WEBAPP_CONFIG_URL`은 browser가 읽는 `https://<origin>/dapp-config.json`이어야 하며, server-backed deployment는 same-origin server config를 사용합니다. 정확한 environment variable과 release checklist는 [deployment guide](../../docs/clairveil-web-app-deployment-kr.md)를 참고합니다.

Smoke test는 다음 boundary를 확인합니다.

- DApp이 `clairveiljs/browser-dapp` high-level API를 사용합니다.
- DApp/server가 low-level planner/prover payload builder를 직접 호출하지 않습니다.
- Server privacy preparation route가 없습니다.
- Local helper route는 local mode에서만 활성화됩니다.
