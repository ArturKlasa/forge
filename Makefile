BINARY   := forge
CMD_PATH := ./cmd/forge

.PHONY: build test lint run

## build: compile the forge binary into the project root
build:
	go build -o $(BINARY) $(CMD_PATH)

## test: run all tests
test:
	go test ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## run: build and run the forge binary (pass ARGS="..." to supply arguments)
run: build
	./$(BINARY) $(ARGS)
