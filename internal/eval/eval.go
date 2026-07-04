// Package eval runs the agent against a set of tasks and scores the outcomes.
// A task runs through an agent.Session (live) or is scored from a recorded
// trajectory (replay); the report is machine-readable for CI and benchmarking.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gnanam1990/argus/pkg/agent"
)

// Task is one evaluation case.
type Task struct {
	Name    string        `json:"name"`
	Prompt  string        `json:"prompt"`
	Timeout time.Duration `json:"timeout,omitempty"`
}

// Result is the outcome of one task.
type Result struct {
	Task   string `json:"task"`
	Pass   bool   `json:"pass"`
	Steps  int    `json:"steps"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Report aggregates task results.
type Report struct {
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Results []Result `json:"results"`
}

// JSON renders the report as indented JSON.
func (r Report) JSON() []byte {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return b
}

// Scorer decides whether an outcome passes.
type Scorer func(*agent.Outcome, error) bool

// Completed passes when the run completed (assistant finished) without error.
func Completed(o *agent.Outcome, err error) bool {
	return err == nil && o != nil && o.Reason == agent.ReasonCompleted
}

// SessionFactory builds a fresh Session for a task (each session needs its own
// provider state).
type SessionFactory func(task Task) agent.Session

// Run executes each task through a fresh session and scores it.
func Run(ctx context.Context, tasks []Task, factory SessionFactory, score Scorer) Report {
	var rep Report
	for _, task := range tasks {
		tctx := ctx
		var cancel context.CancelFunc
		if task.Timeout > 0 {
			tctx, cancel = context.WithTimeout(ctx, task.Timeout)
		}
		out, err := factory(task).Run(tctx, task.Prompt)
		if cancel != nil {
			cancel()
		}

		res := Result{Task: task.Name, Pass: score(out, err)}
		if out != nil {
			res.Steps = out.Steps
			res.Reason = out.Reason
		}
		if err != nil {
			res.Error = err.Error()
		}
		rep.Results = append(rep.Results, res)
		rep.Total++
		if res.Pass {
			rep.Passed++
		}
	}
	return rep
}

// LoadTasks reads a JSON manifest: {"tasks": [{"name","prompt"}, ...]}.
func LoadTasks(path string) ([]Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read manifest: %w", err)
	}
	var doc struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("eval: parse manifest: %w", err)
	}
	return doc.Tasks, nil
}
