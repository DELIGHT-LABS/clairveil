# Clairveil 시작 가이드

> English version: [clairveil-getting-started.md](clairveil-getting-started.md)

이 문서는 현재 checkout의 전제조건, 초기화, 설정, 초기 장애 대응 기준입니다. Clairveil은 `PUBLICATION_READY_EXPERIMENTAL` 상태이며, 아래 절차는 development chain과 development Groth16 artifact를 만듭니다. Production 배포나 trusted setup ceremony가 아닙니다.

## 1. 전제조건

기본 repository workflow에 필요한 도구:

| 도구 | 기준 | 사용처 |
| --- | --- | --- |
| Go | `1.25.12` | build, test, binary, circuit setup |
| Python | `3.9+` | init/release script와 JSON 검증 |
| Bash | `/bin/bash` | Make target과 script |
| Git | Repository가 최소 version을 고정하지 않음 | clone, exact-ref 문서, release manifest |
| Make | Repository가 최소 version을 고정하지 않음 | repository build, test, init, release target |
| Node.js/npm | Node.js `22+` | `make examples`, `make ci` |

선택 도구는 작업별로 필요합니다. 외부 DSN 없이 PostgreSQL reservation integration을 실행할 때는 Docker, live batch localnet gate에는 `grpcurl`, proto output 재생성에만 `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`, `buf`, `clang-format`이 필요합니다.

기본 repository workflow에는 third-party Python package가 필요하지 않습니다. `make docs-check`는 canonical prover HTTP schema와 fixture의 full Draft 2020-12 검증에 Go toolchain을 사용합니다.

초기화 전에 주요 버전을 확인합니다.

```bash
git --version
make --version
go version
python3 --version
bash --version
node --version
npm --version
```

Script는 Unix-like command environment를 가정합니다. Platform 지원 범위는 repository CI와 downstream project가 입증해야 하며, 이 문서는 검증하지 않은 OS 지원을 주장하지 않습니다.

## 2. 리소스 계획

`make init`은 active development circuit artifact 전체를 생성합니다. 기록된 full-shape batch run의 peak RSS는 약 `3,339,862,016`~`3,354,689,536` bytes였고 batch R1CS와 PK는 각각 `122,813,535`, `209,218,621` bytes입니다. 이 값은 hard limit가 아니라 reference measurement입니다. 사용 가능한 memory 4 GiB 초과, free disk 1 GiB 이상을 계획하고 Go build cache와 local chain data를 위한 여유를 추가하세요.

Validator는 exact consensus identity 비교 후 required VK만 필요합니다. Prover는 선택한 R1CS/PK pair도 필요하므로 storage와 memory 경계가 더 큽니다.

## 3. 초기화와 시작

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
export CLAIRVEIL_HOME=${CLAIRVEIL_HOME:-"$HOME/.clairveil"}
export CHAIN_ID=${CHAIN_ID:-clairveil-local-1}
make init
source "$CLAIRVEIL_HOME/clairveil.env"
clairveild --home "$CLAIRVEIL_HOME" start
```

`make init`은 전체 binary를 빌드하고 project binary 여섯 개(`clairveild`, `clairveil-setup`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`, `clairveil-verify`)를 `GOBIN` 또는 `$(go env GOPATH)/bin`에 설치한 뒤 아래를 수행합니다. 설치되는 `clairveil-verify`는 legacy-only debugging helper이며 초기화와 현행 note 검증에서는 사용하지 않습니다.

1. 기존 `~/.clairveil`을 timestamp backup으로 이동합니다.
2. `privacy-note-v1` development artifact set을 생성합니다.
3. `alice`, `bob`, `relayer`, `auditor` development key를 만듭니다.
4. Genesis를 초기화하고 계정 funding과 validator gentx를 수행합니다.
5. Auditor disclosure public key를 genesis에 기록합니다.
6. Export된 runtime environment를 `~/.clairveil/clairveil.env`에 씁니다.

