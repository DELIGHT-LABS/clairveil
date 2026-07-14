SHELL := /bin/bash

.PHONY: test
test:
	go test ./...

.PHONY: build
build:
	go build ./cmd/clairveild
	go build ./cmd/clairveil-setup
	go build ./cmd/clairveil-verify
	go build ./cmd/clairveil-proverd
	go build ./cmd/clairveil-benchreport
	go build ./cmd/clairveil-proverload
	go build ./cmd/clairveil-localnetload
	go build ./cmd/clairveil-userlatency
	go build ./cmd/clairveil-bulktransferbench
	go build ./cmd/clairveil-payroll
	go build ./cmd/clairveil-payrolld

.PHONY: install
install: build
	./scripts/install-binaries.sh

.PHONY: init
init: install
	./scripts/init-localnet.sh

.PHONY: proto
proto:
	./scripts/generate-proto.sh

.PHONY: localnet-smoke
localnet-smoke:
	./scripts/localnet-smoke.sh

.PHONY: privacy-e2e-smoke
privacy-e2e-smoke:
	./scripts/privacy-e2e-smoke.sh

.PHONY: privacy-batch-joinsplit-localnet
privacy-batch-joinsplit-localnet:
	./scripts/privacy-batch-joinsplit-localnet.sh

.PHONY: dapp-local
dapp-local:
	./scripts/dapp-local.sh

.PHONY: privacy-bench
privacy-bench:
	./scripts/privacy-bench.sh

.PHONY: privacy-bench-localnet
privacy-bench-localnet:
	./scripts/privacy-bench-localnet.sh

.PHONY: privacy-localnet-tps-bench
privacy-localnet-tps-bench:
	./scripts/privacy-localnet-tps-bench.sh

.PHONY: privacy-transfer-batch-localnet-bench
privacy-transfer-batch-localnet-bench:
	./scripts/privacy-transfer-batch-localnet-bench.sh

.PHONY: privacy-proverd-bench
privacy-proverd-bench:
	./scripts/privacy-proverd-bench.sh

.PHONY: privacy-proverd-load-bench
privacy-proverd-load-bench:
	./scripts/privacy-proverd-load-bench.sh

.PHONY: privacy-proverd-scale-bench
privacy-proverd-scale-bench:
	./scripts/privacy-proverd-scale-bench.sh

.PHONY: privacy-user-latency-bench
privacy-user-latency-bench:
	./scripts/privacy-user-latency-bench.sh

.PHONY: privacy-bulk-transfer-bench
privacy-bulk-transfer-bench:
	./scripts/privacy-bulk-transfer-bench.sh

.PHONY: privacy-bulk-readiness-check
privacy-bulk-readiness-check:
	./scripts/privacy-bulk-readiness-check.sh

.PHONY: reference-payroll-demo
reference-payroll-demo:
	./scripts/reference-payroll-demo.sh

.PHONY: reference-payroll-live-localnet
reference-payroll-live-localnet:
	./scripts/reference-payroll-live-localnet.sh

.PHONY: reservation-sql-integration
reservation-sql-integration:
	./scripts/reservation-sql-integration.sh

.PHONY: reference-payroll-rehearsal
reference-payroll-rehearsal:
	./scripts/reference-payroll-rehearsal.sh

.PHONY: privacy-public-capacity-report
privacy-public-capacity-report:
	./scripts/privacy-public-capacity-report.sh

.PHONY: privacy-benchmark-report
privacy-benchmark-report:
	./scripts/privacy-benchmark-report.sh

.PHONY: examples
examples:
	npm --prefix examples/audit-disclosure-keys test
	npm --prefix examples/js-sdk-fixture-validator run validate
	npm --prefix examples/js-sdk-prover-http-client run demo
	npm --prefix examples/clairveil-dapp ci
	npm --prefix examples/clairveil-dapp run check:dapp
	npm --prefix examples/clairveil-dapp run test:dapp
	npm --prefix examples/clairveil-dapp run check:clairveiljs
	npm --prefix examples/clairveil-dapp run test:clairveiljs

.PHONY: vulncheck
vulncheck:
	./scripts/govulncheck-with-policy.sh

.PHONY: docs-check
docs-check:
	./scripts/docs-check.sh

.PHONY: check
check: docs-check test build examples

.PHONY: ci
ci: check

.PHONY: release-check
release-check:
	$(MAKE) ci
	$(MAKE) vulncheck
	$(MAKE) localnet-smoke
	$(MAKE) privacy-e2e-smoke
	$(MAKE) privacy-batch-joinsplit-localnet
	RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 $(MAKE) privacy-bulk-readiness-check

.PHONY: release-pack
release-pack:
	./scripts/release-pack.sh

.PHONY: release-pack-verify
release-pack-verify:
	./scripts/release-pack-verify.sh

.PHONY: validate-joinsplit-artifact-rotation-evidence
validate-joinsplit-artifact-rotation-evidence:
	./scripts/validate-joinsplit-artifact-rotation-evidence.sh

.PHONY: docker-proverd-build
docker-proverd-build:
	./scripts/docker-proverd-build.sh

.PHONY: clean
clean:
	rm -f clairveild clairveil-setup clairveil-verify clairveil-proverd clairveil-benchreport clairveil-proverload clairveil-localnetload clairveil-userlatency clairveil-bulktransferbench clairveil-payroll clairveil-payrolld
