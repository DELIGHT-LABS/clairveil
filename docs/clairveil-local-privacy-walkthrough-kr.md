# Clairveil 로컬 Privacy 워크스루

기준일: 2026-05-04

이 문서는 빈 로컬 환경에서 시작해 기본 홈 `~/.clairveil`에 single-node를 띄우고, `clairveild` CLI로 현재 privacy 기능을 직접 실행하는 튜토리얼입니다.

로컬 노드를 빠르게 준비하기만 하면 되는 경우에는 `make init`을 사용할 수 있습니다. `make init`은 build/install, 기존 `~/.clairveil` 백업, key 생성, genesis, audit master pubkey, ZK artifact를 자동으로 준비합니다. 이 문서는 각 단계가 무엇을 하는지 이해하기 위한 수동 walkthrough이므로 일부러 `make init`으로 축약하지 않습니다.

검증하는 기능은 아래입니다.

- transparent keyring 기반 `clairs1...` shielded address 파생
- `show-view-key`, `show-disclosure-pubkey`
- deposit
- note scan / machine-readable note JSON
- 단일 `transfer` 명령
- user selective disclosure: private, public, recipient-encrypted
- mandatory audit disclosure
- sender self-view disclosure
- `decode-transfer-disclosure`
- direct withdraw
- prepare / relay withdraw

## 1. 저장소와 출력 디렉터리 준비

```bash
test -d ~/clairveil/.git || git clone https://github.com/DELIGHT-LABS/clairveil.git ~/clairveil
cd ~/clairveil
rm -rf ~/clairveil-privacy-walkthrough
mkdir -p ~/clairveil-privacy-walkthrough/out ~/clairveil-privacy-walkthrough/bin
rm -rf ~/.clairveil
```

## 2. 바이너리 빌드

```bash
cd ~/clairveil
go build -o ~/clairveil-privacy-walkthrough/bin/clairveild ./cmd/clairveild
go build -o ~/clairveil-privacy-walkthrough/bin/clairveil-setup ./cmd/clairveil-setup
cd ~/clairveil-privacy-walkthrough
```

## 3. zk artifact 생성

```bash
~/clairveil-privacy-walkthrough/bin/clairveil-setup --out ~/clairveil-privacy-walkthrough/artifacts
```

생성 결과를 확인합니다.

```bash
ls ~/clairveil-privacy-walkthrough/artifacts
```

최소한 아래 파일들이 보여야 합니다.

- `privacy_zk_checksums.env`
- `privacy_zk_manifest.json`
- `privacy_deposit_r1cs.bin`
- `privacy_deposit_pk.bin`
- `privacy_deposit_vk.bin`
- `privacy_spend_r1cs.bin`
- `privacy_spend_pk.bin`
- `privacy_spend_vk.bin`
- `privacy_joinsplit_r1cs.bin`
- `privacy_joinsplit_pk.bin`
- `privacy_joinsplit_vk.bin`

## 4. transparent 계정 생성

```bash
~/clairveil-privacy-walkthrough/bin/clairveild keys add alice --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add bob --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add relayer --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add auditor --keyring-backend test
```

주소를 저장합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a alice --keyring-backend test | tee out/alice-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a bob --keyring-backend test | tee out/bob-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a relayer --keyring-backend test | tee out/relayer-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a auditor --keyring-backend test | tee out/auditor-address.txt
```

각 파일에는 `clair1...` 주소가 들어갑니다.

## 5. auditor disclosure pubkey 준비

Clairveil의 최신 transfer는 audit disclosure를 항상 포함합니다. 그래서 genesis에 넣을 audit master pubkey를 먼저 만듭니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-disclosure-pubkey --from auditor --keyring-backend test --output json | tee out/auditor-disclosure.json
```

`public_key_hex` 값을 파일로 저장합니다.

```bash
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/auditor-disclosure.json').read_text())
Path('out/auditor-disclosure.hex').write_text(data['public_key_hex'] + '\n')
print(data['public_key_hex'])
PY
```

출력은 64자 hex 문자열이어야 합니다.

## 6. chain init

```bash
~/clairveil-privacy-walkthrough/bin/clairveild init local --chain-id clairveil-local-1
```

`clairveild init`은 기본적으로 `uclair` staking/mint/gov denom과 bank metadata를 넣습니다. 아래 명령으로 확인합니다.

```bash
python3 - <<'PY'
import json
from pathlib import Path
doc = json.loads((Path.home() / '.clairveil' / 'config' / 'genesis.json').read_text())
app = doc['app_state']
print('bond denom:', app['staking']['params']['bond_denom'])
print('mint denom:', app['mint']['params']['mint_denom'])
print('gov denom:', app['gov']['params']['min_deposit'][0]['denom'])
print('bank metadata base:', app['bank']['denom_metadata'][0]['base'])
PY
```

모두 `uclair`로 보이면 정상입니다.

## 7. genesis account 추가

