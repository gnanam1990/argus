package trajectory

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

func TestNewManifestStampsVersion(t *testing.T) {
	t.Parallel()
	m := NewManifest("book a flight")
	if m.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	if m.Task != "book a flight" {
		t.Errorf("Task = %q", m.Task)
	}
}

func TestStepJSONRoundTrip(t *testing.T) {
	t.Parallel()
	step := Step{
		Index:      2,
		Screenshot: action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}},
		Text:       "clicking submit",
		Actions:    []action.Action{{Type: action.Click, Button: action.Left, Mark: action.NoMark}},
		Results:    []action.Result{{Terminated: false}},
		Usage:      model.Usage{InputTokens: 10, OutputTokens: 4},
	}
	b, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Step
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Index != step.Index || got.Text != step.Text ||
		got.Usage != step.Usage || len(got.Actions) != 1 ||
		got.Actions[0].Type != action.Click {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, step)
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	temp := 0.2
	seed := 7
	m := Manifest{
		SchemaVersion: SchemaVersion,
		Task:          "t",
		Model:         "some-model",
		ConfigHash:    "abc",
		GitSHA:        "deadbee",
		Temperature:   &temp,
		Seed:          &seed,
		StartedAt:     "2026-07-04T00:00:00Z",
	}
	b, _ := json.Marshal(m)
	var got Manifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SchemaVersion || got.Model != "some-model" ||
		got.Temperature == nil || *got.Temperature != 0.2 || got.Seed == nil || *got.Seed != 7 {
		t.Errorf("manifest round-trip = %+v", got)
	}
}

func TestMemoryRecorder(t *testing.T) {
	t.Parallel()
	r := NewMemory(NewManifest("task"))
	for i := 0; i < 3; i++ {
		if err := r.Append(Step{Index: i}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if r.Len() != 3 {
		t.Errorf("Len = %d, want 3", r.Len())
	}
	steps := r.Steps()
	for i, s := range steps {
		if s.Index != i {
			t.Errorf("step[%d].Index = %d", i, s.Index)
		}
	}
	if r.Manifest().Task != "task" {
		t.Errorf("manifest task = %q", r.Manifest().Task)
	}
}

func TestMemoryDeepCopiesOnAppendAndRead(t *testing.T) {
	t.Parallel()
	r := NewMemory(NewManifest("t"))
	step := Step{
		Index:      0,
		Screenshot: action.Image{MIME: action.MIMEPNG, Data: []byte{0xAA}},
		Actions:    []action.Action{{Type: action.Key, Keys: []string{"ctrl", "c"}}},
	}
	if err := r.Append(step); err != nil {
		t.Fatal(err)
	}
	// Mutate the caller's step after Append.
	step.Screenshot.Data[0] = 0xFF
	step.Actions[0].Keys[0] = "MUT"

	got := r.Steps()[0]
	if got.Screenshot.Data[0] != 0xAA {
		t.Error("Append must deep-copy screenshot data")
	}
	if got.Actions[0].Keys[0] != "ctrl" {
		t.Error("Append must deep-copy action keys")
	}

	// Mutating the read copy must not affect stored state either.
	got.Screenshot.Data[0] = 0x11
	if r.Steps()[0].Screenshot.Data[0] != 0xAA {
		t.Error("Steps must return an independent deep copy")
	}
}

func TestMemoryClosed(t *testing.T) {
	t.Parallel()
	r := NewMemory(NewManifest("t"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Append(Step{}); err != ErrClosed {
		t.Errorf("Append after Close = %v, want ErrClosed", err)
	}
}

func TestMemoryConcurrentAppend(t *testing.T) {
	t.Parallel()
	r := NewMemory(NewManifest("t"))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = r.Append(Step{Index: i})
		}(i)
	}
	wg.Wait()
	if r.Len() != 50 {
		t.Errorf("Len = %d, want 50", r.Len())
	}
}

func TestNoOpRecorder(t *testing.T) {
	t.Parallel()
	var r Recorder = NoOp{}
	if err := r.Append(Step{Index: 1}); err != nil {
		t.Errorf("NoOp.Append = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("NoOp.Close = %v", err)
	}
}
