package main

import (
	"os"

	"xai-proxy/internal/cli"
)

// version can be overridden at link time: -ldflags "-X main.version=..."
var version = "0.1.0"

func main() {
	cli.Version = version
	os.Exit(cli.Run(os.Args[1:]))
}