생성된 `init-out/*-key.json`에는 development key material이 들어 있습니다. Home 권한을 제한하고, 이 key를 재사용하거나 production 환경으로 복사하지 마세요.

활성화된 기본 listener는 RPC `26657`, P2P `26656`, gRPC `9090`입니다. Generated `app.toml`은 REST address를 `tcp://localhost:1317`로 설정하지만 `[api] enable = false`이므로 기본상태에서는 `1317`을 bind하지 않습니다. `enable = true`를 명시하거나 `--api.enable`로 시작한 경우에만 해당 address를 bind합니다. Reference node를 시작하기 전에 활성 port를 쓰는 다른 local node를 멈추세요. Smoke-test script는 port override를 받지만 일반 `clairveild start`는 generated config 또는 daemon flag로 변경합니다.

## 4. 주요 설정

| 변수 | 기본값 | 범위 |
| --- | --- | --- |
| `GOBIN` | `go env GOBIN`, 이후 `$(go env GOPATH)/bin` | Binary 설치 |
| `CLAIRVEIL_HOME` | `~/.clairveil` | `make init` home과 backup 위치 |
| `CHAIN_ID` | `clairveil-local-1` | Init/smoke-test chain ID |
| `NODE_NAME` | `local` | Init/smoke-test node moniker |
| `KEYRING_BACKEND` | `test` | Development init keyring |
| `CLAIRVEIL_INIT_ACCOUNTS` | `alice bob relayer auditor` | 공백 구분 init key |
| `VALIDATOR_KEY` / `AUDITOR_KEY` | `alice` / `auditor` | 필수 역할이며 account list에 포함돼야 함 |
| `FUND_AMOUNT` / `STAKE_AMOUNT` | Script 기본값 | Development genesis balance |
| `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR` | `make init`에서는 `<home>/artifacts/privacy`, unset runtime fallback은 `.` | Artifact output/runtime directory. `clairveil.env`를 source하거나 명시적으로 설정 |
| `CLAIRVEILD_BIN` / `CLAIRVEIL_SETUP_BIN` | 설치된 binary | 명시적 binary override |
| `RPC_PORT`, `P2P_PORT`, `ABCI_PORT`, `GRPC_PORT`, `API_PORT`, `PPROF_PORT` | Script 기본값 | Smoke/localnet script 설정. REST disabled 상태에서 `API_PORT`는 bind하지 않음 |

