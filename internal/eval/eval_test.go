package eval_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/eval"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// fakeSession returns a scripted outcome/error.
type fakeSession struct {
	outcome *agent.Outcome
	err     error
}

func (s fakeSession) Run(context.Context, string) (*agent.Outcome, error) {
	return s.outcome, s.err
}
func (s fakeSession) History() *model.Conversation { return nil }

func TestRunScoresTasks(t *testing.T) {
	t.Parallel()
	tasks := []eval.Task{
		{Name: "ok", Prompt: "do ok"},
		{Name: "incomplete", Prompt: "do partial"},
		{Name: "boom", Prompt: "fail"},
	}
	factory := func(task eval.Task) agent.Session {
		switch task.Name {
		case "ok":
			return fakeSession{outcome: &agent.Outcome{Reason: agent.ReasonCompleted, Steps: 3}}
		case "incomplete":
			return fakeSession{outcome: &agent.Outcome{Reason: agent.ReasonMaxSteps, Steps: 40}}
		default:
			return fakeSession{err: errors.New("provider down")}
		}
	}

	rep := eval.Run(context.Background(), tasks, factory, eval.Completed)
	if rep.Total != 3 || rep.Passed != 1 {
		t.Fatalf("report = %d/%d passed, want 1/3", rep.Passed, rep.Total)
	}
	byName := map[string]eval.Result{}
	for _, r := range rep.Results {
		byName[r.Task] = r
	}
	if !byName["ok"].Pass || byName["ok"].Steps != 3 {
		t.Errorf("ok result = %+v", byName["ok"])
	}
	if byName["incomplete"].Pass || byName["incomplete"].Reason != agent.ReasonMaxSteps {
		t.Errorf("incomplete result = %+v", byName["incomplete"])
	}
	if byName["boom"].Pass || byName["boom"].Error == "" {
		t.Errorf("boom result = %+v", byName["boom"])
	}
}

func TestReportJSON(t *testing.T) {
	t.Parallel()
	rep := eval.Report{Total: 1, Passed: 1, Results: []eval.Result{{Task: "t", Pass: true}}}
	if !strings.Contains(string(rep.JSON()), `"passed": 1`) {
		t.Errorf("json = %s", rep.JSON())
	}
}

func TestLoadTasks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	_ = os.WriteFile(path, []byte(`{"tasks":[{"name":"a","prompt":"do a"},{"name":"b","prompt":"do b"}]}`), 0o600)

	tasks, err := eval.LoadTasks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].Name != "a" || tasks[1].Prompt != "do b" {
		t.Errorf("tasks = %+v", tasks)
	}

	if _, err := eval.LoadTasks("/no/such.json"); err == nil {
		t.Error("missing manifest should error")
	}
}
