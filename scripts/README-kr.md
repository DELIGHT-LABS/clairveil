# Scripts

이 디렉터리는 Clairveil 개발, 검증, release handoff에 쓰는 반복 가능한 script entrypoint를 담습니다.

## Script 목록

- `generate-proto.sh`: `proto/clairveil/privacy/v1`에서 privacy protobuf와 gRPC Gateway Go file을 재생성합니다.
- `install-binaries.sh`: `make build`로 만든 `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd`를 Go install 경로에 복사합니다.
- `init-localnet.sh`: 기존 home을 timestamp backup으로 보관하고, 기본 local chain genesis, test keys, audit pubkey, ZK artifact를 준비합니다.
- `govulncheck-with-policy.sh`: `govulncheck`를 실행하고 repo vulnerability exception policy를 적용합니다.
- `localnet-smoke.sh`: `clairveild`를 build하고 임시 local validator genesis를 만든 뒤 node start와 block commit을 짧게 검증합니다.
- `privacy-e2e-smoke.sh`: deposit, transfer, disclosure decode, direct withdraw, relayed withdraw까지 local privacy flow 전체를 검증합니다.
- `release-pack.sh`: downstream handoff tarball과 외부 sha256 파일을 `dist/` 아래 생성합니다.
- `release-pack-verify.sh`: handoff tarball checksum, 내부 `SHA256SUMS.txt`, 필수 파일, manifest commit을 검증합니다.
- `docker-proverd-build.sh`: prover compose file을 검증하고 reference prover Docker image를 build/inspect합니다.