격리된 초기화 예:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home \
CHAIN_ID=my-local-chain \
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" \
make init
```

`clairveil-setup`이 쓰는 `privacy_zk_checksums.env`는 exported variable이 아니라 shell assignment입니다. `make init` 밖에서 raw file을 source할 때는 directory를 명시하고 모든 assignment를 export합니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/absolute/path/to/artifacts/privacy
set -a
source "$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

`make init`이 만든 `~/.clairveil/clairveil.env`에는 이미 `export`가 포함되어 있어 바로 source할 수 있습니다.

## 5. 첫 검증

실행 중인 node 없이:

```bash
make docs-check
go test ./...
make build
```

활성화된 기본 RPC, P2P, gRPC port에 다른 node가 없을 때:

```bash
make localnet-smoke
make privacy-e2e-smoke
```

더 무거운 batch, payroll, benchmark, release gate는 [clairveil-testing-guide-kr.md](clairveil-testing-guide-kr.md)에서 선택합니다.

## 6. Troubleshooting과 정리

- `clairveild: command not found`: `$(go env GOPATH)/bin` 또는 `go env GOBIN` 값을 `PATH`에 넣고 `make install`을 다시 실행합니다.
- setup이 kill되거나 out-of-memory/no-space를 보고함: 경쟁 workload를 멈추고 disk를 확보하거나 더 큰 host에서 artifact를 생성합니다. Strict preflight를 통과한 complete artifact directory만 재사용합니다.
- artifact checksum/circuit identity mismatch: 오래된 development artifact와 proof job을 제거하고 exact active set을 다시 생성한 뒤 fresh genesis에서 시작합니다. Environment checksum은 consensus identity를 override할 수 없습니다.
- `address already in use`: 기존 node를 멈추거나 smoke script의 관련 port 전체를 바꿉니다. RPC/P2P/gRPC/REST 중 하나만 바꾸면 안 됩니다.
- `privacy_scan`이 `ResourceExhausted`를 반환함: typed record 하나가 `max_encoded_bytes`를 초과했다면 server 최대치 안에서 byte budget을 늘립니다. Output/event limit을 줄여도 단일 record는 분할되지 않습니다. 최대치에도 들어가지 않으면 server/contract incident로 취급합니다. 마지막으로 수락된 cursor를 저장하고 건너뛰지 않습니다.
- `commitment_paths_at_root`가 `ResourceExhausted`를 반환함: historical rebuild가 너무 크면 current root 또는 trusted local historical index를 사용하고, 일시적인 rebuild admission 포화라면 bounded retry합니다. 요청 commitment 수를 줄여도 historical tree의 leaf count는 줄지 않습니다.
- `make release-pack-verify`가 dirty tree를 거부함: 편집 중에는 `make docs-check`를 사용합니다. Clean untagged commit은 commit-bound CI snapshot에만 사용하고, 공개 가능한 release pack은 최종 annotated exact-SemVer tagged commit에서 생성·검증합니다. 또는 out-of-band commit과 함께 explicit archive를 검증합니다.

Disposable home을 지우려면 node를 먼저 멈추고 자신이 명시적으로 선택한 path만 삭제합니다. `make init`은 기존 home을 `<home>.backup-YYYYMMDD-HHMMSS`로 보존합니다. 이 backup에도 private development key와 wallet data가 있을 수 있으므로 직접 검토한 뒤 삭제하세요.

## 7. 첫 Privacy 흐름

3절의 node를 실행한 채 repository root에서 두 번째 Bash terminal을 연다. 두 terminal에서 같은 home과 chain ID를 사용한다. 아래 명령은 기본 account 이름과 새 home을 전제로 한다. Test keyring과 출력 파일에는 development private material이 들어가므로 disposable `CLAIRVEIL_HOME`을 사용한다.

전용 output directory를 만들고 transparent address 네 개를 저장한다. 아래 shell function은 각 transaction evidence를 유지하면서 command를 읽기 쉽게 한다.

```bash
umask 077
REPO_DIR="$PWD"
export CLAIRVEIL_HOME=${CLAIRVEIL_HOME:-"$HOME/.clairveil"}
export CHAIN_ID=${CHAIN_ID:-clairveil-local-1}
export RPC_ADDR=${RPC_ADDR:-tcp://127.0.0.1:26657}
source "$CLAIRVEIL_HOME/clairveil.env"
WORK_DIR=${WORK_DIR:-"$PWD/tmp/clairveil-first-flow"}
mkdir -p -m 700 "$WORK_DIR"
cd "$WORK_DIR"
run_cli() { command clairveild --home "$CLAIRVEIL_HOME" "$@"; }
wait_for_tx() {
  local txhash="$1" response code attempt
  for attempt in {1..60}; do
    if response="$(run_cli query tx "$txhash" --node "$RPC_ADDR" --output json 2>/dev/null)"; then
      code="$(python3 -c 'import json,sys; doc=json.load(sys.stdin); print(doc.get("code", doc.get("tx_response", {}).get("code", 0)))' <<<"$response")"
      [ "$code" = 0 ] && return 0
      printf 'transaction %s was included with code %s\n' "$txhash" "$code" >&2; return 1
    fi
    sleep 2
  done
  printf 'timed out waiting for transaction %s\n' "$txhash" >&2; return 1
}
txhash_from() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["txhash"])' "$1"; }
for name in alice bob relayer auditor; do
  run_cli keys show -a "$name" --keyring-backend test > "${name}-address.txt"
