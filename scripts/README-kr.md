# Scripts

이 디렉터리는 Clairveil 개발, 검증, release handoff에 쓰는 반복 가능한 script entrypoint를 담습니다.

## Script 목록

아래 audit-field prover-load entry는 V2 contract를 대상으로 합니다. Legacy entry는 static, historical, capacity-planning 전용으로 명시합니다. 현재 checkout에는 end-to-end native V2 node smoke를 제공한다고 주장하는 script가 없습니다.

- `docs-check.sh`: CommonMark/GFM AST로 tracked/new working-tree Markdown link와 fragment, top-level 문서 언어 pair와 index, plan archive 경계, `HEAD`에서 도달 가능한 exact-SemVer annotated commit tag와 실제 changelog heading, 문서 위치, release-pack manifest, packed-link closure를 검증합니다.
- `markdown-ast.go`: 문서 및 release 검사가 사용하는 Goldmark CommonMark/GFM link/heading AST를 제공합니다. Unit test는 multiline reference, 중첩 괄호, fragment, comment, code fence를 다룹니다.
- `generate-proto.sh`: `proto/clairveil/privacy/v1`과 `proto/clairveil/privacy/v2` 양쪽에서 privacy protobuf와 gRPC Gateway Go file을 재생성합니다.
- `install-binaries.sh`: `make build`로 만든 project binary 일곱 개(`clairveild`, `clairveil-setup`, legacy-only `clairveil-verify`, `clairveil-auditor`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`)를 Go install 경로에 복사합니다. Verify helper는 현행 audit-field flow에 속하지 않습니다.
- `govulncheck-with-policy.sh`: `govulncheck`를 실행하고 repo vulnerability exception policy를 적용합니다.
- `privacy-batch-joinsplit-localnet.sh`: node/prover를 시작하지 않고 static legacy batch fixture와 SDK conformance를 검증합니다. 0이 아닌 `RUN_LOCALNET`은 검증 전에 거절하며 live V2 증적에는 별도의 명시적 native harness가 필요합니다.
- `privacy-bench.sh`: privacy circuit benchmark를 실행하고 structured JSON/Markdown report를 생성합니다.
- `privacy-proverd-bench.sh`: in-process prover HTTP transport benchmark를 실행합니다.
- `privacy-proverd-load-bench.sh`: 기본 profile은 `audit_field_only`입니다. complete V2 witness request인 `PROVERLOAD_AUDIT_REQUEST`를 설정하고 이를 보관하거나 log에 남기면 안 됩니다. 도구는 HTTP response version/circuit/artifact/PI23 framing을 검사하며 current transaction CLI가 exact-artifact 암호 검증을 수행합니다.
- `privacy-proverd-scale-bench.sh`: pool 측정용 기본값으로 external prover load benchmark를 실행하고 `privacy-proverd-scale` report를 생성합니다. comma-separated `PROVERD_URLS`가 필요하며 unhealthy endpoint 제외 모드를 기본으로 켭니다. 단 public claim eligibility는 `unhealthy_endpoint_count=0`일 때만 통과합니다.
- `privacy-bulk-transfer-bench.sh`: chunk size, prover 수, tx/sec 계획을 위한 synthetic bulk payroll 처리량 summary를 생성합니다.
- `reference-payroll-demo.sh`: local file state 위에서 reference payroll product를 simulated daemon까지 실행합니다.
- `reference-payroll-rehearsal.sh`: 1천건, 1만건, 10만건, 100개 회사 x 1천건 legacy capacity-planning simulation을 생성합니다. 0이 아닌 `RUN_LOCALNET`을 거절하며 live 증적을 만들지 않습니다.
- `reservation-sql-integration.sh`: Payroll reservation store를 SQLite와 PostgreSQL에서 검증합니다. `CLAIRVEIL_TEST_POSTGRES_DSN`을 사용하거나 임시 PostgreSQL 17 Docker container를 시작합니다.
- `privacy-bulk-readiness-check.sh`: critical unit/reservation check와 synthetic bulk planning benchmark를 실행합니다. 0이 아닌 `RUN_LOCALNET`은 거절하며 `RUN_PROVER_SCALE=1`과 `PROVERD_URLS`를 통한 optional external prover-scale 검증은 유지합니다.
- `privacy-public-capacity-report.sh`: explicit component report를 병합하며, 기본값은 current prover scale/load report만 선택합니다. 제거된 V1 localnet/latency report family는 자동 선택하지 않습니다.
- `privacy-benchmark-report.sh`: current default family를 합쳐 `benchmarks/clairveil-benchmark-results-report-kr.md`를 생성합니다. Historical report는 `SUMMARY_REPORTS`를 명시해서만 확인합니다.
- `release-pack.sh`: caller umask와 무관한 deterministic Git-derived file mode와 canonical directory/metadata mode로 downstream handoff tarball과 외부 sha256 파일을 `dist/` 아래 생성합니다. Final release는 packed commit의 annotated exact-SemVer tag를 요구하고 untagged clean commit은 CI용 non-publishable `snapshot-<full-sha>` identity를 사용합니다.
- `release-pack-verify.sh`: tag-or-snapshot commit binding, release tag의 paired changelog heading, canonical/safe raw tar member, exact selected Git file set, canonical manifest, checksum, 필수 파일, 모든 packed Git blob과 raw/extracted Git-derived exact permission mode를 검증합니다. Default verify는 clean tree를 요구하고 기존 archive/checksum pair를 재사용합니다. Explicit input은 미리 존재해야 하고 재생성되지 않으며 exact lowercase 40-character `RELEASE_PACK_EXPECTED_COMMIT`이 필요합니다.
- `prepare-joinsplit-artifact-rotation-evidence.sh`: Clean tree에서 source-bound rotation evidence용 previous/current JoinSplit artifact directory를 repository 밖에 준비합니다.
- `validate-joinsplit-artifact-rotation-evidence.sh`: 제공하거나 새로 준비한 artifact set으로 exact JoinSplit artifact rotation, fresh-genesis, regression evidence gate를 실행합니다.
- `docker-proverd-build.sh`: prover compose file을 검증하고 reference prover Docker image를 build/inspect합니다.
