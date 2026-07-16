GO_VERSION := 1.26.5
BINARY := xai-proxy
PKG := ./cmd/xai-proxy
VERSION ?= 0.1.0
IMAGE ?= xai-proxy:local

.PHONY: check-go build test vet clean container-build container-run container-stop container-login

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

# --- Apple Container / OCI ---
# Requires: container system start; container builder start (first build)
container-build:
	container build -t $(IMAGE) -f Dockerfile \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg VERSION=$(VERSION) \
		.

DATA_DIR ?= $(HOME)/.xai-proxy-container

container-login:
	mkdir -p "$(DATA_DIR)" && chmod 700 "$(DATA_DIR)"
	container run --rm -it --name xai-proxy-login \
		--volume "$(DATA_DIR):/data" \
		--env XAI_PROXY_HOME=/data \
		$(IMAGE) login --no-browser

container-run:
	-container stop xai-proxy 2>/dev/null
	-container delete xai-proxy 2>/dev/null
	mkdir -p "$(DATA_DIR)" && chmod 700 "$(DATA_DIR)"
	container run -d --name xai-proxy \
		--publish 8645:8645 \
		--volume "$(DATA_DIR):/data" \
		--env XAI_PROXY_HOME=/data \
		$(IMAGE)

container-stop:
	-container stop xai-proxy
	-container delete xai-proxy