done
```

Alice와 Bob의 shielded address, recipient disclosure key, Alice view key를 저장한다. `clairs1...`는 `clair1...` keyring account에서 유도한 shielded address다.

```bash
run_cli tx privacy show-address --from alice --keyring-backend test --output json > alice-shielded.json
run_cli tx privacy show-address --from bob --keyring-backend test --output json > bob-shielded.json
run_cli tx privacy show-view-key --from alice --keyring-backend test --output json > alice-view-key.json
run_cli tx privacy show-disclosure-pubkey --from bob --keyring-backend test --output json > bob-disclosure.json
python3 - <<'PY'
import json
from pathlib import Path
for source, target, field in [('alice-shielded.json', 'alice-shielded-address.txt', 'address'), ('bob-shielded.json', 'bob-shielded-address.txt', 'address'), ('bob-disclosure.json', 'bob-disclosure.hex', 'public_key_hex')]:
    Path(target).write_text(json.loads(Path(source).read_text())[field] + '\n')
PY
```

Alice에 `11uclair`, `10uclair`, `7uclair`, zero dummy note를 deposit한다. Dummy note는 single-transfer 예제를 pairable하게 만든다. 각 hash가 block에 포함될 때까지 기다린 후 다음 command를 실행한다.

```bash
for amount in 11 10 7 0; do
  run_cli tx privacy deposit "${amount}uclair" --from alice --keyring-backend test \
    --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 2500000 --gas-prices 8500000000uclair --yes --output json > "deposit-${amount}.json"
  wait_for_tx "$(txhash_from "deposit-${amount}.json")"
done
run_cli tx privacy list-notes --from alice --keyring-backend test --node "$RPC_ADDR" --json > alice-notes.json
```

Spendable amount에 `11`, `10`, `7`, `0`이 있는지 확인한다. Note가 없으면 wallet scan이 필요하거나 deposit이 아직 포함되지 않은 경우가 보통이다. 튜토리얼을 계속하려고 다른 note를 선택하면 안 된다.

### Transfer와 disclosure

먼저 `11uclair`를 private로 전송한다. 모든 transfer에는 mandatory audit disclosure가 포함되며, 여기서 private는 user disclosure가 없다는 뜻이다.

```bash
run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 11uclair --from alice --keyring-backend test \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 9000000 --gas-prices 8500000000uclair --yes --output json > transfer-private.json
wait_for_tx "$(txhash_from transfer-private.json)"

run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 7uclair --privacy-policy amount --disclosure-mode public \
  --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 9000000 --gas-prices 8500000000uclair --yes --output json > transfer-public.json
wait_for_tx "$(txhash_from transfer-public.json)"
run_cli tx privacy decode-transfer-disclosure --tx-hash "$(txhash_from transfer-public.json)" --disclosure-plane public --node "$RPC_ADDR" --report > transfer-public-report.json
```

Public report summary는 `user`, `public`, `amount`, `7`, `uclair`를 식별해야 한다. 다음으로 `10uclair`를 recipient-encrypted `amount-from-to` disclosure로 전송한다. Bob은 user plane, auditor는 audit plane, Alice는 self-view를 읽는다. 각 report는 `verification.verified: true`여야 한다. Audit delivery가 chain에 포함되었다고 decrypt 가능성이 보장되지는 않는다.

```bash
run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 10uclair \
  --privacy-policy amount-from-to --disclosure-mode recipient-encrypted --disclosure-pubkey "$(cat bob-disclosure.hex)" \
  --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 10000000 --gas-prices 8500000000uclair --yes --output json > transfer-recipient.json
