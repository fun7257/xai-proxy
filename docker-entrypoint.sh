#!/bin/sh
# Container entrypoint for xai-proxy.
# On serve: ensure local client API key + OAuth, then exec the binary.
# Other commands (generate, login, status, …) pass through unchanged.
set -eu

BIN="${XAI_PROXY_BIN:-/usr/local/bin/xai-proxy}"
HOME_DIR="${XAI_PROXY_HOME:-/data}"

log() {
	printf '%s\n' "xai-proxy-init: $*" >&2
}

ensure_client_key() {
	if [ -s "${HOME_DIR}/client_key" ]; then
		log "client API key already configured (verifier at ${HOME_DIR}/client_key; plaintext not on disk)"
		return 0
	fi

	log "no client API key found; running generate..."
	# generate: banners on stderr, plaintext key once on stdout.
	key="$("$BIN" generate)"
	if [ -z "$key" ]; then
		log "generate failed: empty key on stdout"
		exit 1
	fi

	log "================================================================"
	log "CLIENT API KEY (shown once — save it from container logs now):"
	log "  ${key}"
	log "================================================================"
	log "Use: Authorization: Bearer <key>  on /v1/*"
	log "Disk stores a salted SHA-256 hash only; re-generate if this log is lost."
}

oauth_ready() {
	# status JSON on stdout; non-zero exit when not fully ready — ignore exit.
	out="$("$BIN" status 2>/dev/null || true)"
	printf '%s\n' "$out" | grep -q '"state"[[:space:]]*:[[:space:]]*"ready"'
}

ensure_oauth() {
	if oauth_ready; then
		log "OAuth already authorized (state=ready)"
		return 0
	fi

	log "OAuth not authorized; running login --no-browser..."
	log "Open the device URL, enter the code, then wait for this process to continue."
	"$BIN" login --no-browser

	if ! oauth_ready; then
		log "login finished but OAuth still not ready; run: xai-proxy status"
		exit 1
	fi
	log "OAuth login successful"
}

case "${1:-}" in
serve)
	ensure_client_key
	ensure_oauth
	log "starting: ${BIN} $*"
	exec "$BIN" "$@"
	;;
*)
	exec "$BIN" "$@"
	;;
esac
