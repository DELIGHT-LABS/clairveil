SHELL := /bin/bash

.PHONY: test
test:
	go test ./...

.PHONY: build
build:
	go build ./cmd/clairveild
	go build ./cmd/clairveil-setup
	go build ./cmd/clairveil-verify
	go build ./cmd/clairveil-auditor
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
	@echo "Use 'clairveild init --audit-config <path> --chain-id <id>' to initialize a node."

.PHONY: proto
proto:
	./scripts/generate-proto.sh

.PHONY: privacy-batch-joinsplit-localnet
privacy-batch-joinsplit-localnet:
	./scripts/privacy-batch-joinsplit-localnet.sh

.PHONY: privacy-bench
privacy-bench:
	./scripts/privacy-bench.sh

.PHONY: privacy-proverd-bench
privacy-proverd-bench:
	./scripts/privacy-proverd-bench.sh

.PHONY: privacy-proverd-load-bench
privacy-proverd-load-bench:
	./scripts/privacy-proverd-load-bench.sh

.PHONY: privacy-proverd-scale-bench
privacy-proverd-scale-bench:
	./scripts/privacy-proverd-scale-bench.sh

.PHONY: privacy-bulk-transfer-bench
privacy-bulk-transfer-bench:
	./scripts/privacy-bulk-transfer-bench.sh

.PHONY: privacy-bulk-readiness-check
privacy-bulk-readiness-check:
	./scripts/privacy-bulk-readiness-check.sh

.PHONY: reference-payroll-demo
reference-payroll-demo:
	./scripts/reference-payroll-demo.sh

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
	$(MAKE) privacy-batch-joinsplit-localnet
	$(MAKE) privacy-bulk-readiness-check

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
	rm -f clairveild clairveil-setup clairveil-verify clairveil-auditor clairveil-proverd clairveil-benchreport clairveil-proverload clairveil-localnetload clairveil-userlatency clairveil-bulktransferbench clairveil-payroll clairveil-payrolld
