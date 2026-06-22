# Clairveil DApp

Clairveil DApp은 브라우저에서 Keplr 또는 MetaMask를 연결해 Clairveil privacy 기능을 테스트하는 예제 웹앱입니다.

이 예제의 핵심 경계는 단순합니다.

- **DApp**: 입력값과 UI 흐름만 관리하고, ClairveilJS high-level API를 호출합니다.
- **ClairveilJS**: note 생성, commitment/encrypted note, scan, note planning, prover payload, disclosure encode/decode, deposit/transfer/withdraw 준비를 처리합니다.
- **Optional local server**: 로컬 테스트용 static server, faucet, local signer, auditor/admin helper만 제공합니다.

즉 일반적인 public node 환경에서는 DApp + ClairveilJS + 공개 RPC/REST + prover URL만 있으면 됩니다. 백엔드 서버는 필수가 아닙니다.

## 파일 구조

| 파일 | 역할 |
| --- | --- |
| `public/dapp-config.js` | 브라우저가 읽는 static chain profile 목록 |
| `public/app.js` | DApp UI와 wallet event 흐름 |
| `public/app.bundle.js` | `npm run build:dapp`로 생성되는 브라우저 번들 |
| `server.js` | local test helper 서버 (faucet, local signer...) |
| `.env.example` | 로컬/server-backed 실행에서 사용할 수 있는 환경 변수 override 템플릿 |
| `test/dapp-smoke.test.js` | DApp 구조와 privacy boundary smoke test |

Standalone SDK는 이 repo 안에 복사하지 않습니다. 현재 package dependency는 `github:DELIGHT-LABS/clairveiljs`를 가리키며, SDK를 로컬에서 같이 개발할 때만 임시로 `file:` dependency로 바꿀 수 있습니다. 최소 SDK 사용 흐름 예제는 ClairveilJS 쪽 `examples/minimal-keplr-flow.js`, `examples/minimal-metamask-flow.js`에 둡니다.

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
  - Withdraw, 즉 veiled balance를 transparent account로 이동
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
  - alice/bob/auditor local signer
  - local CLI deposit/note scan
  - auditor test scalar/decode

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

이 엔드포인트들은 `server.js`가 켜져 있을 때만 있습니다. Public node 환경에서는 local helper 기능 없이 static DApp + ClairveilJS만으로 wallet privacy flow를 수행할 수 있습니다.

| Endpoint | Mode | 용도 |
| --- | --- | --- |
| `GET /api/config` | all | server-backed config와 chain profile 전달 |
| `GET /api/health` | all | local node 상태, tree/audit config, local accounts 확인 |
| `POST /api/local-signers/ensure` | local only | alice/bob/auditor 등 local signer 생성 |
| `GET /api/wallet/:name/show-address` | local only | local signer의 transparent/shielded 주소 조회 |
| `GET /api/wallet/:name/notes` | local only | local signer note scan |
| `POST /api/faucet` | local only | alice/dev account에서 연결된 wallet으로 faucet 송금 |
| `POST /api/deposit` | local only | local CLI signer deposit 테스트 |
| `GET /api/auditor/test-scalar` | local/admin only | 테스트 auditor scalar 조회 |
| `POST /api/auditor/decode` | local/admin only | audit disclosure private scalar로 disclosure decode |

### Browser ClairveilJS high-level calls

DApp UI는 privacy 준비 로직을 직접 구현하지 않고 `clairveiljs/browser-dapp`의 high-level API를 호출합니다. 아래 call들이 선택된 chain profile의 REST/RPC/prover/wallet API를 사용합니다.

| ClairveilJS call | 사용하는 네트워크/API |
| --- | --- |
| `health()` | RPC `/status`, REST `/tree_state`, REST `/audit_config` |
| `getBalances(address)` | REST `/cosmos/bank/v1beta1/balances/{address}` |
| `buildBankSendSignDoc(...)` | REST account info, Keplr `signDirect` |
| `evmNativeSendTransaction(...)` | MetaMask `eth_sendTransaction` |
| `prepareDeposit(...)` | note/commitment/encrypted note 생성, Cosmos sign doc 또는 EVM precompile tx 생성 |
| `scanWalletNotes(...)` | privacy events/commitments/nullifiers 조회 후 wallet root seed로 note scan |
| `prepareTransfer(...)` | note scan, planner, audit config, prover `/v1/prover/transfer`, disclosure payload, Cosmos sign doc 또는 EVM precompile tx 생성 |
| `prepareWithdraw(...)` | note scan, planner, prover `/v1/prover/withdraw`, Cosmos sign doc 또는 EVM precompile tx 생성 |
| `broadcastSignedTx(...)` | Cosmos signed tx broadcast/wait |
| `waitForEvmTransaction(...)` | EVM receipt wait |
| `fetchPrivacyEvents(...)` | REST privacy event feed 조회 |
| `fetchBlockEvents(...)` | RPC tx search 기반 block/event 조회 |
| `fetchAuditableTransfers(...)` | REST privacy events 중 audit 가능한 transfer 목록 조회 |
| `decodeUserDisclosure(...)` | tx/event disclosure payload 조회 후 wallet privacy material로 decode |

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