wait_for_tx "$(txhash_from transfer-recipient.json)"
for plane in recipient audit self-view; do
  account=bob; [ "$plane" = audit ] && account=auditor; [ "$plane" = self-view ] && account=alice
  run_cli tx privacy decode-transfer-disclosure --tx-hash "$(txhash_from transfer-recipient.json)" \
    --disclosure-plane "$plane" --from "$account" --keyring-backend test --node "$RPC_ADDR" --report > "transfer-${plane}-report.json"
done
run_cli tx privacy list-notes --from bob --keyring-backend test --node "$RPC_ADDR" --rescan-wallet --json > bob-notes.json
```

Bob에게 `11`, `7`, `10` note가 있어야 한다. Scanner는 typed global `(height, global_sequence, output_index)` 순서를 사용하고 복구한 `NoteV1` commitment를 재계산하며 retry duplicate를 제거해야 한다. View tag는 hint이므로 safe mode는 mismatch에도 decrypt를 시도한다. Recipient, auditor, self-view consumer는 plaintext blinding으로 digest를 재계산한다. Audit ciphertext를 decrypt하지 못하면 chain failure가 아니라 `AuditDeliveryFailed` 또는 `ManualReview`로 보고한다.

### Direct와 relayed withdraw

Bob의 `11uclair`를 Alice로 direct-withdraw한 뒤, Bob의 `7uclair` withdraw를 prepare하고 relayer가 정확히 그 prepared file을 제출하게 한다.

```bash
run_cli tx privacy withdraw 11uclair --recipient "$(cat alice-address.txt)" --from bob --keyring-backend test \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 3500000 --gas-prices 8500000000uclair --yes --output json > withdraw-direct.json
wait_for_tx "$(txhash_from withdraw-direct.json)"
run_cli tx privacy prepare-withdraw 7uclair --recipient "$(cat alice-address.txt)" --from bob --keyring-backend test \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --out withdraw-payload.json --output json > withdraw-payload.stdout.json
python3 - <<'PY'
import json
from pathlib import Path
if json.loads(Path('withdraw-payload.stdout.json').read_text()) != json.loads(Path('withdraw-payload.json').read_text()):
    raise SystemExit('prepared payload differs from stdout')
PY
run_cli tx privacy relay-withdraw withdraw-payload.json --from relayer --keyring-backend test \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" --gas 3500000 --gas-prices 8500000000uclair --yes --output json > withdraw-relayed.json
wait_for_tx "$(txhash_from withdraw-relayed.json)"
run_cli tx privacy list-notes --from bob --keyring-backend test --node "$RPC_ADDR" --rescan-wallet --json > bob-notes-final.json
```

최종 Bob의 spendable amount는 `10`이다. 다음 Make target을 실행하기 전에 `cd "$REPO_DIR"`로 repository root에 돌아간다. Disposable home을 제거하기 전 기록한 PID 또는 foreground terminal에서 node를 멈춘다. 다른 run이 선택한 path는 삭제하지 않는다.

## 8. BatchJoinSplit16x32 Localnet

`transfer-batch-16x32`는 `MsgBatchTransfer` 하나와 `BatchJoinSplit16x32` proof 하나다. Single 2x2 transfer는 `MsgTransfer`와 proof 하나를 사용하고, `transfer-batch`는 Cosmos transaction 하나에 독립 2x2 `MsgTransfer` message/proof 여러 개를 넣는다. 세 경로를 구별해야 한다. 구현은 experimental이며 formal trusted setup과 external audit은 아직 수행하지 않았다. Remote prover는 batch 전체 witness를 받는다. Public input/output count는 batch shape를 노출하고 padding은 추가 chain-state/gas 비용으로 active output count만 숨긴다.

기본 target은 static/conformance gate다. Node를 시작하거나 대형 artifact를 만들지 않고 machine-readable fixture를 검증한다. 실제 node와 remote-prover scenario는 명시적으로 실행한다.

```bash
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

