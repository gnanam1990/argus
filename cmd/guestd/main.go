// Command guestd is the in-sandbox computer server. It runs inside a container
// or VM (typically under xvfb-run) and exposes the local computer over the
// transport protocol so the agent on the host can drive it.
//
// Security posture: binds localhost by default (no auth). Set ARGUS_GUEST_TOKEN
// to require a bearer token and bind a routable address for a cloud sandbox.
// Set ARGUS_GUEST_TLS_CERT + ARGUS_GUEST_TLS_KEY to serve HTTPS; a non-loopback
// bind without them serves cleartext (the bearer token, screenshots, and input
// commands all cross the wire unencrypted) and guestd logs a loud warning.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/internal/guest"
	"github.com/gnanam1990/argus/internal/guest/proto"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/internal/version"
)

// shutdownTimeout bounds how long graceful shutdown waits for in-flight
// commands to finish before the listener is forced closed.
const shutdownTimeout = 5 * time.Second

func main() {
	addr := flag.String("addr", "127.0.0.1:7180", "listen address (localhost by default)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *addr, os.Getenv, slog.Default(), nil); err != nil {
		fmt.Fprintln(os.Stderr, "guestd:", err)
		os.Exit(1)
	}
}

// run builds the guest server for addr and serves it until ctx is cancelled,
// then shuts down gracefully (or returns a serve-time error). It is the
// testable core of main: addr is bound via net.Listen (not
// http.ListenAndServe) precisely so tests can use an ephemeral port ("...:0")
// and, via ready, discover the real bound address before making requests.
// ready may be nil (production use); if non-nil, the bound address is sent on
// it once listening starts, before serving begins.
func run(ctx context.Context, addr string, getenv func(string) string, log *slog.Logger, ready chan<- string) error {
	opts := []transport.ServerOption{
		transport.WithCommands(proto.Commands()...),
		transport.WithRateLimit(120, secondsWindow),
		transport.WithAuditLogger(log),
	}
	if token := getenv("ARGUS_GUEST_TOKEN"); token != "" {
		opts = append(opts, transport.WithAuth(transport.NewBearerToken(token)))
		log.Info("guestd: bearer auth enabled")
	} else if !isLoopback(addr) {
		return errors.New("refusing to bind a non-loopback address without ARGUS_GUEST_TOKEN")
	}

	certFile, keyFile := getenv("ARGUS_GUEST_TLS_CERT"), getenv("ARGUS_GUEST_TLS_KEY")
	useTLS := certFile != "" && keyFile != ""
	warnIfCleartext(log, addr, useTLS)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if ready != nil {
		ready <- ln.Addr().String()
	}

	srv := &http.Server{Handler: transport.NewServer(guest.New(shell.New()), opts...)}

	errCh := make(chan error, 1)
	go func() {
		log.Info("guestd: listening", "addr", ln.Addr().String(), "tls", useTLS)
		var serveErr error
		if useTLS {
			serveErr = srv.ServeTLS(ln, certFile, keyFile)
		} else {
			serveErr = srv.Serve(ln) //nolint:gosec // fail-closed token check + cleartext warning above gate this
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Info("guestd: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// warnIfCleartext logs a loud, unmissable warning when a non-loopback bind
// will serve plaintext HTTP, so the operator doesn't discover the exposure
// after the fact (docs/threat-model.md's transport claim now matches this:
// TLS is opt-in via env vars, and cleartext non-loopback binds are warned,
// not silently allowed).
func warnIfCleartext(log *slog.Logger, addr string, useTLS bool) {
	if useTLS || isLoopback(addr) {
		return
	}
	log.Warn("guestd: serving a non-loopback address over PLAINTEXT HTTP " +
		"— the bearer token, screenshots, and input commands are visible to anyone on the network path; " +
		"set ARGUS_GUEST_TLS_CERT/ARGUS_GUEST_TLS_KEY, or put this behind a TLS-terminating proxy or SSH tunnel")
}
