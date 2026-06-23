# Clairveil Release Handoff Pack

이 문서는 Clairveil release를 downstream chain 팀, JS/TS SDK 팀, web wallet 팀, prover 운영 팀에게 넘길 때 확인해야 하는 산출물과 검증 절차를 한 곳에 묶은 handoff pack입니다.

Clairveil repo는 reusable privacy core와 reference host를 제공합니다. 실제 production chain의 EVM, policy module, precompile, validator 운영, audit private key custody, wallet storage encryption, remote prover 노출 정책은 downstream project가 결정하고 책임집니다.

## 1. 릴리즈 수령자가 받아야 하는 산출물

| 구분 | 파일/경로 | 수령자 | 용도 |
| --- | --- | --- | --- |
| Go module | `go.mod`, `x/privacy`, `app`, `cmd/clairveild` | Core chain team | downstream Cosmos SDK app import/fork 기준 |
| Proto | `proto/clairveil/privacy/v1` | Core chain team, JS SDK team | tx/query type generation |
| SDK fixtures | `x/privacy/client/sdk/conformance/testdata` | JS SDK team, web wallet team | wallet/prover/query contract conformance |
| JSON Schema | `docs/schemas/clairveil-js-wallet-contract.schema.json` | JS SDK team, web wallet team | machine-readable fixture shape validation |
| Prover service | `cmd/clairveil-proverd`, `x/privacy/client/sdk/proverservice`, `x/privacy/client/sdk/provertransport` | Prover operations, JS SDK team | local/remote companion prover contract |
| ZK artifact tooling | `cmd/clairveil-setup`, `cmd/clairveil-verify`, `x/privacy/zk` | Core chain team, prover operations | artifact generation, checksum, preflight |
| Walkthrough | `docs/clairveil-local-privacy-walkthrough-kr.md` | Integrators | local end-to-end manual verification |
| Circuit guide | `docs/clairveil-circuits-kr.md` | Core chain team, prover operations, security reviewers | Deposit/Spend/JoinSplit 회로와 artifact 영향 설명 |
| CLI reference | `docs/clairveil-cli-reference-kr.md` | Integrators, wallet/SDK teams | 사용자-facing command와 flag 설명 |
| Testing guide | `docs/clairveil-testing-guide-kr.md` | Maintainers, integrators | test matrix와 release 검증 명령 |
| Operations guide | `docs/clairveil-operations-guide-kr.md` | Operators, security reviewers | node/prover/artifact/Merkle/audit 운영 기준 |
| Privacy accounting design note | `docs/clairveil-privacy-accounting-design-note-kr.md` | Core chain team, security reviewers | deposit binding, amount bound, reserve invariant, artifact contract 설계 근거 |
| Maintainer instructions | `docs/clairveil-maintainer-instructions-kr.md` | Maintainers | 변경 유형별 문서/검증 규칙 |
| Integration guide | `docs/clairveil-downstream-cosmos-integration-guide-kr.md` | Core chain team | app wiring and responsibility checklist |
| Client product brief | `docs/clairveil-client-product-brief-kr.md` | Wallet/app product, client 팀 | product capability 범위와 client profile |
| Client UX flows | `docs/clairveil-client-ux-flows-kr.md` | Wallet/app product, client 팀 | setup, scan, transfer, withdraw, disclosure, recovery flow |
| Client risk decisions | `docs/clairveil-client-risk-decisions-kr.md` | Product, security, operations | storage, prover, audit, disclosure, telemetry 결정 |
| Client API checklist | `docs/clairveil-client-api-checklist-kr.md` | Client SDK, app 팀 | chain/prover API, fixture, release gate, compatibility check |
| JS SDK handoff | `docs/clairveil-js-sdk-handoff-kr.md` | JS SDK team, web wallet team | SDK implementation checklist |
| Release policy | `docs/clairveil-release-versioning-policy-kr.md`, `docs/clairveil-release-note-template-kr.md` | Maintainers, release recipients | tag, changelog, release note, compatibility impact 기준 |
| Prover profile | `docs/clairveil-proverd-remote-production-profile-kr.md` | Prover operations | remote prover production controls |
| Merkle restore SOP | `docs/clairveil-merkle-restore-sop-kr.md` | Core chain team, operators | snapshot/restore/migration 후 tree state 검증 |
| Security docs | `docs/clairveil-threat-model-kr.md`, `docs/clairveil-security-best-practices-review-kr.md` | Security reviewers, operators | trust boundary and residual risk review |

## 2. 릴리즈 전 repo maintainer 검증

릴리즈 tag를 만들기 전 maintainer는 아래 명령을 실행합니다.

```bash
make release-check
make release-pack
make release-pack-verify
```

`make release-check`는 아래 순서로 실행됩니다.

```text
make ci
make vulncheck
make localnet-smoke
make privacy-e2e-smoke
```

