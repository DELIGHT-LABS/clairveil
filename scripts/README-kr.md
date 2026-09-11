# Scripts

이 디렉터리는 Clairveil 개발, 검증, release handoff에 쓰는 반복 가능한 script entrypoint를 담습니다.

## Script 목록

아래 localnet, E2E, prover-load entry는 reviewed V2 runtime을 사용합니다. 남은 legacy entry는 static, historical, capacity-planning 전용으로 명시합니다.

- `docs-check.sh`: CommonMark/GFM AST로 tracked/new working-tree Markdown link와 fragment, top-level 문서 언어 pair와 index, plan archive 경계, `HEAD`에서 도달 가능한 exact-SemVer annotated commit tag와 실제 changelog heading, 문서 위치, release-pack manifest, packed-link closure를 검증합니다.
- `markdown-ast.go`: 문서 및 release 검사가 사용하는 Goldmark CommonMark/GFM link/heading AST를 제공합니다. Unit test는 multiline reference, 중첩 괄호, fragment, comment, code fence를 다룹니다.
- `generate-proto.sh`: `proto/clairveil/privacy/v1`에서 privacy protobuf와 gRPC Gateway Go file을 재생성합니다.
- `install-binaries.sh`: `make build`로 만든 project binary 여섯 개(`clairveild`, `clairveil-setup`, legacy-only `clairveil-verify`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`)를 Go install 경로에 복사합니다. Verify helper는 현행 typed-note flow에 속하지 않습니다.
- `init-localnet.sh`: 기존 home을 timestamp backup으로 보관하고, 기존 검토 runtime bundle·일치 artifact·offline audit-secret file에서 V2 fresh genesis home을 준비합니다. key, artifact, runtime archive를 새로 만들지 않습니다.
- `govulncheck-with-policy.sh`: `govulncheck`를 실행하고 repo vulnerability exception policy를 적용합니다.
- `localnet-smoke.sh`와 `privacy-e2e-smoke.sh`: reviewed audit-field V2 runner의 stable entrypoint입니다. 일치하는 `CLAIRVEILD_BIN`, `CLAIRVEIL_PROVERD_BIN`, `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR`, `CLAIRVEIL_AUDIT_RUNTIME_DIR`, `CLAIRVEIL_AUDIT_SECRET_FILE`가 필요하며 V2 DeliverTx와 forced rescan을 검사합니다.
- `privacy-batch-joinsplit-localnet.sh`: 기본 static fixture/conformance gate를 유지합니다. `RUN_LOCALNET=1`은 16x32 capacity workload가 아닌 작은 `transfer-batch-16x32` proof 1회를 같은 reviewed V2 runner로 실행합니다.
- `privacy-bench.sh`: privacy circuit benchmark를 실행하고 structured JSON/Markdown report를 생성합니다.
- `privacy-proverd-bench.sh`: in-process prover HTTP transport benchmark를 실행합니다.
- `privacy-proverd-load-bench.sh`: 기본 profile은 `audit_field_only`입니다. complete V2 witness request인 `PROVERLOAD_AUDIT_REQUEST`를 설정하고 이를 보관하거나 log에 남기면 안 됩니다. 도구는 HTTP response version/circuit/artifact/PI23 framing을 검사하며 exact-artifact 암호 검증은 CLI smoke가 수행합니다.
- `privacy-proverd-scale-bench.sh`: pool 측정용 기본값으로 external prover load benchmark를 실행하고 `privacy-proverd-scale` report를 생성합니다. comma-separated `PROVERD_URLS`가 필요하며 unhealthy endpoint 제외 모드를 기본으로 켭니다. 단 public claim eligibility는 `unhealthy_endpoint_count=0`일 때만 통과합니다.
- `privacy-bench-localnet.sh`: localnet privacy smoke를 실행하고 fee, gas, reserve, localnet summary를 생성합니다.
- `privacy-localnet-tps-bench.sh`: localnet smoke output을 `chain_tps` benchmark family로 변환합니다.
- `privacy-transfer-batch-localnet-bench.sh`: localnet smoke에 multi-message `transfer-batch` tx를 추가로 켜서 실행합니다. `TRANSFER_BATCH_COUNT=N`, `TRANSFER_BATCH_AMOUNT=A`로 batch envelope 크기를 바꿀 수 있습니다.
- `privacy-bulk-transfer-bench.sh`: chunk size, prover 수, tx/sec 계획을 위한 synthetic bulk payroll 처리량 summary를 생성합니다.
- `reference-payroll-demo.sh`: local file state 위에서 reference payroll product를 simulated daemon까지 실행합니다.
- `reference-payroll-live-localnet.sh`: 실제 localnet에서 payroll input, reservation, `transfer-batch`, recipient scan, settle, final report를 끝까지 실행합니다.
- `reference-payroll-rehearsal.sh`: 1천건, 1만건, 10만건, 100개 회사 x 1천건 reference payroll capacity simulation report를 생성하고, 옵션으로 작은 live localnet smoke를 함께 실행합니다.
- `reservation-sql-integration.sh`: Payroll reservation store를 SQLite와 PostgreSQL에서 검증합니다. `CLAIRVEIL_TEST_POSTGRES_DSN`을 사용하거나 임시 PostgreSQL 17 Docker container를 시작합니다.
- `privacy-bulk-readiness-check.sh`: bulk transfer production-readiness check를 실행합니다. critical unit test, reservation failure invariant, synthetic bulk bench를 기본 실행하고, localnet/prover-pool 검증은 옵션으로 켤 수 있습니다.
- `privacy-user-latency-bench.sh`: localnet privacy smoke를 wallet-flow latency tracing enabled 상태로 실행하고 `privacy-user-latency` report를 생성합니다. `USER_LATENCY_REPEAT=N`으로 반복 sample을 모을 수 있으며, `RUN_PROFILE=public_claim`은 blocked dry run override가 없으면 최소 100회 반복을 요구합니다.
- `privacy-public-capacity-report.sh`: component report를 public capacity aggregate로 병합하고, component 또는 claim별 evidence가 public gate를 통과하지 못하면 aggregate도 ineligible 상태로 남깁니다. prover report가 둘 다 있으면 기본 입력은 alternative `prover_rps` evidence 충돌을 피하기 위해 `privacy-proverd-load`보다 `privacy-proverd-scale`을 우선합니다.
- `privacy-benchmark-report.sh`: family별 `latest.json`을 합쳐 사람이 한 문서로 읽을 수 있는 `benchmarks/clairveil-benchmark-results-report-kr.md`를 생성합니다. `privacy-public-capacity-report.sh`는 기본적으로 이 script를 마지막에 호출하며, `GENERATE_HUMAN_BENCHMARK_REPORT=0`으로 끌 수 있습니다.
- `release-pack.sh`: caller umask와 무관한 deterministic Git-derived file mode와 canonical directory/metadata mode로 downstream handoff tarball과 외부 sha256 파일을 `dist/` 아래 생성합니다. Final release는 packed commit의 annotated exact-SemVer tag를 요구하고 untagged clean commit은 CI용 non-publishable `snapshot-<full-sha>` identity를 사용합니다.
- `release-pack-verify.sh`: tag-or-snapshot commit binding, release tag의 paired changelog heading, canonical/safe raw tar member, exact selected Git file set, canonical manifest, checksum, 필수 파일, 모든 packed Git blob과 raw/extracted Git-derived exact permission mode를 검증합니다. Default verify는 clean tree를 요구하고 기존 archive/checksum pair를 재사용합니다. Explicit input은 미리 존재해야 하고 재생성되지 않으며 exact lowercase 40-character `RELEASE_PACK_EXPECTED_COMMIT`이 필요합니다.
- `prepare-joinsplit-artifact-rotation-evidence.sh`: Clean tree에서 source-bound rotation evidence용 previous/current JoinSplit artifact directory를 repository 밖에 준비합니다.
- `validate-joinsplit-artifact-rotation-evidence.sh`: 제공하거나 새로 준비한 artifact set으로 exact JoinSplit artifact rotation, fresh-genesis, regression evidence gate를 실행합니다.
- `docker-proverd-build.sh`: prover compose file을 검증하고 reference prover Docker image를 build/inspect합니다.
