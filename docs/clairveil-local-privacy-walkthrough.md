# Clairveil Local Privacy Walkthrough

Baseline date: 2026-05-04

This tutorial starts from an empty local environment, creates a single-node chain in the default home `~/.clairveil`, and runs the current privacy features directly with the `clairveild` CLI.

If you only want to prepare a local node quickly, you can use `make init`. `make init` automatically prepares build/install, backup of an existing `~/.clairveil`, key creation, genesis, audit master pubkey, and ZK artifacts. This document intentionally does not shorten the process with `make init`, because it is a manual walkthrough for understanding what each step does.

Korean version: [clairveil-local-privacy-walkthrough-kr.md](clairveil-local-privacy-walkthrough-kr.md)

The features validated here are:

- `clairs1...` shielded address derivation from transparent keyring accounts
- `show-view-key`, `show-disclosure-pubkey`
- deposit
- note scan / machine-readable note JSON
- the single `transfer` command
- user selective disclosure: private, public, recipient-encrypted
- mandatory audit disclosure
- `decode-transfer-disclosure`
- direct withdraw
- prepare / relay withdraw

## 1. Prepare Repository And Output Directories

```bash
test -d ~/clairveil/.git || git clone https://github.com/DELIGHT-LABS/clairveil.git ~/clairveil
cd ~/clairveil
rm -rf ~/clairveil-privacy-walkthrough
mkdir -p ~/clairveil-privacy-walkthrough/out ~/clairveil-privacy-walkthrough/bin
rm -rf ~/.clairveil
```

## 2. Build Binaries

```bash
cd ~/clairveil
go build -o ~/clairveil-privacy-walkthrough/bin/clairveild ./cmd/clairveild
go build -o ~/clairveil-privacy-walkthrough/bin/clairveil-setup ./cmd/clairveil-setup
cd ~/clairveil-privacy-walkthrough
```

## 3. Generate zk Artifacts

```bash
~/clairveil-privacy-walkthrough/bin/clairveil-setup --out ~/clairveil-privacy-walkthrough/artifacts
```

Check the generated files.

```bash
ls ~/clairveil-privacy-walkthrough/artifacts
```

At minimum, these files should be present.

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

## 4. Create Transparent Accounts

```bash
~/clairveil-privacy-walkthrough/bin/clairveild keys add alice --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add bob --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add relayer --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild keys add auditor --keyring-backend test
```

Save the addresses.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a alice --keyring-backend test | tee out/alice-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a bob --keyring-backend test | tee out/bob-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a relayer --keyring-backend test | tee out/relayer-address.txt
~/clairveil-privacy-walkthrough/bin/clairveild keys show -a auditor --keyring-backend test | tee out/auditor-address.txt
```

Each file contains a `clair1...` address.

## 5. Prepare Auditor Disclosure Pubkey

The latest Clairveil transfer always includes audit disclosure. Create the audit master pubkey for genesis first.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-disclosure-pubkey --from auditor --keyring-backend test --output json | tee out/auditor-disclosure.json
```

Save the `public_key_hex` value to a file.

```bash
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/auditor-disclosure.json').read_text())
Path('out/auditor-disclosure.hex').write_text(data['public_key_hex'] + '\n')
print(data['public_key_hex'])
PY2
```

The output should be a 64-character hex string.

## 6. Chain Init

```bash
~/clairveil-privacy-walkthrough/bin/clairveild init local --chain-id clairveil-local-1
```

`clairveild init` sets `uclair` as the staking, mint, gov denom and bank metadata by default. Check it with:

```bash
python3 - <<'PY2'
import json
from pathlib import Path
doc = json.loads((Path.home() / '.clairveil' / 'config' / 'genesis.json').read_text())
app = doc['app_state']
print('bond denom:', app['staking']['params']['bond_denom'])
print('mint denom:', app['mint']['params']['mint_denom'])
print('gov denom:', app['gov']['params']['min_deposit'][0]['denom'])
print('bank metadata base:', app['bank']['denom_metadata'][0]['base'])
PY2
```

All values should be `uclair`.

## 7. Add Genesis Accounts

```bash
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account alice 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account bob 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account relayer 100000000000000000000uclair --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild add-genesis-account auditor 100000000000000000000uclair --keyring-backend test
```

## 8. gentx And collect-gentxs

