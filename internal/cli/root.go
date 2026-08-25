package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/credential"
	"xai-proxy/internal/outbound"
	"xai-proxy/internal/proxy"
	"xai-proxy/internal/store"
)

// Version is set by main.
var Version = "0.1.2"

// Run is the CLI entrypoint.
func Run(args []string) int {
	// Global: xai-proxy --proxy URL <cmd> ...
	proxyURL, args := peelProxyFlag(args)

	if len(args) < 1 {
		printUsage()
		return 2
	}
	switch args[0] {
	case "login":
		return cmdLogin(args[1:], proxyURL)
	case "serve":
		return cmdServe(args[1:], proxyURL)
	case "generate":
		return cmdGenerate(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "logout":
		return cmdLogout(args[1:])
	case "version", "-version", "--version":
		fmt.Println(Version)
		return 0
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		return 2
	}
}

func peelProxyFlag(args []string) (proxyURL string, rest []string) {
	rest = args
	for len(rest) > 0 {
		a := rest[0]
		if a == "--proxy" && len(rest) > 1 {
			proxyURL = rest[1]
			rest = rest[2:]
			continue
		}
		if strings.HasPrefix(a, "--proxy=") {
			proxyURL = strings.TrimPrefix(a, "--proxy=")
			rest = rest[1:]
			continue
		}
		break
	}
	return strings.TrimSpace(proxyURL), rest
}

func applyProxy(explicit string) error {
	if explicit != "" {
		if _, err := outbound.ClassifyProxyURL(explicit); err != nil {
			return err
		}
		outbound.SetDefaultProxyURL(explicit)
		return nil
	}
	if u := outbound.ResolveProxyURL(""); u != "" {
		if _, err := outbound.ClassifyProxyURL(u); err != nil {
			return fmt.Errorf("outbound proxy from environment: %w", err)
		}
	}
	return nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `xai-proxy — local xAI OAuth proxy (chat + multimodal native /v1 paths)

Usage:
  xai-proxy generate                         # mint client API key (overwrites; shown once)
  xai-proxy [--proxy URL] login  [--no-browser] [--proxy URL]
  xai-proxy [--proxy URL] serve  [--host ...] [--port ...] [--proxy URL] [--header-timeout ...] [--i-understand-non-loopback-bind]
  xai-proxy status
  xai-proxy logout
  xai-proxy version

Client auth (local API key, required on /v1/*):
  1. Run:        xai-proxy generate   (prints key ONCE to stdout; save it)
  2. Clients:    Authorization: Bearer <key>
  On disk:       $XAI_PROXY_HOME/client_key  stores salted SHA-256 only (not the secret)
  Each generate overwrites the previous verifier; plaintext is never re-shown
  /health and /ready stay open for probes

serve options:
  --host / --port / --proxy
  --header-timeout duration          max wait for upstream response headers (default 15m; 0 disables).
                                     Non-SSE chat waits here while the model thinks.
  --i-understand-non-loopback-bind   required when --host is not loopback (client key still required)

login options:
  --no-browser                 print device URL only (do not open a browser)

First run: generate → login → serve.

Outbound proxy (OAuth + API egress):
  --proxy URL     http(s)://host:port | socks5://host:port | socks5h://host:port
  Env: XAI_PROXY_HOME, XAI_PROXY_OUTBOUND, XAI_BASE_URL
       ALL_PROXY, HTTPS_PROXY, HTTP_PROXY (+ lowercase), NO_PROXY / no_proxy
  Priority: --proxy > XAI_PROXY_OUTBOUND > ALL_PROXY > HTTPS_PROXY > HTTP_PROXY > direct
  See README "Environment variables" for details.
`)
}

func mergeProxy(flagVal, global string) string {
	flagVal = strings.TrimSpace(flagVal)
	if flagVal != "" {
		return flagVal
	}
	return strings.TrimSpace(global)
}

