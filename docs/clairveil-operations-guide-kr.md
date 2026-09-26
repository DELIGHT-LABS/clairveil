# Clairveil 운영 가이드

이 문서는 Clairveil을 채택하는 downstream project가 운영에서 결정해야 하는 항목을 정리합니다. 이 repository는 reusable privacy core와 reference host이며 production chain 자체가 아닙니다.

영문판: [clairveil-operations-guide.md](clairveil-operations-guide.md)

## 1. 운영 책임 경계

| 영역 | Clairveil repo | Downstream project |
| --- | --- | --- |
| Privacy module | `x/privacy` 구현과 reference app | app wiring, store key, module account, governance/policy 결합 |
| Reference node | `clairveild` local 검증 | validator 운영, sentry, snapshot, upgrade, monitoring |
| ZK artifact | 생성/검증 tooling | artifact signing, provenance, reproducible build, release custody |
| Prover | `clairveil-proverd` reference service | topology, auth, quota, deployment, logging, retention |
| Audit disclosure | public initial key, epoch lifecycle, decode flow | audit private-key custody, governance rotation, access control |
| Wallet | CLI/SDK helper와 fixture | browser/mobile storage encryption, UX, telemetry redaction |

## 2. Node 운영 baseline

Production-like node는 initial audit key와 proof of possession을 담은 public V4 audit configuration을 입력받고, privacy module account를 bank module account로 등록하며, configured circuit identity를 supplied artifact manifest에 결합해야 합니다. Wallet 동기화용 V1 scan/tree/reserve/asset query와 별도로 live audit configuration/key history용 V2 `audit/configuration`, `audit/key_schedule`, `audit/keys/{epoch}` query를 노출합니다. Release 전 snapshot/restore rehearsal도 완료해야 합니다. 이 store는 일반 Cosmos state와 wallet scan state이며 transaction replay archive나 audit ledger가 아닙니다.

V2 audit transfer는 four-circuit `privacy-note-v1-u128-audit-field-v1` consensus identity와 일치하는 local audit-field VK가 있을 때만 활성화합니다.

```bash
clairveild start \
  --audit-config /secure/config/audit-config.json \
  --audit-artifacts /opt/clairveil/privacy-artifacts \
  --minimum-gas-prices 0uclair
```

## 3. ZK artifact 운영

`clairveil-setup`은 audit-field R1CS/PK/VK와 checksum manifest를 생성합니다. 지원 flag는 `--out`, `--development`뿐입니다.

```bash
clairveil-setup --out artifacts/privacy --development
```

`--circuit`, `--overwrite`는 제거되었습니다. 이전 selective JoinSplit rotation과 hash는 실행 가능한 setup 절차가 아닌 legacy history입니다. 새 directory에 fresh development bundle을 만들고 fresh genesis에서 binding하며 strict preflight를 요구하세요. Old manifest와 섞으면 안 됩니다.

`privacy-note-v1-u128-audit-field-v1`은 `deposit-audit-field-u128-v1`, `spend-audit-field-u128-v1`, `joinsplit-2x2-audit-field-u128-v1`, `batch-joinsplit-16x32-audit-field-u128-v1` exact order의 descriptor를 요구합니다. Validator는 consensus identity를 비교하고 네 VK만 load하며 prover readiness는 선택한 R1CS/PK pair만 lazy load합니다. `privacy_zk_manifest.json` schema `v2`는 ordered descriptor, VK SHA-256, public-input schema SHA-256을 포함해 `CircuitSetIdentity` schema `v1`과 일치해야 합니다. Environment checksum은 추가 consistency check일 뿐 consensus identity를 override할 수 없고, mismatch는 startup/readiness를 실패시켜야 합니다.

Repository artifact는 development artifact이며 formal trusted setup이나 production distribution이 아닙니다. Production release는 circuit source commit, generation command, checksum manifest, signer를 기록하고 artifact를 read-only mount하며 directory를 `--audit-artifacts`로 전달하고 stale artifact 또는 chain verifier mismatch를 release blocker로 처리해야 합니다. Node startup은 manifest와 consensus identity 검사를 자동으로 수행합니다. 기록된 batch artifact hash와 resource history는 [clairveil-batch-joinsplit-16x32-kr.md](clairveil-batch-joinsplit-16x32-kr.md)에 남아 있습니다.

## 4. Merkle tree 운영

Privacy pool은 depth-32 single Merkle tree입니다.

| Transaction | Leaf 변화 |
| --- | --- |
| deposit | +1 |
| native 2x2 transfer | +2 |
| batch transfer | +1..32 |
| withdraw | +0 |

