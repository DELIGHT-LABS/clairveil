# Scripts

이 디렉터리는 Clairveil 개발, 검증, release handoff에 쓰는 반복 가능한 script entrypoint를 담습니다.

## Script 목록

- `generate-proto.sh`: `proto/clairveil/privacy/v1`에서 privacy protobuf와 gRPC Gateway Go file을 재생성합니다.
- `install-binaries.sh`: `make build`로 만든 `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd`를 Go install 경로에 복사합니다.
- `init-localnet.sh`: 기존 home을 timestamp backup으로 보관하고, 기본 local chain genesis, test keys, audit pubkey, ZK artifact를 준비합니다.
- `govulncheck-with-policy.sh`: `govulncheck`를 실행하고 repo vulnerability exception policy를 적용합니다.
- `localnet-smoke.sh`: `clairveild`를 build하고 임시 local validator genesis를 만든 뒤 node start와 block commit을 짧게 검증합니다.
- `privacy-e2e-smoke.sh`: deposit, transfer, disclosure decode, direct withdraw, relayed withdraw까지 local privacy flow 전체를 검증합니다.
- `privacy-batch-joinsplit-localnet.sh`: 기본값으로 Session 3B 16x32 fixture를 검증하고, `RUN_LOCALNET=1`이면 실제 node와 `clairveil-proverd`를 시작해 단계형 one-proof command로 1/1, mixed disclosure 3/4, 31+change, exact32, padding, 실제 process restart/retry, non-zero-height genesis export/import, typed cursor/path continuation, reserve/asset/wallet round trip을 실행합니다. `grpcurl`이 필요합니다. 이미 검증한 development artifact를 재사용하려면 `CLAIRVEIL_BATCH_ARTIFACT_DIR`를 지정합니다.
- `privacy-bench.sh`: privacy circuit benchmark를 실행하고 structured JSON/Markdown report를 생성합니다.
- `privacy-proverd-bench.sh`: in-process prover HTTP transport benchmark를 실행합니다.
- `privacy-proverd-load-bench.sh`: `PROVERD_URL`로 이미 실행 중인 external `clairveil-proverd` 1개를 측정하거나, `PROVERD_URLS`로 round-robin prover pool을 측정합니다. `PROVERLOAD_ALLOW_UNHEALTHY_ENDPOINTS=1`을 설정하면 preflight 실패 endpoint를 제외하고 unhealthy endpoint count를 기록합니다.
- `privacy-proverd-scale-bench.sh`: pool 측정용 기본값으로 external prover load benchmark를 실행하고 `privacy-proverd-scale` report를 생성합니다. comma-separated `PROVERD_URLS`가 필요하며 unhealthy endpoint 제외 모드를 기본으로 켭니다. 단 public claim eligibility는 `unhealthy_endpoint_count=0`일 때만 통과합니다.
- `privacy-bench-localnet.sh`: localnet privacy smoke를 실행하고 fee, gas, reserve, localnet summary를 생성합니다.
- `privacy-localnet-tps-bench.sh`: localnet smoke output을 `chain_tps` benchmark family로 변환합니다.
- `privacy-transfer-batch-localnet-bench.sh`: localnet smoke에 multi-message `transfer-batch` tx를 추가로 켜서 실행합니다. `TRANSFER_BATCH_COUNT=N`, `TRANSFER_BATCH_AMOUNT=A`로 batch envelope 크기를 바꿀 수 있습니다.
- `privacy-bulk-transfer-bench.sh`: chunk size, prover 수, tx/sec 계획을 위한 synthetic bulk payroll 처리량 summary를 생성합니다.
- `reference-payroll-demo.sh`: local file state 위에서 reference payroll product를 simulated daemon까지 실행합니다.
- `reference-payroll-live-localnet.sh`: 실제 localnet에서 payroll input, reservation, `transfer-batch`, recipient scan, settle, final report를 끝까지 실행합니다.
- `reference-payroll-rehearsal.sh`: 1천건, 1만건, 10만건, 100개 회사 x 1천건 reference payroll capacity simulation report를 생성하고, 옵션으로 작은 live localnet smoke를 함께 실행합니다.
- `privacy-bulk-readiness-check.sh`: bulk transfer production-readiness check를 실행합니다. critical unit test, reservation failure invariant, synthetic bulk bench를 기본 실행하고, localnet/prover-pool 검증은 옵션으로 켤 수 있습니다.
- `privacy-user-latency-bench.sh`: localnet privacy smoke를 wallet-flow latency tracing enabled 상태로 실행하고 `privacy-user-latency` report를 생성합니다. `USER_LATENCY_REPEAT=N`으로 반복 sample을 모을 수 있으며, `RUN_PROFILE=public_claim`은 blocked dry run override가 없으면 최소 100회 반복을 요구합니다.
- `privacy-public-capacity-report.sh`: component report를 public capacity aggregate로 병합하고, component 또는 claim별 evidence가 public gate를 통과하지 못하면 aggregate도 ineligible 상태로 남깁니다. prover report가 둘 다 있으면 기본 입력은 alternative `prover_rps` evidence 충돌을 피하기 위해 `privacy-proverd-load`보다 `privacy-proverd-scale`을 우선합니다.
- `privacy-benchmark-report.sh`: family별 `latest.json`을 합쳐 사람이 한 문서로 읽을 수 있는 `benchmarks/clairveil-benchmark-results-report-kr.md`를 생성합니다. `privacy-public-capacity-report.sh`는 기본적으로 이 script를 마지막에 호출하며, `GENERATE_HUMAN_BENCHMARK_REPORT=0`으로 끌 수 있습니다.
- `release-pack.sh`: downstream handoff tarball과 외부 sha256 파일을 `dist/` 아래 생성합니다.
- `release-pack-verify.sh`: canonical/safe tar member, exact selected Git file set, canonical manifest, checksum, 필수 파일, 모든 packed Git blob을 검증합니다. Explicit archive에는 exact lowercase 40-character `RELEASE_PACK_EXPECTED_COMMIT`이 필요합니다.
- `docker-proverd-build.sh`: prover compose file을 검증하고 reference prover Docker image를 build/inspect합니다.
