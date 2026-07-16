# xai-proxy — Development & Security Guide

Hard rules for humans and AI assistants working in `xai-proxy/`. These take
precedence over informal habits.

Product behavior, paths, OAuth, and CLI: see [docs/](docs/).

**Documentation language:** all docs and code comments are English. Chinese
product docs use parallel `*_zh.md` files (this guide is English-only).

## Development style

1. **Language and toolchain**
   - Go **1.26.5** (`go.mod` must say `go 1.26.5`)
   - Prefer `make build` / `make test` / `make vet`; changes must pass `go test ./...`

2. **HTTP stack**
   - Standard library `net/http` only (Server / Client / ServeMux)
   - Do not introduce chi, echo, gin, fiber, gorilla/mux, fasthttp, etc.

3. **Dependencies**
   - Default: **zero third-party** deps; if stdlib can do it, do not add a module
   - If a dependency is required: prefer `golang.org/x/*`, then well-maintained de-facto standard libraries
   - Outbound SOCKS may use `golang.org/x/net`
   - New `require` lines must explain why stdlib is insufficient and the maintenance status

4. **Code organization**
   - Respect existing `internal/*` boundaries; do not duplicate responsibilities across packages
   - Pass-through proxy: do not rewrite request/response body shapes; no protocol translation layers
   - Streaming (SSE / binary / multipart) must not be incorrectly fully buffered or rewritten
   - Config and on-disk state go through `internal/store` and `XAI_PROXY_HOME`; no scattered hard-coded paths

5. **Change hygiene**
   - Touch only files needed for the task; no drive-by refactors or unrelated doc expansion
   - Behavior changes need tests (`*_test.go`); security-sensitive logic (auth compare, host pin, token write-back) must have coverage
   - User-facing errors should be readable; log details still follow the security rules below

## Security guidelines

1. **Secrets and tokens**
   - Never log or echo `access_token`, `refresh_token`, or client keys (including in samples committed to the repo)
   - On-disk secrets: directory `0700`, files `0600`; atomic write-back to avoid truncated files
   - Keep client keys separate from upstream OAuth credentials; after inbound auth succeeds, strip client auth headers, then attach the upstream credential

2. **Authentication and comparison**
   - Compare secrets with **constant-time** (or equivalent) comparison
   - Business API routes must not be unauthenticated by default; probe routes, if open, must be explicit and minimal
   - Do not add hidden “skip auth for debugging” flags or env backdoors

3. **Network boundary**
   - Default listen address is loopback only; non-loopback binds require an explicit, hard-to-misclick confirmation
   - Upstream URL hosts must be pinned to expected domains over `https`; do not follow unvalidated redirects to arbitrary hosts
   - Outbound proxy URLs may contain credentials: never log them

4. **Concurrency and credential rotation**
   - Single-use refresh (and similar) credentials: serialize refresh (lock / singleflight); do not discard old state before a successful write-back
   - Distinguish terminal failures from transient / entitlement failures to avoid wiping credentials or refresh loops

5. **Supply chain and surface**
   - Do not vendor unrelated large runtimes; do not copy external project trees into this repo as dependencies
   - Do not add debug endpoints, default-exposed pprof, or unauthenticated admin interfaces that expand attack surface