`leaf_count`, `max_leaves`, `remaining_leaves`, current root, historical-root retention을 추적합니다.

| 사용률 | 의미 |
| --- | --- |
| 50% | 장기 capacity trend 확인을 시작합니다. |
| 70% | 새 pool/circuit upgrade를 논의합니다. |
| 85% | upgrade plan을 확정합니다. |
| 95% | migration window를 준비하거나 큰 inflow를 제한합니다. |

## 5. Merkle 복구 검증

Snapshot, restore, migration 뒤에는 `Leaf/*`, `MerkleNode/*`, `CommitmentIndex/*`, `HistoricalRoot/*`, cached root, `leaf_count`를 같은 height의 일관된 snapshot에서 복구합니다. Leaf나 lower node가 빠졌을 때 cached root 하나만 맞는 것은 정상 복구가 아닙니다.

`tree_state` query로 `leaf_count`, `max_leaves`, `remaining_leaves`, `root`를 확인합니다. 오래된 commitment 하나와 최근 append된 commitment 하나 이상을 고릅니다. 각 샘플의 `merkle_path`가 `path`, `path_helper`를 error 없이 반환해야 하며, commitment bytes와 반환된 `path`, `path_helper`로 root를 오프체인에서 재계산해 `tree_state.root`와 일치시킵니다. `merkle_path.root`만 비교해서는 충분하지 않습니다.

`TreeState`는 query마다 모든 lower `MerkleNode/*`를 스캔하지 않습니다. 이는 비용을 피하기 위한 의도적인 책임 분리이며 sampled `MerklePath` 재계산과 append/write의 required-node check가 lower-node integrity를 담당합니다. 따라서 복구 완료는 `TreeState` 성공만이 아니라 sample recomputation 성공까지 의미합니다. `MaxMerkleRebuildLeaves`를 넘는 tree에 cached root가 없으면 keeper는 의도적으로 자동 복구하지 않으므로, offline rebuild, state restore 재검증, 별도 migration plan을 사용합니다.

## 6. Prover 운영

`clairveil-proverd`는 private seed 대신 prepared proof payload를 받지만, payload에는 amount, note randomness, Merkle path/root, nullifier, shielded public key, disclosure payload/ciphertext, timing, client-identity metadata가 포함될 수 있습니다. 이는 단순 CPU worker가 아니라 privacy-sensitive trusted component입니다. Request/response body나 private witness material을 보관하면 안 됩니다.

### Remote topology와 disclosure 경계

| Topology | 적합한 상황 | Downstream 결정 |
| --- | --- | --- |
| Browser/WASM prover | 사용자가 privacy metadata를 browser 밖으로 보내지 않아야 하는 UX | SDK와 artifact delivery |
| Local daemon | 개발, desktop wallet, 고신뢰 workstation | installer, lifecycle, local auth |
| Private remote sidecar | company-controlled wallet backend | mTLS, private network, retention |
| Public remote prover | 일반 web-wallet UX | strong auth, quota, monitoring |

Configured prover endpoint 하나를 사용하고 automatic failover를 비활성화합니다. Timeout/response check 뒤 같은 endpoint를 retry하는 것은 허용됩니다. Witness-bearing request를 다른 endpoint로 보내려면 추가 operator와 privacy boundary를 명시한 사용자 또는 product-policy의 explicit opt-in이 필요하며 availability만으로 disclosure 범위를 넓힐 수 없습니다.

현재 contract는 `/v2/prover/audit-field`, request/response envelope `v1`, `privacy-note-v1-u128-audit-field-v1`, base64 byte slice, final PI23입니다. V2 message 전에 exact local artifact identity로 response를 확인해야 합니다. Transfer/withdraw/batch/deposit V1 자료는 V2 input이 아닌 legacy evidence입니다. Request body, bearer credential, signature, disclosure plaintext/blinding, proof를 log, trace, crash dump, analytics에서 제외합니다.

### Production HTTP 경계

Remote prover를 private network 또는 edge proxy 뒤에 둡니다. Non-loopback witness traffic은 HTTPS를 사용해야 하며 TLS는 edge proxy, load balancer, service mesh, mTLS에서 terminate해도 됩니다. Reference prover는 bearer token을 설정한 경우에만 이를 강제하며 token 미설정 상태에서는 해당 검사가 비활성화됩니다. Production remote deployment는 bearer authentication을 설정하거나 동등한 mTLS/session-bound control 뒤에 두고 user, wallet, IP, API token별 quota를 둡니다. Bearer token은 최소 128-bit random entropy를 사용하고 secret manager 또는 동등한 injection path로 제공하며 rotation 절차를 문서화합니다.