```bash
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account alice 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account bob 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account relayer 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account auditor 100000000000000000000uclair --keyring-backend test
```

## 8. gentx와 collect-gentxs

```bash
~/clairveil-privacy-walkthrough/bin/clairveild gentx alice 9000000000000000000uclair --chain-id clairveil-local-1 --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild collect-gentxs
~/clairveil-privacy-walkthrough/bin/clairveild validate
```

`validate`가 `valid genesis file` 메시지를 출력하면 정상입니다.

## 9. genesis에 audit master pubkey 설정

5장에서 만든 auditor disclosure pubkey를 privacy genesis에 넣습니다.

```bash
python3 - <<'PY'
import base64
import json
from pathlib import Path

genesis_path = Path.home() / '.clairveil' / 'config' / 'genesis.json'
auditor_hex = Path('out/auditor-disclosure.hex').read_text().strip()
auditor_b64 = base64.b64encode(bytes.fromhex(auditor_hex)).decode()

doc = json.loads(genesis_path.read_text())
doc['app_state']['privacy']['audit_master_pubkey'] = auditor_b64
genesis_path.write_text(json.dumps(doc, indent=2))
print(auditor_b64)
PY
```

비어 있지 않은 base64 문자열이 출력되면 정상입니다.

다시 genesis를 검증합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild validate
```

## 10. 노드 시작

zk artifact checksum env를 적용하고 preflight를 strict로 둡니다.

```bash
set -a
source ~/clairveil-privacy-walkthrough/artifacts/privacy_zk_checksums.env
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

노드를 백그라운드로 시작합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild start --minimum-gas-prices 0uclair > ~/clairveil-privacy-walkthrough/clairveild.log 2>&1 &
echo $! | tee out/clairveild.pid
```

블록 생성까지 잠시 기다립니다.

```bash
sleep 10
~/clairveil-privacy-walkthrough/bin/clairveild status | tee out/status.json
```

`latest_block_height`가 1 이상이면 계속 진행합니다.

## 11. shielded address, view key, disclosure pubkey 확인

Alice와 Bob의 shielded address를 저장합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-address --from alice --keyring-backend test --output json | tee out/alice-shielded.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-address --from bob --keyring-backend test --output json | tee out/bob-shielded.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-view-key --from alice --keyring-backend test --output json | tee out/alice-view-key.json
```

JSON에서 `address` 필드를 꺼냅니다.

```bash
python3 - <<'PY'
import json
from pathlib import Path
for src, dst in [('out/alice-shielded.json', 'out/alice-shielded-address.txt'),
                 ('out/bob-shielded.json', 'out/bob-shielded-address.txt')]:
    data = json.loads(Path(src).read_text())
    Path(dst).write_text(data['address'] + '\n')
    print(dst, data['address'])
PY
```

주소는 `clairs1...`로 시작해야 합니다.

Bob의 disclosure pubkey도 저장합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-disclosure-pubkey --from bob --keyring-backend test --output json | tee out/bob-disclosure.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/bob-disclosure.json').read_text())
Path('out/bob-disclosure.hex').write_text(data['public_key_hex'] + '\n')
print(data['public_key_hex'])
PY
```

## 12. Alice가 privacy note 준비

이번 튜토리얼은 Alice에게 아래 note를 준비합니다.

- `11uclair`
- `10uclair`
- `7uclair`
- `0uclair` dummy

각 tx 후에는 tx hash가 블록에 들어갈 때까지 기다립니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 11uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-11.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-11.json').read_text())
Path('out/deposit-11.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-11.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 10uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-10.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-10.json').read_text())
Path('out/deposit-10.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-10.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 7uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-7.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-7.json').read_text())
Path('out/deposit-7.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-7.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 0uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-dummy.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-dummy.json').read_text())
Path('out/deposit-dummy.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-dummy.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

Alice note를 확인합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from alice --keyring-backend test --node tcp://localhost:26657 --json | tee out/alice-notes.json
python3 - <<'PY'
import json
from pathlib import Path
doc = json.loads(Path('out/alice-notes.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('alice spendable note amounts:', ', '.join(amounts))
print('summary:', doc['summary'])
PY
```

`11, 10, 7, 0`이 보이면 정상입니다.

## 13. 기본 private transfer

