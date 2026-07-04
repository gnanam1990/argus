package computer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/computer/fake"
)

// opsComputer is a fake driver that additionally implements the optional
// Commander and FileReader capability interfaces.
type opsComputer struct {
	*fake.Computer
	ran  string
	read string
}

func (o *opsComputer) RunCommand(_ context.Context, cmd string) (string, error) {
	o.ran = cmd
	return "out:" + cmd, nil
}

func (o *opsComputer) ReadFile(_ context.Context, path string) ([]byte, error) {
	o.read = path
	return []byte("data:" + path), nil
}

func TestExecutorRunCommandBridge(t *testing.T) {
	t.Parallel()
	oc := &opsComputer{Computer: fake.New()}
	e := computer.NewExecutor(oc, computer.WithCapabilities(action.RunCommand))

	res, err := e.Execute(context.Background(), action.Action{Type: action.RunCommand, Text: "echo hi", Mark: action.NoMark})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "out:echo hi" || oc.ran != "echo hi" {
		t.Errorf("bridge output = %q, ran = %q", res.Output, oc.ran)
	}
}

func TestExecutorReadFileBridge(t *testing.T) {
	t.Parallel()
	oc := &opsComputer{Computer: fake.New()}
	e := computer.NewExecutor(oc, computer.WithCapabilities(action.ReadFile))

	res, err := e.Execute(context.Background(), action.Action{Type: action.ReadFile, Text: "/tmp/x", Mark: action.NoMark})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "data:/tmp/x" {
		t.Errorf("Output = %q", res.Output)
	}
}

func TestExecutorGatedDeniedWithoutCapability(t *testing.T) {
	t.Parallel()
	oc := &opsComputer{Computer: fake.New()}
	e := computer.NewExecutor(oc) // no capabilities granted

	_, err := e.Execute(context.Background(), action.Action{Type: action.RunCommand, Text: "ls", Mark: action.NoMark})
	if !errors.Is(err, computer.ErrCapabilityDenied) {
		t.Errorf("err = %v, want ErrCapabilityDenied", err)
	}
}

func TestExecutorGatedUnsupportedWithoutInterface(t *testing.T) {
	t.Parallel()
	e := computer.NewExecutor(fake.New(), computer.WithCapabilities(action.RunCommand))

	_, err := e.Execute(context.Background(), action.Action{Type: action.RunCommand, Text: "ls", Mark: action.NoMark})
	if !errors.Is(err, computer.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestAllowAccumulates(t *testing.T) {
	t.Parallel()
	e := computer.NewExecutor(fake.New())
	e.Allow(action.RunCommand)
	e.Allow(action.ReadFile)
	if !e.Allowed(action.RunCommand) || !e.Allowed(action.ReadFile) {
		t.Error("Allow must accumulate grants, not replace them")
	}
}