Edge와 application body limit을 맞춥니다. `max_request_bytes=8388608`(8 MiB)이 reference default이며 반드시 positive여야 합니다. `0`은 invalid이고 unlimited가 아닙니다. Gzip wire와 decompressed-body limit을 모두 두고 read-header/read/idle/write timeout 정책, worker 수, queue depth를 설정합니다. Long synchronous proof에는 benchmark한 finite write timeout 또는 async job id 반환을 선택합니다. `/healthz`, `/readyz`, `/debug/vars`는 loopback, private network, 인증된 operations plane으로 제한합니다.

`x/privacy/client/sdk/provertransport.HTTPHandler`는 낮은 수준의 transport handler입니다. Admission 전 positive request limit은 적용하지만 production bearer authorization, gzip dual limit, readiness policy, server timeout이 없습니다. Bounded `x/privacy/client/sdk/proverservice.Handler`를 노출하거나 raw limit을 보존하면서 authorization, outer `http.MaxBytesReader`, timeout, 운영 policy를 추가한 wrapper를 사용합니다. Proxy body limit만으로는 충분하지 않습니다.

### Runtime, readiness, admission

`clairveil-proverd`는 audit-field V2 route만 노출합니다. `/healthz`, `/readyz`는 configured prover에서 advertised `routes`, `circuits`를 산출합니다. Development-grade artifact manifest, VK, public-input schema, supplied consensus identity가 runtime과 정확히 일치하지 않으면 readiness는 fail closed합니다.

Reference admission default는 circuit별 `max_in_flight=1`, `max_queued=4`입니다. Queue saturation은 retryable busy response를 반환합니다. Request/witness content 없이 in-flight, queued, rejected, canceled, queue-wait, prove-time, CPU, RSS, route/status, latency, auth failure, body-limit rejection, readiness/preflight failure를 export합니다.

Context cancellation은 caller wait를 끝내지만 실행 중인 in-process gnark proof는 반환할 때까지 계속되고 permit을 유지할 수 있습니다. Reference service는 solver를 preempt하지 못합니다. Hard cancellation 또는 OOM containment에는 isolated, memory-limited worker process를 사용하고 종료합니다.

BatchJoinSplit16x32의 `/v1/proofs/batch-transfer` 경계는 legacy-only입니다. Current V2는 선택한 audit-field `circuit_id`와 `/v2/prover/audit-field`를 사용하며 ad-hoc endpoint를 허용하거나 legacy batch measurement를 current runtime evidence로 만들지 않습니다.

### Canonical audit-field V2 prover route

`POST /v2/prover/audit-field`만 bounded service handler로 노출합니다. Complete witness와 PI23을 받으므로 bearer auth, positive gzip/body limit, audit-field admission, redacted logging, `Cache-Control: no-store`를 적용합니다. Invalid framing/version은 `400`, validated request가 proving에 도달한 뒤의 failure만 `500`입니다. SDK는 finite timeout을 사용하고 반복 response field를 확인한 뒤 exact local artifact identity와 final PI23으로 verify합니다. Authoritative route contract는 [HTTP API](clairveil-proverd-http-api-kr.md)입니다.

## 7. Audit key 운영

모든 asset transaction에는 mandatory audit authorization이 포함됩니다. Active audit epoch의 private key는 해당 envelope가 보호하는 transaction field를 읽을 수 있습니다. Production에는 key-generation ceremony, HSM/KMS 또는 동등한 custody, decrypt 권한 분리, access log와 approval workflow, governance-controlled future epoch activation/cancellation, compromised-key incident response, original successful transaction과 execution event를 대조하는 auditor UX가 필요합니다. Clairveil은 private-key custody나 replay ledger를 구현하지 않습니다.

## 8. Wallet 운영

Reference CLI는 restrictive permission의 local JSON file을 저장하지만 이는 production wallet storage가 아닌 development baseline입니다. Production wallet은 root-seed/derived-secret encryption, viewing-key policy, note-cache encryption, prepared payload/proof JSON retention, telemetry redaction, remote-prover trust-boundary UX, disclosure-decode verification 표시를 결정해야 합니다.

## 9. Monitoring

Transaction type별 count, batch input/output count·deterministic precharge·out-of-gas rejection·atomic rollback error, disclosure-mode distribution, proof latency/error rate, nullifier rejection, Merkle `leaf_count`·usage·failed `merkle_path` query, `reserve`의 `invariant_holds=false`, artifact preflight, remote-prover auth/body-limit failure를 모니터링합니다. 6절의 route/status, queue, cancellation, prove-time, CPU, RSS, readiness signal도 보존합니다.

