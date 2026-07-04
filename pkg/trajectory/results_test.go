package trajectory_test

import (
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// Result output must be recorded (masked), with result screenshots stripped —
// they duplicate the next observation's PNG.
func TestDiskRecordsResults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() + "/run"
	mask := func(s string) string { return strings.ReplaceAll(s, "hunter2", "«redacted»") }
	d, err := trajectory.NewDisk(dir, trajectory.NewManifest("t"), trajectory.WithMask(mask))
	if err != nil {
		t.Fatal(err)
	}

	step := trajectory.Step{
		Index:   0,
		Actions: []action.Action{{Type: action.RunCommand, Text: "cat cred", Mark: action.NoMark}},
		Results: []action.Result{{
			Output:     "password is hunter2",
			Screenshot: action.Image{Data: []byte("PNGDATA"), MIME: action.MIMEPNG},
		}},
	}
	if err := d.Append(step); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	_, records, err := trajectory.LoadDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Results) != 1 {
		t.Fatalf("records = %+v", records)
	}
	res := records[0].Results[0]
	if res.Output != "password is «redacted»" {
		t.Errorf("result output not masked: %q", res.Output)
	}
	if !res.Screenshot.Empty() {
		t.Error("result screenshots must be stripped from the JSONL")
	}
}
