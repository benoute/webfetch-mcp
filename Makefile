.PHONY: build test race vet fmt fmt-check vuln fix check clean

build:
	go build -o webfetch-mcp ./cmd/webfetch-mcp

test:
	go test -v ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Show `go fix` modernizer suggestions without applying them (not a CI gate).
fix:
	go fix -diff ./...

# Same gates as CI (minus the go-version drift check).
check: fmt-check vet race vuln

clean:
	rm -f webfetch-mcp
