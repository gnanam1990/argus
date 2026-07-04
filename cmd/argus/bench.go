package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/bench"
	"github.com/gnanam1990/argus/internal/config"
)

// benchCmd scores click-grounding accuracy against a local dataset: for each
// case (screenshot + instruction + target box), the configured grounder must
// produce a point inside the box.
func benchCmd(args []string, out io.Writer) error {
	var dir, configPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "-d":
			if i+1 < len(args) {
				i++
				dir = args[i]
			}
		case "--config", "-c":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		default:
			if dir == "" {
				dir = args[i]
			}
		}
	}
	if dir == "" {
		return fmt.Errorf("bench: a dataset directory is required (argus bench ./datasets/screenspot)")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	gr, _ := app.BuildGrounder(cfg)
	if gr == nil {
		return fmt.Errorf("bench: grounding.mode %q builds no detector; set it to ax, omniparser, or chain", cfg.Grounding.Mode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	rep, err := bench.Run(ctx, dir, bench.GrounderPointer(gr, cfg.Grounding.MinConfidence))
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(rep.JSON()))
	fmt.Fprintf(out, "\naccuracy: %.1f%% (%d/%d)\n", rep.Accuracy*100, rep.Hits, rep.Total)
	return nil
}