func cmdLogin(args []string, globalProxy string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	noBrowser := fs.Bool("no-browser", false, "do not open a browser")
	proxyFlag := fs.String("proxy", "", "outbound HTTP or SOCKS5 proxy URL")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	explicit := mergeProxy(*proxyFlag, globalProxy)
	if err := applyProxy(explicit); err != nil {
		fmt.Fprintf(os.Stderr, "proxy: %v\n", err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "Outbound: %s\n", outbound.Describe(explicit))

	if code := doLogin(!*noBrowser); code != 0 {
		return code
	}
	fmt.Fprintln(os.Stderr, "  Next:   xai-proxy serve")
	return 0
}

// doLogin runs device-code OAuth and saves tokens. openBrowser=false → --no-browser style.
func doLogin(openBrowser bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client, err := outbound.NewClient(outbound.Options{Timeout: 30 * time.Second})
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy client: %v\n", err)
		return 1
	}
	result, err := auth.DeviceLogin(ctx, client, openBrowser, func(s string) {
		fmt.Fprintln(os.Stderr, s)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
		return 1
	}
	if err := credential.SaveLogin(result); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save tokens: %v\n", err)
		return 1
	}
	path, _ := store.TokensPath()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Login successful!")
	fmt.Fprintf(os.Stderr, "  Tokens: %s\n", path)
	return 0
}

func cmdServe(args []string, globalProxy string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "listen host")
	port := fs.Int("port", 7257, "listen port")
	proxyFlag := fs.String("proxy", "", "outbound HTTP or SOCKS5 proxy URL")
	headerTimeout := fs.Duration("header-timeout", outbound.DefaultResponseHeaderTimeout, "max wait for upstream response headers (0 disables)")
	allowRemote := fs.Bool("i-understand-non-loopback-bind", false, "required if host is not loopback; does not disable client API key auth")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	explicit := mergeProxy(*proxyFlag, globalProxy)
	if err := applyProxy(explicit); err != nil {
		fmt.Fprintf(os.Stderr, "proxy: %v\n", err)
		return 2
	}

	mgr := credential.NewManager(nil)
	if !mgr.IsAuthenticated() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run `xai-proxy login` first.")
		return 2
	}
	return runServe(*host, *port, *allowRemote, explicit, *headerTimeout)
}

