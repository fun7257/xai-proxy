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
	"syscall"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/credential"
	"xai-proxy/internal/proxy"
	"xai-proxy/internal/store"
)

// Version is set by main.
var Version = "0.1.0"

// Run is the CLI entrypoint.
func Run(args []string) int {
	if len(args) < 1 {
		printUsage()
		return 2
	}
	switch args[0] {
	case "login":
		return cmdLogin(args[1:])
	case "serve":
		return cmdServe(args[1:])
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

func printUsage() {
	fmt.Fprintf(os.Stderr, `xai-proxy — local xAI OAuth proxy (chat + multimodal native /v1 paths)

Usage:
  xai-proxy login [--no-browser]
  xai-proxy serve [--host 127.0.0.1] [--port 8645] [--i-understand-no-client-auth]
  xai-proxy status
  xai-proxy logout
  xai-proxy version

After login once, point clients at http://127.0.0.1:8645/v1
The proxy ignores client Authorization and attaches your OAuth bearer.

All routes are xAI-native. Chat/text paths also work with OpenAI SDKs (full compat only).
Native paths: /chat/completions /responses /models /embeddings /completions
             /images/* /tts /stt /videos/*   (no /audio/* shims — use /tts and /stt)
`)
}

func cmdLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	noBrowser := fs.Bool("no-browser", false, "do not open a browser")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}
	result, err := auth.DeviceLogin(ctx, client, !*noBrowser, func(s string) {
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
	fmt.Fprintln(os.Stderr, "  Next:   xai-proxy serve")
	return 0
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "listen host")
	port := fs.Int("port", 8645, "listen port")
	allowRemote := fs.Bool("i-understand-no-client-auth", false, "required if host is not loopback")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !proxy.IsLoopback(*host) && !*allowRemote {
		fmt.Fprintf(os.Stderr,
			"refusing to bind non-loopback address %q without --i-understand-no-client-auth\n"+
				"(this proxy has no client authentication; anyone who can connect spends your SuperGrok quota)\n",
			*host)
		return 2
	}

	mgr := credential.NewManager(nil)
	if !mgr.IsAuthenticated() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run `xai-proxy login` first.")
		return 2
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := proxy.NewServer(mgr, proxy.Options{Host: *host, Port: *port, Logger: logger})

	fmt.Fprintf(os.Stderr, "Starting xai-proxy\n")
	fmt.Fprintf(os.Stderr, "  Listening on:  http://%s/v1\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "  Forwarding to: https://api.x.ai/v1 (chat + images + tts + stt + videos)\n")
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
