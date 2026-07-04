// Package trajectory defines the versioned record of an agent run: a provenance
// Manifest plus an ordered list of Steps (observation → model output →
// execution). The same schema is the runtime log and the eval/RL export, so a
// recorded run can be replayed and scored later.
//
// Secret handling: a Step can contain typed secrets (a Type action's text) and
// screen contents. Masking is applied at export time (a later stage); this
// package defines the schema and in-memory/no-op recorders only.
package trajectory

import (
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// SchemaVersion is the on-disk/record schema version. Bump on any
// backwards-incompatible change to Manifest or Step.
const SchemaVersion = 1

// Manifest is the run-level provenance needed to reproduce and attribute a
// trajectory. Timestamps are supplied by the caller (RFC 3339 strings) so this
// package stays deterministic and testable.
type Manifest struct {
	SchemaVersion int      `json:"schema_version"`
	Task          string   `json:"task"`
	Model         string   `json:"model,omitempty"`
	ProviderBeta  string   `json:"provider_beta,omitempty"`
	ConfigHash    string   `json:"config_hash,omitempty"`
	GitSHA        string   `json:"git_sha,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	Seed          *int     `json:"seed,omitempty"`
	StartedAt     string   `json:"started_at,omitempty"`
}

// NewManifest builds a manifest stamped with the current SchemaVersion.
func NewManifest(task string) Manifest {
	return Manifest{SchemaVersion: SchemaVersion, Task: task}
}

// Step is one iteration of the agent loop: the observation the model saw, its
// output (reasoning text and requested actions), the results of executing them,
// and token usage.
type Step struct {
	Index      int             `json:"index"`
	Screenshot action.Image    `json:"screenshot,omitempty"`
	Text       string          `json:"text,omitempty"`
	Actions    []action.Action `json:"actions,omitempty"`
	Results    []action.Result `json:"results,omitempty"`
	Usage      model.Usage     `json:"usage"`
}

// Recorder persists steps for a run. Implementations must be safe for
// sequential use by one session; the Memory recorder is additionally
// concurrency-safe.
type Recorder interface {
	// Append records the next step.
	Append(s Step) error
	// Close finalizes the recording.
	Close() error
}

// NoOp is a Recorder that discards everything. It is the default when recording
// is disabled.
type NoOp struct{}

var _ Recorder = NoOp{}

// Append discards the step.
func (NoOp) Append(Step) error { return nil }

// Close does nothing.
func (NoOp) Close() error { return nil }
