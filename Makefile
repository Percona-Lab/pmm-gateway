all: install test-race

install:
	go install -v ./...
	go test -v -i ./...

install-race:
	go install -v -race ./...
	go test -v -race -i ./...

test: install
	go test -v ./...

test-race: install-race
	go test -v -race ./...

run: install _run

run-race: install-race _run

_run:
	pmm-gateway