```bash
~/clairveil-privacy-walkthrough/bin/clairveild gentx alice 9000000000000000000uclair --chain-id clairveil-local-1 --keyring-backend test
~/clairveil-privacy-walkthrough/bin/clairveild collect-gentxs
~/clairveil-privacy-walkthrough/bin/clairveild validate
```

If `validate` prints `valid genesis file`, the genesis is valid.

## 9. Set Audit Master Pubkey In Genesis

Insert the auditor disclosure pubkey from section 5 into privacy genesis.

```bash
python3 - <<'PY2'
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
PY2
```

If a non-empty base64 string is printed, continue.

Validate genesis again.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild validate
```

## 10. Start Node

Apply the zk artifact checksum environment and set preflight to strict.

```bash
set -a
source ~/clairveil-privacy-walkthrough/artifacts/privacy_zk_checksums.env
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

Start the node in the background.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild start --minimum-gas-prices 0uclair > ~/clairveil-privacy-walkthrough/clairveild.log 2>&1 &
echo $! | tee out/clairveild.pid
```

Wait briefly for block production.

```bash
sleep 10
~/clairveil-privacy-walkthrough/bin/clairveild status | tee out/status.json
```

Continue when `latest_block_height` is 1 or greater.

## 11. Check Shielded Address, View Key, Disclosure Pubkey

Save Alice and Bob shielded addresses.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-address --from alice --keyring-backend test --output json | tee out/alice-shielded.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-address --from bob --keyring-backend test --output json | tee out/bob-shielded.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-view-key --from alice --keyring-backend test --output json | tee out/alice-view-key.json
```

Extract the `address` field from JSON.

```bash
python3 - <<'PY2'
import json
from pathlib import Path
for src, dst in [('out/alice-shielded.json', 'out/alice-shielded-address.txt'),
                 ('out/bob-shielded.json', 'out/bob-shielded-address.txt')]:
    data = json.loads(Path(src).read_text())
    Path(dst).write_text(data['address'] + '\n')
    print(dst, data['address'])
PY2
```

The addresses must start with `clairs1...`.

Save Bob's disclosure pubkey as well.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy show-disclosure-pubkey --from bob --keyring-backend test --output json | tee out/bob-disclosure.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/bob-disclosure.json').read_text())
Path('out/bob-disclosure.hex').write_text(data['public_key_hex'] + '\n')
print(data['public_key_hex'])
PY2
```

## 12. Alice Prepares Privacy Notes

This tutorial prepares these notes for Alice.

- `11uclair`
- `10uclair`
- `7uclair`
- `0uclair` dummy

After each tx, wait until the tx hash is included in a block.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 11uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-11.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-11.json').read_text())
Path('out/deposit-11.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-11.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 10uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-10.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-10.json').read_text())
Path('out/deposit-10.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-10.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 7uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-7.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-7.json').read_text())
Path('out/deposit-7.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-7.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy deposit 0uclair --from alice --keyring-backend test --chain-id clairveil-local-1 --gas 2500000 --gas-prices 8500000000uclair --yes --output json | tee out/deposit-dummy.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/deposit-dummy.json').read_text())
Path('out/deposit-dummy.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/deposit-dummy.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

Check Alice's notes.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from alice --keyring-backend test --node tcp://localhost:26657 --json | tee out/alice-notes.json
python3 - <<'PY2'
import json
from pathlib import Path
doc = json.loads(Path('out/alice-notes.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('alice spendable note amounts:', ', '.join(amounts))
print('summary:', doc['summary'])
PY2
```

If `11, 10, 7, 0` appear, continue.

## 13. Basic Private Transfer