Alice가 Bob에게 `11uclair`를 보냅니다. user disclosure는 없습니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 11uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/transfer-private.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-private.json').read_text())
Path('out/transfer-private.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-private.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 14. public user disclosure transfer

Alice가 Bob에게 `7uclair`를 보내고, user disclosure는 `amount`만 public으로 엽니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 7uclair \
  --privacy-policy amount \
  --disclosure-mode public \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/transfer-public.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-public.json').read_text())
Path('out/transfer-public.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-public.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

public disclosure report를 읽습니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-public.txhash)" \
  --disclosure-plane public \
  --report | tee out/transfer-public-report.json
python3 - <<'PY'
import json
from pathlib import Path
doc = json.loads(Path('out/transfer-public-report.json').read_text())
print(doc['summary'])
PY
```

확인할 값:

- `summary.plane`은 `user`
- `summary.delivery`는 `public`
- `summary.policy`는 `amount`
- `summary.amount`는 `7`
- `summary.asset_denom`은 `uclair`

## 15. recipient-encrypted user disclosure transfer

Alice가 Bob에게 `10uclair`를 보내고, user disclosure는 `amount-from-to`를 Bob에게 encrypted로 보냅니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 10uclair \
  --privacy-policy amount-from-to \
  --disclosure-mode recipient-encrypted \
  --disclosure-pubkey "$(cat out/bob-disclosure.hex)" \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 10000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/transfer-recipient.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-recipient.json').read_text())
Path('out/transfer-recipient.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-recipient.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

Bob이 user disclosure를 봅니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane recipient \
  --from bob \
  --keyring-backend test \
  --report | tee out/transfer-recipient-user-report.json
```

Auditor가 audit disclosure를 봅니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --report | tee out/transfer-recipient-audit-report.json
```

Alice가 sender self-view disclosure를 봅니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane self-view \
  --from alice \
  --keyring-backend test \
  --report | tee out/transfer-recipient-self-view-report.json
```

세 report를 요약합니다.

```bash
python3 - <<'PY'
import json
from pathlib import Path
for path in [
    'out/transfer-recipient-user-report.json',
    'out/transfer-recipient-audit-report.json',
    'out/transfer-recipient-self-view-report.json',
]:
    doc = json.loads(Path(path).read_text())
    print(path, doc['summary'])
    print('verified:', doc['verification']['verified'])
PY
```

확인할 값:

- user report의 `delivery`는 `recipient-encrypted`
- user report의 `policy`는 `amount-from-to`
- audit report의 `delivery`는 `audit-encrypted`
- audit report의 `policy`는 `audit-full`
- 둘 다 `amount`, `asset_denom`, `from_shielded_address`, `to_shielded_address`를 확인할 수 있음
- `verification.verified`가 `true`

## 16. Bob note 확인

Bob은 이제 `11`, `7`, `10` note를 갖습니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from bob --keyring-backend test --node tcp://localhost:26657 --json | tee out/bob-notes.json
python3 - <<'PY'
import json
from pathlib import Path
doc = json.loads(Path('out/bob-notes.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('bob spendable note amounts:', ', '.join(amounts))
PY
```

`11, 7, 10`이 보이면 정상입니다.

## 17. direct withdraw

Bob이 `11uclair`를 Alice transparent 주소로 direct withdraw 합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy withdraw 11uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/withdraw-direct.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/withdraw-direct.json').read_text())
Path('out/withdraw-direct.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/withdraw-direct.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 18. prepare / relay withdraw

Bob의 `7uclair` note를 prepared payload로 만들고, relayer가 제출합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --out out/withdraw-payload.json \
  --output json | tee out/withdraw-payload.stdout.json
```

stdout payload와 파일 payload가 같은지 확인합니다.

```bash
python3 - <<'PY'
import json
from pathlib import Path
stdout_payload = json.loads(Path('out/withdraw-payload.stdout.json').read_text())
file_payload = json.loads(Path('out/withdraw-payload.json').read_text())
if stdout_payload != file_payload:
    raise SystemExit('stdout payload and file payload differ')
print('prepare-withdraw stdout payload matches file payload')
PY
```

relayer가 제출합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy relay-withdraw out/withdraw-payload.json \
  --from relayer \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/withdraw-relayed.json
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('out/withdraw-relayed.json').read_text())
Path('out/withdraw-relayed.txhash').write_text(data['txhash'] + '\n')
PY
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/withdraw-relayed.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 19. 마무리 확인

Alice 잔액과 Bob의 남은 note를 확인합니다.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild query bank balances "$(cat out/alice-address.txt)" --output json | tee out/alice-balances.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from bob --keyring-backend test --node tcp://localhost:26657 --json | tee out/bob-notes-final.json
python3 - <<'PY'
import json
from pathlib import Path
doc = json.loads(Path('out/bob-notes-final.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('bob final spendable note amounts:', ', '.join(amounts))
PY
```

`bob final spendable note amounts: 10`이 보이면 정상입니다.

## 20. 종료

```bash
kill "$(cat out/clairveild.pid)"
```

## 21. 이 튜토리얼에서 검증한 것

- `clairveild` standalone daemon으로 local genesis와 node start가 가능함
- `clair1...` transparent keyring에서 `clairs1...` shielded address가 파생됨
- deposit / transfer / withdraw가 `uclair` 기준으로 동작함
- user selective disclosure, sender self-view disclosure, mandatory audit disclosure가 함께 동작함
- direct withdraw와 prepared / relayed withdraw가 모두 동작함