각 단계의 의미는 아래와 같습니다.

| 단계 | 의미 |
| --- | --- |
| `make ci` | Go test, Go binary build, JS/TS examples를 검증합니다. |
| `make vulncheck` | govulncheck policy gate를 실행합니다. 새 actionable vulnerability가 있으면 실패합니다. |
| `make localnet-smoke` | reference daemon이 genesis부터 init/start 가능한지 확인합니다. |
| `make privacy-e2e-smoke` | deposit, transfer, public disclosure, recipient disclosure, audit disclosure, direct withdraw, relayed withdraw를 로컬 노드에서 검증합니다. |

`make release-check`는 pull request마다 자동으로 돌리기에는 무겁습니다. PR 기본 검증은 `.github/workflows/test.yml`의 `make ci`와 `.github/workflows/security.yml`의 `make vulncheck`가 담당하고, release 후보 검증은 사람이 수동으로 `make release-check`를 실행합니다.

Prover Docker packaging을 검증하려면 아래 명령을 별도로 실행합니다.

```bash
make docker-proverd-build
```

이 명령은 compose config, Dockerfile build, image inspect를 확인합니다. Docker daemon이 필요한 검증이므로 기본 `release-check`에는 포함하지 않습니다.

`make release-pack`은 `dist/clairveil-handoff-<version>.tar.gz`와 `.sha256` 파일을 생성합니다. 이 pack은 전체 소스 배포본이 아니라 downstream handoff 계약 묶음입니다. 포함 대상은 license/notice, 주요 handoff/security/operation 문서, circuit/CLI/testing/maintainer 문서, Merkle restore SOP, proto, JSON Schema, conformance fixture, client/JS 예제, prover Docker sample, release pack scripts, `RELEASE-MANIFEST.txt`, `SHA256SUMS.txt`입니다.

`make release-pack-verify`는 handoff pack의 외부 `.sha256`, pack 내부 `SHA256SUMS.txt`, 필수 handoff 파일 목록, 그리고 기본 archive의 manifest commit이 현재 `HEAD`와 일치하는지 확인합니다. `RELEASE_PACK_ARCHIVE`를 지정하지 않은 기본 실행에서는 stale local archive가 누락 파일을 가리지 않도록 검증 전에 기본 pack을 다시 생성합니다. 이 검증은 “tarball이 만들어졌다”가 아니라 “넘겨도 되는 계약 묶음인지”를 확인하는 단계입니다.

## 3. 릴리즈 전 maintainer 체크리스트

1. `git status --short`가 비어 있는지 확인합니다.
2. `make release-check`를 통과시킵니다.
3. `make release-pack`을 실행해 handoff tarball과 checksum을 생성합니다.
4. `make release-pack-verify`로 handoff tarball의 checksum, 내부 파일 checksum, 필수 파일, manifest commit을 검증합니다.
5. remote prover image를 넘기거나 운영할 예정이면 `make docker-proverd-build`를 통과시킵니다.
6. `docs/clairveil-release-handoff-pack-kr.md`의 산출물 목록이 현재 repo 구조와 맞는지 확인합니다.
7. `docs/schemas/clairveil-js-wallet-contract.schema.json`이 최신 fixture와 함께 `make examples`에서 검증되는지 확인합니다.
8. `x/privacy/client/sdk/conformance/testdata` fixture가 downstream JS SDK 팀에게 전달될 release commit과 같은 commit인지 확인합니다.
9. ZK artifact checksum과 preflight mode 정책이 release note에 포함되어 있는지 확인합니다.
10. Merkle snapshot/restore/migration 관련 변경이 있으면 `docs/clairveil-merkle-restore-sop-kr.md`의 샘플 path 재계산 절차가 release note에 반영되어 있는지 확인합니다.
11. `GO-2024-2584`, `GO-2026-4479` 같은 accepted vulnerability policy exception이 release note의 known risk에 남아 있는지 확인합니다.
12. downstream project가 audit master private key custody, wallet storage encryption, remote prover topology를 별도 운영 문서로 소유한다는 점을 release note에 명시합니다.
13. `docs/clairveil-release-versioning-policy-kr.md`의 release note template을 사용해 compatibility impact와 downstream action을 작성합니다.

## 4. Downstream core chain 팀 수령 기준

Core chain 팀은 아래를 확인합니다.

1. `github.com/DELIGHT-LABS/clairveil` module version 또는 fork commit을 고정합니다.
2. `x/privacy` module, keeper, store key, module account permission, tx/query command wiring을 downstream app에 연결합니다.
3. `proto/clairveil/privacy/v1` service path와 generated type이 downstream API gateway와 충돌하지 않는지 확인합니다.
4. downstream denom, chain-id, fee/gas policy를 정하고 tutorial, fixtures, e2e config와 충돌하는 값을 문서화합니다.
5. production-like genesis에는 audit master public key를 설정합니다.
6. ZK artifact preflight는 release candidate와 production-like node에서 `strict`로 운영합니다.
7. downstream EVM, policy module, precompile integration test는 Clairveil repo의 smoke test와 별도로 작성합니다.

