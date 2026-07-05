package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/actor"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/capture"
	"github.com/gnanam1990/argus/internal/computeruse/grounding"
	"github.com/gnanam1990/argus/internal/computeruse/instructions"
	"github.com/gnanam1990/argus/internal/computeruse/mcp"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/computer"
)

// cuParts holds the platform-specific host implementations chosen by build tag
// (see computeruse_darwin.go / computeruse_other.go).
type cuParts struct {
	checker  permissions.Checker
	guardian permissions.Guardian
	focuser  capture.Focuser
	lister   capture.AppLister
}

// ComputerUse is the assembled app-aware computer-use subsystem: the MCP server
// plus the pieces the CLI needs directly (approvals, permissions, state).
type ComputerUse struct {
	Server       *mcp.Server
	Store        approval.Store
	Orchestrator permissions.Orchestrator
	State        state.StateProvider
	Actor        actor.Actor
}

// PermissionOrchestrator builds just the permissions orchestrator (no host
// driver), for `argus cu doctor` and other checks that don't drive the desktop.
func PermissionOrchestrator() permissions.Orchestrator {
	plat := cuPlatform()
	return permissions.New(plat.checker, plat.guardian)
}

// BuildComputerUse wires the subsystem over the host driver: permissions,
// approval store (pre-approving any configured apps), per-app instructions,
// accessibility grounding, actor, and the async capture worker behind an MCP
// server. The returned cleanup closes the host driver.
func BuildComputerUse(cfg config.Config) (*ComputerUse, func() error, error) {
	comp := hostDriver(cfg.Sandbox.Display, cfg.ComputerUse.IsSmoothCursor())
	plat := cuPlatform()
	orch := permissions.New(plat.checker, plat.guardian)

	storePath, err := approval.DefaultPath()
	if err != nil {
		_ = comp.Close()
		return nil, nil, err
	}
	store := approval.NewFileStore(storePath)
	for _, b := range cfg.ComputerUse.AutoApproveApps {
		if b == "" {
			continue
		}
		if err := store.Set(context.Background(), b, approval.Approved); err != nil {
			_ = comp.Close()
			return nil, nil, fmt.Errorf("pre-approve %q: %w", b, err)
		}
	}

	loader := instructions.NewChainLoader(os.ReadFile, os.UserConfigDir, cfg.ComputerUse.InstructionDirs...)
	// Ground against the same display the driver captures: pass its global
	// bounds so accessibility frames map into the display's local space. Use
	// logical coordinates — the actor feeds these frames straight to the
	// driver, which works in logical points, so pixel scaling must be off.
	axOpts := []ax.HostOption{ax.WithLogicalCoords()}
	if db, ok := comp.(computer.DisplayBounder); ok {
		x, y, w, h := db.DisplayBounds()
		if w > 0 && h > 0 {
			axOpts = append(axOpts, ax.WithDisplayBounds(x, y, w, h))
		}
	}
	provider := grounding.New(ax.HostSource(axOpts...), comp.Screenshot)
	act := actor.New(comp)

	var wopts []capture.Option
	if ms := cfg.ComputerUse.MaxCaptureTimeout; ms > 0 {
		wopts = append(wopts, capture.WithTimeout(time.Duration(ms)*time.Millisecond))
	}
	worker := capture.NewDefaultWorker(orch, store, plat.focuser, provider, loader, comp, wopts...)
	sp := capture.NewProvider(worker, plat.lister)

	srv := mcp.New(sp, act, orch, store)
	cleanup := func() error { return comp.Close() }
	return &ComputerUse{Server: srv, Store: store, Orchestrator: orch, State: sp, Actor: act}, cleanup, nil
}
