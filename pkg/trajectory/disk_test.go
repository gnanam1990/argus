package trajectory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

func TestDiskRecordAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManifest("book a flight")
	m.Model = "claude-opus-4-8"
	m.GitSHA = "deadbee"

	rec, err := NewDisk(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	err = rec.Append(Step{
		Index:      0,
		Screenshot: action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}},
		Text:       "clicking",
		Actions:    []action.Action{{Type: action.Click, Button: action.Left, Mark: action.NoMark}},
		Usage:      model.Usage{InputTokens: 10, OutputTokens: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Append(Step{Index: 1, Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	// Screenshot written as a file.
	if _, err := os.Stat(filepath.Join(dir, "step-0.png")); err != nil {
		t.Errorf("screenshot file missing: %v", err)
	}

	// Reload manifest + records.
	gotM, records, err := LoadDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	if gotM.Model != "claude-opus-4-8" || gotM.GitSHA != "deadbee" || gotM.SchemaVersion != SchemaVersion {
		t.Errorf("manifest = %+v", gotM)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].ScreenshotFile != "step-0.png" || records[0].Text != "clicking" {
		t.Errorf("record 0 = %+v", records[0])
	}
	if len(records[0].Actions) != 1 || records[0].Actions[0].Type != action.Click {
		t.Errorf("record 0 actions = %+v", records[0].Actions)
	}
}

func TestDiskMasksSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mask := func(s string) string { return strings.ReplaceAll(s, "sk-secret", "***") }
	rec, err := NewDisk(dir, NewManifest("t"), WithMask(mask))
	if err != nil {
		t.Fatal(err)
	}
	// Secret in reasoning text and in a typed action.
	step := Step{
		Index:   0,
		Text:    "the key is sk-secret",
		Actions: []action.Action{{Type: action.Type, Text: "sk-secret", Mark: action.NoMark}},
	}
	if err := rec.Append(step); err != nil {
		t.Fatal(err)
	}
	_ = rec.Close()

	raw, _ := os.ReadFile(filepath.Join(dir, "steps.jsonl"))
	if strings.Contains(string(raw), "sk-secret") {
		t.Errorf("secret leaked to disk:\n%s", raw)
	}
	if !strings.Contains(string(raw), "***") {
		t.Error("mask token not written")
	}
}

func TestDiskClosedAppend(t *testing.T) {
	t.Parallel()
	rec, err := NewDisk(t.TempDir(), NewManifest("t"))
	if err != nil {
		t.Fatal(err)
	}
	_ = rec.Close()
	if err := rec.Append(Step{}); err != ErrClosed {
		t.Errorf("append after close = %v, want ErrClosed", err)
	}
}

func TestSamplesExport(t *testing.T) {
	t.Parallel()
	records := []Record{
		{Index: 0, ScreenshotFile: "step-0.png", Actions: []action.Action{{Type: action.Click}}},
		{Index: 1, ScreenshotFile: "step-1.png"},
	}
	samples := Samples(records)
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if samples[0].Screenshot != "step-0.png" || samples[0].Reward != 0 || len(samples[0].Actions) != 1 {
		t.Errorf("sample 0 = %+v", samples[0])
	}
}
