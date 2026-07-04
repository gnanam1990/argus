// Command guestd is the in-sandbox computer server. It runs inside a container
// or VM (typically under xvfb-run) and exposes the local computer over the
// transport protocol so the agent on the host can drive it.
//
// Security posture: binds localhost by default (no auth). Set ARGUS_GUEST_TOKEN
// to require a bearer token and bind a routable address for a cloud sandbox.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/internal/guest"
	"github.com/gnanam1990/argus/internal/guest/proto"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/internal/version"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7180", "listen address (localhost by default)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	opts := []transport.ServerOption{
		transport.WithCommands(proto.Commands()...),
		transport.WithRateLimit(120, secondsWindow),
		transport.WithAuditLogger(slog.Default()),
	}
	if token := os.Getenv("ARGUS_GUEST_TOKEN"); token != "" {
		opts = append(opts, transport.WithAuth(transport.NewBearerToken(token)))
		slog.Info("guestd: bearer auth enabled")
	} else if !isLoopback(*addr) {
		fmt.Fprintln(os.Stderr, "guestd: refusing to bind a non-loopback address without ARGUS_GUEST_TOKEN")
		os.Exit(1)
	}

	srv := transport.NewServer(guest.New(shell.New()), opts...)
	slog.Info("guestd: listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, srv); err != nil { //nolint:gosec // localhost dev server
		fmt.Fprintln(os.Stderr, "guestd:", err)
		os.Exit(1)
	}
}
