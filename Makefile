all: install-race

install:
	go install -v ./...
	go test -v -i ./...

install-race:
	go install -v -race ./...
	go test -v -race -i ./...

run: install _run

run-race: install-race _run

_run:
	pmm-gateway
