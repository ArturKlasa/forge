BINARY   := forge
CMD_PATH := ./cmd/forge
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -X github.com/arturklasa/forge/internal/version.Version=$(VERSION)

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: build test lint run release clean-dist

## build: compile the forge binary into the project root
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD_PATH)

## test: run all tests
test:
	go test ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## run: build and run the forge binary (pass ARGS="..." to supply arguments)
run: build
	./$(BINARY) $(ARGS)

## release: build cross-platform binaries into dist/
release: clean-dist
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		output=dist/forge_$${os}_$${arch}; \
		[ "$$os" = "windows" ] && output=$${output}.exe; \
		echo "Building $$output ..."; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$output $(CMD_PATH) || exit 1; \
	done
	@cd dist && sha256sum forge_* > SHA256SUMS
	@echo "Release binaries written to dist/"

## clean-dist: remove dist/ directory
clean-dist:
	rm -rf dist/