Live runner는 `tmp/privacy-batch-joinsplit-localnet/out`에 기록하며 mnemonic이 있는 key JSON, prepared witness, proof의 mode `0600`을 요구한다. Fixture의 one-input payment, mixed-disclosure change, 31-payments-plus-change, exact 32-payment, explicit zero-padding case를 실행한다. 정확한 amount, role, mode는 [`privacy_batch_transfer_v1_contract.json`](../x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json)에 고정되어 있다.

다음은 API-shape 예시이며 완료된 live runner를 이어서 실행하는 경로가 아니다. Runner는 별도 home/localnet을 만들고 종료한다. 계속 실행 중인 자체 node에서 fresh batch-compatible Alice note를 만든 경우에만 실행한다. 실제 shielded recipient, disclosure key, RPC endpoint, chain ID로 바꾼다. `--prover-url`을 생략하면 local proof이며 URL을 주면 정확히 하나의 remote prover를 선택한다.

```bash
clairveild --home "$CLAIRVEIL_HOME" tx privacy prepare-batch-transfer \
  --payment '<recipient-clairs-address>,4uclair' \
  --payment '<recipient-clairs-address>,5uclair,amount,public' \
  --payment '<recipient-clairs-address>,6uclair,amount-from-to,recipient-encrypted,<recipient-disclosure-pubkey-hex>' \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --output-mode compact --prepared-out prepared.json --rescan-wallet --from alice --keyring-backend test
clairveild --home "$CLAIRVEIL_HOME" tx privacy prove-batch-transfer prepared.json --proof-out proof.json --prover-url http://127.0.0.1:18080 --output json
clairveild --home "$CLAIRVEIL_HOME" tx privacy broadcast-batch-transfer prepared.json proof.json --from alice --keyring-backend test \
  --node "$RPC_ADDR" --chain-id "$CHAIN_ID" --gas 80000000 --gas-prices 8500000000uclair --yes --output json
```

`--output-mode exact32`는 의도한 padding에만, `--no-self-view`는 sender가 batch 전체의 self-view를 끌 때만 사용한다. Bearer token은 command argument가 아니라 `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`에 둔다. Loopback은 HTTP를 쓸 수 있으나 모든 non-loopback prover URL은 HTTPS여야 하며 client는 redirect를 따르지 않는다. Automatic multi-prover failover는 없으므로 remote failure는 privacy-aware 정책으로 명시적으로 처리한다.

Live runner는 node와 prover를 재시작하고 stored hash로 exact32 transaction을 조회한 다음, 이미 spent된 payload에 새 envelope로 서명했을 때 fail closed하는지 검증한다. 이는 spent-nullifier smoke이지 durable worker retry 검증은 아니다. Production 순서는 operation ID, reservation, prepared payload, proof, exact signed bytes 보존; tx hash first query; 재서명 전 모든 input nullifier query; policy가 허용하면 같은 signed bytes retry; atomic batch 일부 retry 금지; expected output evidence가 일치할 때만 item 성공 처리다.

```bash
CLAIRVEIL_BATCH_LOCALNET_WORK_DIR=/fast-disk/clairveil-batch \
CLAIRVEIL_BATCH_ARTIFACT_DIR=/verified/dev-artifacts RPC_PORT=27657 PROVERD_PORT=19080 \
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

Work directory는 신규 또는 비어 있어야 한다. 첫 실행 뒤 script는 그 directory만 초기화할 수 있는 marker를 남기며 symlink, 보호 path, marker 없는 non-empty directory는 거부한다. `BATCH_EXPIRES_IN` 기본값은 7200초, `BATCH_GAS` 기본값은 80000000이다. [reference payroll README](../examples/reference-payroll/README-kr.md)에 one-proof durable reservation, exact-byte retry, reconciliation, item-evidence contract가 있다.

다음 문서: [아키텍처](clairveil-architecture-kr.md), [운영 가이드](clairveil-operations-guide-kr.md), [reference payroll](../examples/reference-payroll/README-kr.md), [전체 문서 index](README-kr.md).