func runServe(host string, port int, allowRemote bool, explicitProxy string, headerTimeout time.Duration) int {
	if !proxy.IsLoopback(host) && !allowRemote {
		fmt.Fprintf(os.Stderr,
			"refusing to bind non-loopback address %q without --i-understand-non-loopback-bind\n"+
				"(/v1/* still requires the local client key; anyone with the key and network access can use your quota)\n",
			host)
		return 2
	}

	// Timeout 0: allow long SSE/media bodies. Header wait is separate so
	// non-SSE thinking (headers arrive only after generation) is not cut at 2m.
	headerOpt := headerTimeout
	if headerTimeout <= 0 {
		headerOpt = -1
	}
	upClient, err := outbound.NewClient(outbound.Options{Timeout: 0, ResponseHeaderTimeout: headerOpt})
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy client: %v\n", err)
		return 1
	}
	refreshClient, err := outbound.NewClient(outbound.Options{
		Timeout: time.Duration(auth.DefaultRefreshTimeoutSeconds) * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy client: %v\n", err)
		return 1
	}

	mgr := credential.NewManager(refreshClient)
	if !mgr.IsAuthenticated() {
		fmt.Fprintln(os.Stderr, "Not logged in after login step.")
		return 2
	}

	verifier, err := store.LoadClientKeyVerifier()
	if err != nil {
		fmt.Fprintf(os.Stderr, "client API key: %v\n", err)
		return 1
	}
	if verifier == "" {
		fmt.Fprintf(os.Stderr,
			"no client API key configured\n"+
				"  run:  xai-proxy generate\n"+
				"  (plaintext key is printed once; disk stores a hash only)\n"+
				"  file: %s\n", store.FormatClientKeyPath())
		return 2
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := proxy.NewServerWithUpstream(mgr, upClient, proxy.Options{
		Host: host, Port: port, Logger: logger, ClientKeyVerifier: verifier,
	})

	fmt.Fprintf(os.Stderr, "Starting xai-proxy\n")
	fmt.Fprintf(os.Stderr, "  Listening on:  http://%s/v1\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "  Forwarding to: https://api.x.ai/v1 (chat + images + tts + stt + videos)\n")
	fmt.Fprintf(os.Stderr, "  Outbound:     %s\n", outbound.Describe(explicitProxy))
	if headerTimeout <= 0 {
		fmt.Fprintf(os.Stderr, "  Header wait:  disabled (no upstream header timeout)\n")
	} else {
		fmt.Fprintf(os.Stderr, "  Header wait:  %s (non-SSE thinking waits here)\n", headerTimeout)
	}
	fmt.Fprintf(os.Stderr, "  Client auth:  required on /v1/* (Bearer local key)\n")
	fmt.Fprintf(os.Stderr, "  Key hash:     %s\n", store.FormatClientKeyPath())
	fmt.Fprintf(os.Stderr, "  Body limit:   %d bytes\n\n", proxy.MaxBodyBytes)
	fmt.Fprintf(os.Stderr, "Client config:\n")
	fmt.Fprintf(os.Stderr, "  Base URL:  http://%s/v1\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "  API Key:   (from xai-proxy generate — not stored or shown again)\n")
	fmt.Fprintf(os.Stderr, "  Paths:     all xAI-native; chat also OpenAI full-compat\n")
	fmt.Fprintf(os.Stderr, "  Chat:      /v1/chat/completions model=grok-4.5\n")
	fmt.Fprintf(os.Stderr, "  Media:     /v1/images/* /v1/tts /v1/stt /v1/videos/...\n")
	fmt.Fprintf(os.Stderr, "  No shim:   /v1/audio/* → use /tts and /stt\n\n")
	fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop.")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "\nproxy: got %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return 0
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "proxy: %v\n", err)
			return 1
		}
		return 0
	}
}

func cmdStatus(args []string) int {
	_ = args
	mgr := credential.NewManager(nil)
	oauth := mgr.Status()
	ckOK := store.ClientKeyConfigured()
	out := struct {
		State     string `json:"state"`
		UpdatedAt string `json:"updated_at,omitempty"`
		Message   string `json:"message,omitempty"`
		BaseURL   string `json:"base_url,omitempty"`
		// Local client API key: configured or not (no path/secret).
		ClientKey string `json:"client_key"` // "configured" | "not_configured"
	}{
		State:     oauth.State,
		UpdatedAt: oauth.UpdatedAt,
		Message:   oauth.Message,
		BaseURL:   oauth.BaseURL,
		ClientKey: "not_configured",
	}
	if ckOK {
		out.ClientKey = "configured"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if oauth.State != store.StatusReady || !ckOK {
		return 1
	}
	return 0
}

func cmdLogout(args []string) int {
	_ = args
	if err := credential.Logout(); err != nil {
		fmt.Fprintf(os.Stderr, "logout failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "Logged out. Tokens removed.")
	return 0
}

// cmdGenerate mints a new client API key, overwrites the previous one, and
// prints the secret once on stdout. There is no "show" command.
func cmdGenerate(args []string) int {
	_ = args
	k, err := store.GenerateAndSaveClientKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "New client API key (shown once — save it now; update your clients):")
	fmt.Fprintln(os.Stderr, "  Disk stores a salted SHA-256 hash only (not this secret).")
	fmt.Fprintln(os.Stderr, "  Each generate overwrites the previous verifier.")
	fmt.Fprintf(os.Stderr, "  file: %s\n", store.FormatClientKeyPath())
	fmt.Fprintln(os.Stderr)
	fmt.Println(k)
	return 0
}
