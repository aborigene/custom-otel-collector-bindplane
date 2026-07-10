# Makefile — build, test, and image targets for the fieldcrypto lab.

MODULE      := github.com/aborigene/custom-otel-collector-bindplane
OCB_VERSION := v0.155.0
IMG_PREFIX  ?= fieldcrypto
IMG_TAG     ?= dev
GO_BIN_DIR  := $(shell if [ -n "$$(go env GOBIN)" ]; then printf %s "$$(go env GOBIN)"; else printf %s "$$(go env GOPATH)/bin"; fi)
BUILDER     := $(GO_BIN_DIR)/builder

.PHONY: all test race bench vet build tidy \
        collector decryptor loggen images \
        ocb-install collector-binary run-collector-local clean

all: test build

## ── Go ──────────────────────────────────────────────────────────────────────────
test:            ## run unit tests
	go test ./...

race:            ## run tests with the race detector
	go test -race ./...

bench:           ## run the CPF benchmark
	go test -run '^$$' -bench BenchmarkIsValidCPF -benchmem ./fieldcryptoprocessor/

vet:
	go vet ./...

tidy:
	go mod tidy

build:           ## build the CLIs into ./bin
	go build -o bin/decryptor ./cmd/decryptor
	go build -o bin/loggen ./cmd/loggen

## ── Collector (ocb) ───────────────────────────────────────────────────────────────
ocb-install:     ## install the OpenTelemetry Collector Builder pinned to the manifest
	GOBIN="$(GO_BIN_DIR)" go install go.opentelemetry.io/collector/cmd/builder@$(OCB_VERSION)

collector-binary: ocb-install ## build the collector binary via ocb into ./_build
	$(BUILDER) --config build/manifest.yaml

run-collector-local: collector-binary ## run the collector locally with a local keystore
	mkdir -p .keys
	sed 's#/var/keys#$(CURDIR)/.keys#' deploy/configmap-collector.yaml \
	  | awk '/config.yaml: \|/{flag=1;next} flag{sub(/^    /,"");print}' > .collector-config.yaml
	./_build/custom-otel-collector-bindplane --config .collector-config.yaml

## ── Images ─────────────────────────────────────────────────────────────────────────
collector:       ## build the collector image (context = repo root)
	docker build -f build/Dockerfile.collector -t $(IMG_PREFIX)-collector:$(IMG_TAG) .

decryptor:
	docker build -f build/Dockerfile.decryptor -t $(IMG_PREFIX)-decryptor:$(IMG_TAG) .

loggen:
	docker build -f build/Dockerfile.loggen -t $(IMG_PREFIX)-loggen:$(IMG_TAG) .

images: collector decryptor loggen ## build all three images

clean:
	rm -rf bin _build .keys .collector-config.yaml
