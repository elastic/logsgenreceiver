.DEFAULT_GOAL := all

OCB_VERSION := 0.139.0
OS           := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH         := $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

MODULE_DIR := logsgenreceiver

.PHONY: all
all: tidy test build

.PHONY: install-ocb
install-ocb:
	curl --proto '=https' --tlsv1.2 -fL -o ocb \
	https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/cmd%2Fbuilder%2Fv$(OCB_VERSION)/ocb_$(OCB_VERSION)_$(OS)_$(ARCH)
	chmod +x ocb

.PHONY: build
build:
	./ocb --config builder-config.yaml

.PHONY: test
test:
	cd $(MODULE_DIR) && go test -race -count=1 ./...

.PHONY: bench
bench:
	cd $(MODULE_DIR) && go test -run=^$$ -bench=BenchmarkLogsGenReceiver -benchtime=1x -timeout=10m -v .

.PHONY: tidy
tidy:
	cd $(MODULE_DIR) && go mod tidy

.PHONY: check-tidy
check-tidy: tidy
	@git diff --exit-code $(MODULE_DIR)/go.mod $(MODULE_DIR)/go.sum || \
		(echo "ERROR: go.mod/go.sum are not tidy. Run 'make tidy' and commit." && exit 1)

.PHONY: vet
vet:
	cd $(MODULE_DIR) && go vet ./...

.PHONY: lint
lint:
	cd $(MODULE_DIR) && golangci-lint run ./...

.PHONY: generate
generate:
	cd $(MODULE_DIR) && go generate ./...

.PHONY: install
install: build
	cp ./logsgen-dev/logsgen $(GOBIN)/logsgenreceiver

.PHONY: run
run: install
	./logsgen-dev/logsgen --config ./otelcol.dev.yaml

.PHONY: update-otel-version
update-otel-version:
ifndef NEW_VERSION
	$(error Usage: make update-otel-version NEW_VERSION=x.y.z)
endif
	@echo "Updating OTel version: $(OCB_VERSION) -> $(NEW_VERSION)"
	sed -i.bak 's/$(OCB_VERSION)/$(NEW_VERSION)/g' builder-config.yaml .github/workflows/ci.yaml README.md
	sed -i.bak 's/^OCB_VERSION := .*/OCB_VERSION := $(NEW_VERSION)/' Makefile
	rm -f builder-config.yaml.bak .github/workflows/ci.yaml.bak README.md.bak Makefile.bak
	@echo "Done. Review changes with: git diff"

.PHONY: clean
clean:
	rm -f ocb
	rm -rf ./logsgen-dev/*

.PHONY: help
help:
	@echo "Targets:"
	@echo "  all          - tidy, test, build (default)"
	@echo "  install-ocb  - download the OTel collector builder"
	@echo "  build        - build the custom collector with ocb"
	@echo "  test         - run unit tests with race detector"
	@echo "  bench        - run benchmarks"
	@echo "  tidy         - go mod tidy"
	@echo "  check-tidy   - verify go.mod/go.sum are tidy (for CI)"
	@echo "  vet          - go vet"
	@echo "  lint         - golangci-lint"
	@echo "  generate     - go generate"
	@echo "  install      - build and copy binary to GOBIN"
	@echo "  run          - build, install, and run with dev config"
	@echo "  update-otel-version - update OTel version (NEW_VERSION=x.y.z)"
	@echo "  clean        - remove build artifacts"
	@echo "  help         - show this help"
