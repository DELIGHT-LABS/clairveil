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

.PHONY: examples
examples:
	npm --prefix examples/js-sdk-fixture-validator run validate
	npm --prefix examples/js-sdk-prover-http-client run demo

.PHONY: vulncheck
vulncheck:
	./scripts/govulncheck-with-policy.sh

.PHONY: check
check: test build examples

.PHONY: ci
ci: check

.PHONY: release-check
release-check:
	$(MAKE) ci
	$(MAKE) vulncheck
	$(MAKE) localnet-smoke
	$(MAKE) privacy-e2e-smoke

.PHONY: release-pack
release-pack:
	./scripts/release-pack.sh

.PHONY: release-pack-verify
release-pack-verify:
	./scripts/release-pack-verify.sh

.PHONY: docker-proverd-build
docker-proverd-build:
	./scripts/docker-proverd-build.sh

.PHONY: clean
clean:
	rm -f clairveild clairveil-setup clairveil-verify clairveil-proverd
