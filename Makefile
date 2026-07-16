GO_VERSION := 1.26.5
BINARY := xai-proxy
PKG := ./cmd/xai-proxy
VERSION ?= 0.1.0
IMAGE ?= xai-proxy:local

.PHONY: check-go build test vet clean docker-build docker-up docker-login docker-down

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

# --- Docker ---
docker-build:
	docker build -t $(IMAGE) \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg VERSION=$(VERSION) \
		.

docker-login:
	docker compose run --rm xai-proxy login --no-browser

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
