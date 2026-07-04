package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gnanam1990/argus/internal/viewer"
)

// viewCmd serves the trajectory replay UI for a recorded run directory.
func viewCmd(args []string, out io.Writer) error {
	addr := "127.0.0.1:0"
	var dir string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr", "-a":
			if i+1 < len(args) {
				i++
				addr = args[i]
			}
		default:
			if dir == "" {
				dir = args[i]
			}
		}
	}
	if dir == "" {
		return fmt.Errorf("view: a trajectory directory is required (argus view ./runs/first)")
	}

	srv, err := viewer.New(dir)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("view: listen %s: %w", addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	hs := &http.Server{Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = hs.Shutdown(shCtx)
	}()

	fmt.Fprintf(out, "viewing %s at http://%s  (ctrl-c to stop)\n", dir, ln.Addr())
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
