BINARY := parse
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test fmt vet check clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

# check is the full gate: format, vet, tests, build, install. Run it before
# calling anything done, so the installed binary is never behind the source.
check: fmt vet test build
	cp $(BINARY) $(HOME)/.local/bin/$(BINARY)

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