Alice sends `11uclair` to Bob. There is no user disclosure.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 11uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/transfer-private.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-private.json').read_text())
Path('out/transfer-private.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-private.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 14. Public User Disclosure Transfer

Alice sends `7uclair` to Bob and publicly discloses only `amount` as user disclosure.

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
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-public.json').read_text())
Path('out/transfer-public.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-public.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

Read the public disclosure report.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-public.txhash)" \
  --disclosure-plane public \
  --report | tee out/transfer-public-report.json
python3 - <<'PY2'
import json
from pathlib import Path
doc = json.loads(Path('out/transfer-public-report.json').read_text())
print(doc['summary'])
PY2
```

Check these values.

- `summary.plane` is `user`
- `summary.delivery` is `public`
- `summary.policy` is `amount`
- `summary.amount` is `7`
- `summary.asset_denom` is `uclair`

## 15. Recipient-Encrypted User Disclosure Transfer

Alice sends `10uclair` to Bob and sends `amount-from-to` user disclosure encrypted to Bob.

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
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/transfer-recipient.json').read_text())
Path('out/transfer-recipient.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/transfer-recipient.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

Bob reads the user disclosure.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane recipient \
  --from bob \
  --keyring-backend test \
  --report | tee out/transfer-recipient-user-report.json
```

The auditor reads the audit disclosure.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --report | tee out/transfer-recipient-audit-report.json
```

Summarize both reports.

```bash
python3 - <<'PY2'
import json
from pathlib import Path
for path in ['out/transfer-recipient-user-report.json', 'out/transfer-recipient-audit-report.json']:
    doc = json.loads(Path(path).read_text())
    print(path, doc['summary'])
    print('verified:', doc['verification']['verified'])
PY2
```

Check these values.

- User report `delivery` is `recipient-encrypted`
- User report `policy` is `amount-from-to`
- Audit report `delivery` is `audit-encrypted`
- Audit report `policy` is `audit-full`
- Both reports can show `amount`, `asset_denom`, `from_shielded_address`, and `to_shielded_address`
- `verification.verified` is `true`

## 16. Check Bob Notes

Bob now has `11`, `7`, and `10` notes.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from bob --keyring-backend test --node tcp://localhost:26657 --json | tee out/bob-notes.json
python3 - <<'PY2'
import json
from pathlib import Path
doc = json.loads(Path('out/bob-notes.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('bob spendable note amounts:', ', '.join(amounts))
PY2
```

If `11, 7, 10` appear, continue.

## 17. Direct Withdraw

Bob direct-withdraws `11uclair` to Alice's transparent address.

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
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/withdraw-direct.json').read_text())
Path('out/withdraw-direct.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/withdraw-direct.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 18. Prepare / Relay Withdraw

Create a prepared payload for Bob's `7uclair` note and let the relayer submit it.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --out out/withdraw-payload.json \
  --output json | tee out/withdraw-payload.stdout.json
```

Confirm the stdout payload matches the file payload.

```bash
python3 - <<'PY2'
import json
from pathlib import Path
stdout_payload = json.loads(Path('out/withdraw-payload.stdout.json').read_text())
file_payload = json.loads(Path('out/withdraw-payload.json').read_text())
if stdout_payload != file_payload:
    raise SystemExit('stdout payload and file payload differ')
print('prepare-withdraw stdout payload matches file payload')
PY2
```

The relayer submits it.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy relay-withdraw out/withdraw-payload.json \
  --from relayer \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json | tee out/withdraw-relayed.json
python3 - <<'PY2'
import json
from pathlib import Path
data = json.loads(Path('out/withdraw-relayed.json').read_text())
Path('out/withdraw-relayed.txhash').write_text(data['txhash'] + '\n')
PY2
until ~/clairveil-privacy-walkthrough/bin/clairveild query tx "$(cat out/withdraw-relayed.txhash)" --output json >/dev/null 2>&1; do sleep 2; done
```

## 19. Final Checks

Check Alice's balance and Bob's remaining notes.

```bash
~/clairveil-privacy-walkthrough/bin/clairveild query bank balances "$(cat out/alice-address.txt)" --output json | tee out/alice-balances.json
~/clairveil-privacy-walkthrough/bin/clairveild tx privacy list-notes --from bob --keyring-backend test --node tcp://localhost:26657 --json | tee out/bob-notes-final.json
python3 - <<'PY2'
import json
from pathlib import Path
doc = json.loads(Path('out/bob-notes-final.json').read_text())
amounts = [note['amount'] for note in doc['notes'] if note['status'] == 'spendable']
print('bob final spendable note amounts:', ', '.join(amounts))
PY2
```

If `bob final spendable note amounts: 10` appears, the walkthrough succeeded.

## 20. Stop Node

```bash
kill "$(cat out/clairveild.pid)"
```

## 21. What This Tutorial Validates

- Local genesis and node start work with the standalone `clairveild` daemon.
- `clairs1...` shielded addresses are derived from `clair1...` transparent keyring accounts.
- deposit / transfer / withdraw work with `uclair`.
- user selective disclosure and mandatory audit disclosure work together.
- both direct withdraw and prepared / relayed withdraw work.
