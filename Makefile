GO_VERSION := 1.26.5
BINARY := xai-proxy
PKG := ./cmd/xai-proxy
VERSION ?= 0.1.0

.PHONY: check-go build test vet clean

check-go:
	@v=$$(go env GOVERSION | sed 's/^go//'); \
	if [ "$$v" != "$(GO_VERSION)" ]; then \
		echo "error: require Go $(GO_VERSION), got $$v"; \
		echo "hint: export GOTOOLCHAIN=go$(GO_VERSION)"; \
		exit 1; \
	fi

build: check-go
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) $(PKG)

test: check-go
	go test ./...

vet: check-go
	go vet ./...

clean:
	rm -f $(BINARY)