Private seed, mnemonic, scalar, viewing key, disclosure private key, prepared payload, proof byte, bearer token, decrypted disclosure는 모든 log, trace, crash dump, analytics sink에서 redaction합니다.

## 10. Release gate와 production-capacity 증적

| Gate | 제공하는 증적 | 경계 |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | Static BatchJoinSplit16x32 fixture/conformance | node나 prover를 시작하지 않고 actual proof를 생성하지 않음 |
| `reference-payroll-*` | Legacy multi-message/simulation regression 및 capacity planning | One-proof production capacity 증적이 아님 |
| `make release-check` | `ci`, `vulncheck`, static legacy batch conformance, static/unit/synthetic bulk readiness | Node/prover를 시작하지 않으며 live V2 또는 production-capacity 증적을 제공하지 않음 |

Final downstream release와 mainnet acceptance에는 별도로 기록한 live native V2 증적이 필요하며 checked-in Make target은 이를 제공하지 않습니다. Production-capacity claim은 actual 16x32 workload의 tag/commit-bound artifact로 뒷받침해야 하며 proof/sec, tx/sec, item/sec, RSS, CPU, shape distribution, retry/replan/manual-review 결과, circuit/artifact identity, checksum, 실행 environment를 기록합니다. Synthetic 또는 legacy `reference-payroll-*` 결과를 one-proof production-capacity evidence로 재분류하면 안 됩니다.

## 11. Release 운영

Release commit과 tag를 만들기 전:

```bash
make release-check
```

그 commit에 annotated exact-SemVer tag를 만든 뒤:

```bash
make release-pack
make release-pack-verify
```

Reference prover image는 `make docker-proverd-build`로 build합니다. Release note에는 proto/fixture/schema/CLI/prover contract impact, ZK artifact impact, accepted vulnerability, downstream action, artifact checksum/provenance policy, circuit-set/public-witness/gas/scan-schema version(`privacy-note-v1-u128-audit-field-v1`, PI23, `AuditGasModelV1`, `privacy-sequence-v1`, `privacy-scan-v2`)을 포함합니다.

## 12. Incident 대응 기준

| 상황 | 대응 |
| --- | --- |
| audit key compromise | disclosure access를 중단하고 rotation/migration plan을 실행하며 affected disclosure scope를 산정합니다. |
| prover token leak | token을 rotate하고 access log를 검토하며 proof endpoint abuse를 확인합니다. |
| artifact checksum mismatch | node/prover start를 중지하고 artifact source를 재검증하며 release blocker로 처리합니다. |
| reserve invariant mismatch | release/rollout을 중지하고 module-account balance와 deposit/withdraw total을 비교하며 direct send, top-up, migration write를 조사합니다. |
| Merkle restore mismatch | node를 resume하지 않고 offline rebuild 또는 restore 재시도를 수행합니다. |
| wallet cache corruption | cache를 backup하고 rescan하며 사용자의 seed/key 보존을 확인합니다. |

## 13. Mainnet 전 최소 gate

Clairveil core를 downstream mainnet에 붙이기 전:

1. Downstream app e2e가 deposit/transfer/disclosure/withdraw를 통과합니다.
2. JS/web wallet이 conformance fixture와 live-chain test를 통과합니다.
3. Remote/local/browser prover topology가 결정되어 있습니다.
4. Audit-key custody가 문서화되어 있고 rehearsal이 끝났습니다.
5. Artifact signing/provenance 정책이 있습니다.
6. Snapshot/restore rehearsal과 Merkle-path sample 검증이 끝났습니다.
7. 지원 denom별 deposit/withdraw e2e 뒤 `reserve/{denom=**}`이 `invariant_holds=true`를 반환합니다.
8. Chain-specific threat model이 작성되어 있습니다.
9. Release commit 기준 `TestBatchTransferDirectCoreIntegration`, atomic scan-failure test, cross-message 2x2+batch/batch+batch rollback test가 통과합니다.
10. SDK, remote batch prover route, typed scanner/decrypt path, one-proof payroll integration, CLI/tutorial, conformance fixture, actual localnet workflow가 함께 통과하며 formal setup, production artifact release, external audit, downstream wallet product는 별도 gate입니다.
11. 작동하는 명시적 external flow의 tag/commit-bound live native V2 기록을 승인하고, 모든 production-capacity claim이 10절을 충족합니다. Static Make target은 이 gate를 충족하지 않습니다.