## 5. JS/TS SDK 및 web wallet 팀 수령 기준

JS/TS SDK와 web wallet 팀은 아래를 확인합니다.

1. `docs/clairveil-js-sdk-handoff-kr.md`를 기준 문서로 사용합니다.
2. `docs/schemas/clairveil-js-wallet-contract.schema.json`으로 fixture shape를 검증합니다.
3. `x/privacy/client/sdk/conformance/testdata` fixture를 SDK CI에 포함합니다.
4. `examples/js-sdk-fixture-validator`의 payload hash 재계산, relay withdraw handoff mapping, route/version 확인, prefix check를 SDK 테스트로 옮깁니다.
5. `examples/js-sdk-prover-http-client`의 timeout, bearer auth, payload hash equality check를 prover adapter 구현에 반영합니다.
6. wallet note cache, root seed derived secret, viewing key, disclosure key, prepared payload/proof JSON은 privacy-sensitive data로 분류하고 plaintext browser storage에 남기지 않습니다.
7. remote prover를 쓰는 경우 prover가 알 수 있는 metadata와 trust boundary를 사용자 UX와 threat model에 반영합니다.

## 6. Prover 운영 팀 수령 기준

Prover 운영 팀은 아래를 확인합니다.

1. `docs/clairveil-proverd-remote-production-profile-kr.md`를 기준 문서로 사용합니다.
2. remote prover를 public service, private sidecar, local daemon, browser/WASM 중 어떤 topology로 둘지 결정합니다.
3. remote deployment에는 TLS/mTLS, auth, quota, rate limit, body limit, timeout, redacted logging, health/readiness 노출 정책을 둡니다.
4. prover artifact directory는 read-only로 운영하고 checksum mismatch를 release blocker로 취급합니다.
5. proof request/response의 `payload_hash` equality check를 SDK와 server 양쪽에서 유지합니다.

## 7. Known risk와 accepted exception

현재 release 수령자가 반드시 알아야 하는 known risk는 아래입니다.

| 항목 | 상태 | 수령자 조치 |
| --- | --- | --- |
| `GO-2024-2584` | Cosmos SDK no-fixed-version actionable finding으로 `govulncheck` policy에서 명시 accept | downstream production risk register에서 재평가하고 upstream fixed path가 나오면 dependency alignment를 다시 수행합니다. |
| `GO-2026-4479` | Cosmos SDK/CometBFT server stack을 통해 reachable한 pion/dtls v2 no-fixed-version actionable finding으로 `govulncheck` policy에서 명시 accept | downstream production risk register에서 재평가하고 upstream fixed path가 나오면 dependency alignment를 다시 수행합니다. |
| Audit master private key custody | Clairveil repo는 public key config와 decode flow만 제공 | downstream project가 HSM/KMS, access control, rotation, incident response를 소유합니다. |
| Wallet local storage | reference CLI는 `0600` plaintext JSON을 사용 | web wallet/production wallet은 encrypted storage와 telemetry redaction을 구현합니다. |
| Remote prover metadata exposure | remote prover는 proof input metadata를 볼 수 있음 | user privacy UX와 deployment threat model에 remote prover를 trusted component로 포함합니다. |
| ZK artifact provenance | repo는 checksum/preflight tooling을 제공하지만 ceremony/release signing 정책은 downstream responsibility | production release에서는 artifact signing, provenance, reproducibility policy를 별도로 둡니다. |

## 8. Handoff 완료 기준

Release handoff는 아래를 만족하면 완료로 봅니다.

1. Maintainer가 `make release-check`를 통과시켰습니다.
2. Maintainer가 `make release-pack`과 `make release-pack-verify`를 통과시킨 archive/checksum을 전달했습니다.
3. Core chain 팀이 downstream app import/fork 기준 commit과 module wiring plan을 확정했습니다.
4. JS/TS SDK 팀이 fixture와 JSON Schema를 자기 CI에 가져갔습니다.
5. Web wallet 팀이 wallet storage encryption과 prover topology를 설계 문서에 반영했습니다.
6. Prover 운영 팀이 remote/local prover production profile을 선택했습니다.
7. Security/operations 팀이 accepted vulnerability, audit key custody, ZK artifact provenance를 risk register에 올렸습니다.

이 문서는 release package를 대신하는 압축 파일이 아닙니다. 대신 release commit을 넘겨받는 팀들이 같은 commit, 같은 fixture, 같은 schema, 같은 verification command를 기준으로 통합을 시작하게 만드는 handoff index입니다.
