.PHONY: build build-all test clean run fmt vet

build:
	go build -o webfetch-mcp ./cmd/webfetch-mcp

test:
	go test -v ./...

clean:
	rm -f webfetch-mcp

fmt:
	go fmt ./...

vet:
	go vet ./...
