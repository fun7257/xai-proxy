GO_VERSION := 1.26.5
BINARY := xai-proxy
PKG := ./cmd/xai-proxy
VERSION ?= 0.1.2
DIST := dist

# Cross-compile targets (same set as .github/workflows/release.yml).
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: check-go build test vet clean dist

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

# Local multi-platform archives (mirrors CI release binaries).
dist: check-go
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; archive=tar.gz; \
		if [ "$$os" = "windows" ]; then ext=".exe"; archive=zip; fi; \
		out="$(BINARY)$$ext"; \
		echo "building $$os/$$arch..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath \
				-ldflags="-s -w -X main.version=$(VERSION)" \
				-o "$$out" $(PKG) || exit 1; \
		asset="$(BINARY)_$(VERSION)_$${os}_$${arch}"; \
		if [ "$$archive" = "zip" ]; then \
			zip -9 "$(DIST)/$${asset}.zip" "$$out" >/dev/null; \
		else \
			tar -czf "$(DIST)/$${asset}.tar.gz" "$$out"; \
		fi; \
		rm -f "$$out"; \
	done
	@cd $(DIST) && shasum -a 256 $(BINARY)_* > SHA256SUMS
	@ls -la $(DIST)

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf $(DIST)