Deposit은 새 note commitment/encrypted note 생성만 필요하고 ZK proof는 필요하지 않습니다.

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

기본 static chain profile은 `public/dapp-config.js`의 `chainProfiles`에 추가합니다. 이 파일은 서버 사용 여부를 결정하지 않고, 브라우저 DApp이 사용할 수 있는 chain 목록을 제공합니다. 현재 commit된 static 기본값은 Cosmos/Keplr profile만 노출합니다. Static 배포에서 EVM/MetaMask를 보이게 하려면 `chainProfiles`에 EVM profile을 추가하거나 `globalThis.CLAIRVEIL_DAPP_CONFIG`로 주입하세요. Local server mode에서는 서버가 `/api/config`로 같은 형태의 profile을 내려줄 수도 있습니다.

Cosmos 예시:

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

EVM 예시:

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

그 다음:

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
| `CLAIRVEIL_PROVER_URL` | prover URL |
| `CLAIRVEIL_PUBLIC_PROVER_URL` | 브라우저에 노출할 prover URL |
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
- tx type이 `/clairveil.privacy.v1.MsgDeposit`, `/clairveil.privacy.v1.MsgTransfer`, `/clairveil.privacy.v1.MsgWithdraw`와 호환됩니다.
- account prefix, shielded prefix, denom, decimals가 profile과 일치해야 합니다.
- Keplr `signDirect`로 protobuf sign doc을 서명할 수 있어야 합니다.
- 브라우저에서 REST/RPC/prover에 접근할 수 있도록 CORS가 열려 있어야 합니다.

### EVM 호환

EVM profile은 아래 조건을 만족해야 합니다.

- EVM JSON-RPC가 MetaMask에서 접근 가능해야 합니다.
- Host chain 쪽 Cosmos REST/RPC가 Clairveil privacy event/query surface를 제공해야 합니다.
- EVM privacy precompile ABI가 ClairveilJS의 `IPrivacy` ABI와 호환되어야 합니다.
- Profile의 `evmPrivacyPrecompileAddress`가 target chain이 공개한 fixed precompile address와 일치해야 합니다.
- EVM-derived identity material에 사용할 Clairveil privacy account prefix가 chain과 일치해야 합니다.
- Prover가 `/v1/prover/transfer`, `/v1/prover/withdraw` contract를 지원해야 합니다.

현재 DApp은 임의의 EVM privacy ABI shape를 지원하지 않습니다. EVM Clairveil 지원 chain은 같은 privacy precompile ABI와 payload semantics를 사용해야 합니다.

## Privacy flow

### Setup Clairveil

1. Wallet address와 transparent pubkey를 준비합니다.
2. Clairveil root message를 만듭니다.
3. Keplr `signArbitrary` 또는 MetaMask `personal_sign`으로 root message를 서명합니다.
4. ClairveilJS가 root signature에서 wallet privacy material을 파생합니다.
5. shielded address와 disclosure pubkey가 계산됩니다.

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

### Withdraw

1. ClairveilJS가 spendable notes를 scan합니다.
2. withdraw 가능한 note를 planning합니다.
3. 필요하면 helper/self transaction step을 안내합니다.
4. prover payload와 proof를 준비합니다.
5. Cosmos `MsgWithdraw` 또는 EVM precompile tx를 준비합니다.

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
npm install
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

다른 기기에서 wallet까지 테스트하려면 RPC/REST/prover URL도 그 기기에서 접근 가능한 주소여야 합니다.

## Public node mode

Public/open node에 붙일 때:

```bash
CLAIRVEIL_DAPP_LOCAL_TEST_MODE=0 \
CLAIRVEIL_RPC=https://rpc.example \
CLAIRVEIL_REST=https://rest.example \
CLAIRVEIL_PROVER_URL=https://prover.example \
npm start
```

이 모드에서는 local signer, faucet, auditor test scalar/decode, local CLI deposit route가 비활성화됩니다. Wallet-driven send/deposit/transfer/withdraw/scan/decode는 브라우저 ClairveilJS가 계속 처리합니다.

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
npm run check:clairveiljs
npm run test:clairveiljs
npm run check:clairveiljs:types
```

Smoke test는 다음 boundary를 확인합니다.

- DApp이 `clairveiljs/browser-dapp` high-level API를 사용합니다.
- DApp/server가 low-level planner/prover payload builder를 직접 호출하지 않습니다.
- Server privacy preparation route가 없습니다.
- Local helper route는 local mode에서만 활성화됩니다.
