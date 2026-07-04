package trajectory

import (
	"errors"
	"sync"

	"github.com/gnanam1990/argus/pkg/action"
)

// ErrClosed is returned by Append after the recorder has been closed.
var ErrClosed = errors.New("trajectory: recorder is closed")

// Memory is an in-memory, concurrency-safe Recorder. It deep-copies each step
// so callers may reuse buffers after Append, and exposes the recorded steps for
// inspection and replay in tests.
type Memory struct {
	mu       sync.Mutex
	manifest Manifest
	steps    []Step
	closed   bool
}

var _ Recorder = (*Memory)(nil)

// NewMemory builds an in-memory recorder for the given run manifest.
func NewMemory(m Manifest) *Memory { return &Memory{manifest: m} }

// Append records a deep copy of s.
func (r *Memory) Append(s Step) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	r.steps = append(r.steps, cloneStep(s))
	return nil
}

// Close finalizes the recording. Append after Close returns ErrClosed.
func (r *Memory) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// Manifest returns the run manifest.
func (r *Memory) Manifest() Manifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manifest
}

// Steps returns a deep copy of the recorded steps.
func (r *Memory) Steps() []Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Step, len(r.steps))
	for i, s := range r.steps {
		out[i] = cloneStep(s)
	}
	return out
}

// Len returns the number of recorded steps.
func (r *Memory) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.steps)
}

func cloneStep(s Step) Step {
	s.Screenshot.Data = cloneBytes(s.Screenshot.Data)
	if s.Actions != nil {
		acts := make([]action.Action, len(s.Actions))
		for i, a := range s.Actions {
			a.Keys = cloneStrings(a.Keys)
			a.Path = clonePoints(a.Path)
			acts[i] = a
		}
		s.Actions = acts
	}
	if s.Results != nil {
		res := make([]action.Result, len(s.Results))
		for i, rr := range s.Results {
			rr.Screenshot.Data = cloneBytes(rr.Screenshot.Data)
			rr.Data = cloneBytes(rr.Data)
			res[i] = rr
		}
		s.Results = res
	}
	return s
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func clonePoints(p []action.Point) []action.Point {
	if p == nil {
		return nil
	}
	out := make([]action.Point, len(p))
	copy(out, p)
	return out
}
