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
var Version = "0.1.0"

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
	case "start":
		// First-boot friendly: login if needed (default --no-browser), then serve.
		return cmdStart(args[1:], proxyURL)
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
  xai-proxy [--proxy URL] start  [options]   # login if needed, then serve (container default)
  xai-proxy [--proxy URL] login  [--no-browser] [--proxy URL]
  xai-proxy [--proxy URL] serve  [--host ...] [--port ...] [--proxy URL] [--i-understand-no-client-auth]
  xai-proxy status
  xai-proxy logout
  xai-proxy version

start options (same as serve, plus login):
  --no-browser                 print device URL only (default: true for start)
  --browser                    allow opening a local browser during login
  --host / --port / --proxy / --i-understand-no-client-auth

First run: start prints an accounts.x.ai URL; after you approve in a browser,
tokens are saved and the API proxy starts automatically.

Outbound proxy (OAuth + API egress):
  --proxy URL     http(s)://host:port | socks5://host:port | socks5h://host:port
  Env: XAI_PROXY_OUTBOUND, ALL_PROXY, HTTPS_PROXY, HTTP_PROXY
  Bypass: NO_PROXY / no_proxy
  Priority: --proxy > XAI_PROXY_OUTBOUND > ALL_PROXY > HTTPS_PROXY > HTTP_PROXY > direct
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
	fmt.Fprintln(os.Stderr, "  Next:   xai-proxy serve   (or xai-proxy start)")
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
	port := fs.Int("port", 8645, "listen port")
	proxyFlag := fs.String("proxy", "", "outbound HTTP or SOCKS5 proxy URL")
	allowRemote := fs.Bool("i-understand-no-client-auth", false, "required if host is not loopback")
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
		fmt.Fprintln(os.Stderr, "Not logged in. Run `xai-proxy start` (login+serve) or `xai-proxy login` first.")
		return 2
	}
	return runServe(*host, *port, *allowRemote, explicit)
}

// cmdStart: if no usable tokens, device-login (default --no-browser), then serve.
func cmdStart(args []string, globalProxy string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	// Container-friendly defaults
	host := fs.String("host", "127.0.0.1", "listen host")
	port := fs.Int("port", 8645, "listen port")
	proxyFlag := fs.String("proxy", "", "outbound HTTP or SOCKS5 proxy URL")
	allowRemote := fs.Bool("i-understand-no-client-auth", false, "required if host is not loopback")
	// Default no-browser for start (print URL; user authorizes on another device/browser).
	noBrowser := fs.Bool("no-browser", true, "print device URL only (default true for start)")
	useBrowser := fs.Bool("browser", false, "open a local browser during login (overrides --no-browser)")
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

	mgr := credential.NewManager(nil)
	if !mgr.IsAuthenticated() {
		fmt.Fprintln(os.Stderr, "No credentials found — starting device login…")
		fmt.Fprintln(os.Stderr, "Open the URL below in a browser, approve access, then this process will serve automatically.")
		fmt.Fprintln(os.Stderr)
		openBrowser := *useBrowser // default false; --browser opens local browser
		if *noBrowser && !*useBrowser {
			openBrowser = false
		}
		_ = noBrowser // default true; only --browser flips openBrowser on
		if code := doLogin(openBrowser); code != 0 {
			return code
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Starting API proxy…")
	} else {
		fmt.Fprintln(os.Stderr, "Credentials present — starting API proxy…")
	}

	return runServe(*host, *port, *allowRemote, explicit)
}

func runServe(host string, port int, allowRemote bool, explicitProxy string) int {
	if !proxy.IsLoopback(host) && !allowRemote {
		fmt.Fprintf(os.Stderr,
			"refusing to bind non-loopback address %q without --i-understand-no-client-auth\n"+
				"(this proxy has no client authentication; anyone who can connect spends your SuperGrok quota)\n",
			host)
		return 2
	}

	upClient, err := outbound.NewClient(outbound.Options{Timeout: 0})
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy client: %v\n", err)
		return 1
	}
	if tr, ok := upClient.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = 300 * time.Second
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := proxy.NewServerWithUpstream(mgr, upClient, proxy.Options{Host: host, Port: port, Logger: logger})

	fmt.Fprintf(os.Stderr, "Starting xai-proxy\n")
	fmt.Fprintf(os.Stderr, "  Listening on:  http://%s/v1\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "  Forwarding to: https://api.x.ai/v1 (chat + images + tts + stt + videos)\n")
	fmt.Fprintf(os.Stderr, "  Outbound:     %s\n", outbound.Describe(explicitProxy))
	fmt.Fprintf(os.Stderr, "  Client auth:   none (OAuth attached by proxy)\n")
	fmt.Fprintf(os.Stderr, "  Body limit:    %d bytes\n\n", proxy.MaxBodyBytes)
	fmt.Fprintf(os.Stderr, "Client config:\n")
	fmt.Fprintf(os.Stderr, "  Base URL:  http://%s/v1\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "  API Key:   sk-local (ignored)\n")
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
	st := mgr.Status()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(st)
	if st.State != store.StatusReady {
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
