.PHONY: build test fmt verify

build:
	go build ./cmd/tenantplane

test:
	go test ./...

fmt:
	go fmt ./...

verify: fmt test build

