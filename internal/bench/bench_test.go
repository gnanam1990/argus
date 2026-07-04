package bench_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/bench"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// fakeGrounder is an inline, scriptable grounder.Grounder: it returns a fixed
// set of elements (or an error) regardless of the image it's given.
type fakeGrounder struct {
	els []grounder.Element
	err error
}

func (g fakeGrounder) Detect(context.Context, action.Image) ([]grounder.Element, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.els, nil
}

var _ grounder.Grounder = fakeGrounder{}

func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

// writePNG writes a tiny solid w×h PNG to path.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.Gray{Y: 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeManifest(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write cases.json: %v", err)
	}
}

func approxEqual(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// --- LoadDataset ---

func TestLoadDatasetValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), 10, 10)
	writeManifest(t, dir, `{"cases":[{"image":"a.png","instruction":"click submit","box":[1,2,3,4]}]}`)

	cases, err := bench.LoadDataset(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	want := bench.Case{Image: "a.png", Instruction: "click submit", Box: rect(1, 2, 3, 4)}
	if cases[0] != want {
		t.Errorf("case = %+v, want %+v", cases[0], want)
	}
}

func TestLoadDatasetMissingManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no cases.json written
	if _, err := bench.LoadDataset(dir); err == nil {
		t.Error("expected an error for a missing manifest")
	}
}

func TestLoadDatasetMissingImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// "missing.png" is referenced but never written to dir.
	writeManifest(t, dir, `{"cases":[{"image":"missing.png","instruction":"x","box":[0,0,10,10]}]}`)

	_, err := bench.LoadDataset(dir)
	if err == nil {
		t.Fatal("expected an error for a missing image file")
	}
	if !strings.Contains(err.Error(), "missing.png") {
		t.Errorf("error = %v, want it to name the missing image", err)
	}
}

func TestLoadDatasetEmptyImageName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A blank image name must not silently resolve to dir itself.
	writeManifest(t, dir, `{"cases":[{"image":"","instruction":"x","box":[0,0,10,10]}]}`)

	if _, err := bench.LoadDataset(dir); err == nil {
		t.Error("expected an error for an empty image filename")
	}
}

func TestLoadDatasetMalformedBox(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		box  string
	}{
		{"too few numbers", "[0,0,10]"},
		{"x2 equal x1", "[5,0,5,10]"},
		{"x2 less than x1", "[10,0,5,10]"},
		{"y2 equal y1", "[0,5,10,5]"},
		{"y2 less than y1", "[0,10,10,5]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writePNG(t, filepath.Join(dir, "a.png"), 10, 10)
			writeManifest(t, dir, fmt.Sprintf(`{"cases":[{"image":"a.png","instruction":"x","box":%s}]}`, tt.box))

			if _, err := bench.LoadDataset(dir); err == nil {
				t.Errorf("box %s: expected a malformed-box error", tt.box)
			}
		})
	}
}

// --- Run ---

func TestRunHitMissAndErrorContinues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "hit.png"), 50, 50)
	writePNG(t, filepath.Join(dir, "miss.png"), 50, 50)
	writePNG(t, filepath.Join(dir, "err.png"), 50, 50)
	writeManifest(t, dir, `{"cases":[
		{"image":"hit.png","instruction":"click the submit button","box":[25,25,35,35]},
		{"image":"miss.png","instruction":"click the submit button","box":[0,0,10,10]},
		{"image":"err.png","instruction":"open the settings panel","box":[0,0,10,10]}
	]}`)

	g := fakeGrounder{els: []grounder.Element{
		{ID: 0, Box: rect(0, 0, 10, 10), Label: "Cancel", Interactable: true, Confidence: 0.9},
		{ID: 1, Box: rect(20, 20, 40, 40), Label: "Submit button", Interactable: true, Confidence: 0.9},
	}}
	p := bench.GrounderPointer(g, 0.5)

	rep, err := bench.Run(context.Background(), dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 3 {
		t.Fatalf("total = %d, want 3", rep.Total)
	}
	if rep.Hits != 1 {
		t.Fatalf("hits = %d, want 1", rep.Hits)
	}
	if !approxEqual(rep.Accuracy, 1.0/3.0, 1e-9) {
		t.Errorf("accuracy = %v, want 1/3", rep.Accuracy)
	}

	by := make(map[string]bench.CaseResult, len(rep.Results))
	for _, r := range rep.Results {
		by[r.Image] = r
	}

	// Submit button's box (20,20)-(40,40) centers at (30,30), inside hit.png's box.
	if hit := by["hit.png"]; !hit.Hit || hit.Point != (action.Point{X: 30, Y: 30}) || hit.Err != "" {
		t.Errorf("hit.png result = %+v", hit)
	}
	// Same predicted point (30,30), but miss.png's box is Cancel's box (0,0)-(10,10) — outside.
	if miss := by["miss.png"]; miss.Hit || miss.Err != "" {
		t.Errorf("miss.png result = %+v, want a clean miss with no error", miss)
	}
	// "open the settings panel" matches neither Cancel nor Submit — GrounderPointer
	// errors, Run records it, and (per Total==3) the run still finished all three cases.
	if bad := by["err.png"]; bad.Hit || bad.Err == "" {
		t.Errorf("err.png result = %+v, want a recorded error", bad)
	}
}

func TestRunEmptyDataset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, `{"cases":[]}`)

	p := bench.FuncPointer(func(context.Context, action.Image, string) (action.Point, error) {
		t.Fatal("pointer must not be called for an empty dataset")
		return action.Point{}, nil
	})
	rep, err := bench.Run(context.Background(), dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 0 || rep.Hits != 0 || rep.Accuracy != 0 || len(rep.Results) != 0 {
		t.Errorf("empty-dataset report = %+v, want all zero", rep)
	}
}

func TestRunLoadDatasetErrorPropagates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no cases.json
	p := bench.FuncPointer(func(context.Context, action.Image, string) (action.Point, error) {
		return action.Point{}, nil
	})
	if _, err := bench.Run(context.Background(), dir, p); err == nil {
		t.Error("expected the dataset load error to propagate")
	}
}

func TestRunReadImageErrorRecordedAndContinues(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permission bits")
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "a.png")
	writePNG(t, imgPath, 10, 10)
	writeManifest(t, dir, `{"cases":[{"image":"a.png","instruction":"x","box":[0,0,10,10]}]}`)

	// os.Stat (LoadDataset's existence check) only needs directory search
	// permission, but os.ReadFile (Run's per-case load) needs read permission on
	// the file itself. Revoking it reproduces a case that passes LoadDataset's
	// validation yet still fails to load inside Run.
	if err := os.Chmod(imgPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(imgPath, 0o600) }) // let TempDir cleanup remove it

	p := bench.FuncPointer(func(context.Context, action.Image, string) (action.Point, error) {
		t.Fatal("pointer must not be called when the image can't be read")
		return action.Point{}, nil
	})
	rep, err := bench.Run(context.Background(), dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 1 || rep.Hits != 0 || len(rep.Results) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Results[0].Err == "" || rep.Results[0].Hit {
		t.Errorf("result = %+v, want a recorded read error", rep.Results[0])
	}
}

func TestReportJSON(t *testing.T) {
	t.Parallel()
	rep := bench.Report{Total: 2, Hits: 1, Accuracy: 0.5, Results: []bench.CaseResult{
		{Image: "a.png", Instruction: "click a", Hit: true, Point: action.Point{X: 1, Y: 2}},
	}}
	j := string(rep.JSON())
	for _, want := range []string{`"total": 2`, `"hits": 1`, `"accuracy": 0.5`, `"image": "a.png"`} {
		if !strings.Contains(j, want) {
			t.Errorf("json = %s, want it to contain %q", j, want)
		}
	}
}

// --- FuncPointer ---

func TestFuncPointerDelegates(t *testing.T) {
	t.Parallel()
	called := false
	f := bench.FuncPointer(func(_ context.Context, _ action.Image, instruction string) (action.Point, error) {
		called = true
		if instruction != "click x" {
			t.Errorf("instruction = %q, want %q", instruction, "click x")
		}
		return action.Point{X: 7, Y: 9}, nil
	})

	pt, err := f.PredictPoint(context.Background(), action.Image{}, "click x")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("underlying function was not called")
	}
	if pt != (action.Point{X: 7, Y: 9}) {
		t.Errorf("point = %v, want (7,9)", pt)
	}
}

func TestFuncPointerErrorPropagates(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	f := bench.FuncPointer(func(context.Context, action.Image, string) (action.Point, error) {
		return action.Point{}, wantErr
	})

	if _, err := f.PredictPoint(context.Background(), action.Image{}, "x"); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// --- GrounderPointer / matching ---

// TestGrounderPointerPrefersBestOverlapMatch is the scorer's motivating
// example: a long candidate that happens to contain the wanted word must
// lose to a short, precise label for the same word once the score is
// normalized by sqrt(candidate token count).
func TestGrounderPointerPrefersBestOverlapMatch(t *testing.T) {
	t.Parallel()
	g := fakeGrounder{els: []grounder.Element{
		{ID: 0, Box: rect(0, 0, 100, 20), Label: "Click here to submit your order now", Interactable: true, Confidence: 0.9},
		{ID: 1, Box: rect(0, 30, 20, 50), Label: "Submit", Interactable: true, Confidence: 0.9},
	}}
	p := bench.GrounderPointer(g, 0.5)

	pt, err := p.PredictPoint(context.Background(), action.Image{}, "click submit")
	if err != nil {
		t.Fatal(err)
	}
	want := rect(0, 30, 20, 50).Center()
	if pt != want {
		t.Errorf("point = %v, want %v (the 'Submit' element)", pt, want)
	}
}

// TestGrounderPointerExactLabelWinsOverNoisyText exercises the "exact label
// match wins outright" rule as a decisive tie-breaker, not merely a
// restatement of the overlap score: element 0's combined Label+Text overlap
// score (1/sqrt(7) ~= 0.38, diluted by its long Text) would lose to element
// 1's short Text-only match (1/sqrt(1) = 1.0) if the exact-label shortcut
// did not intervene.
func TestGrounderPointerExactLabelWinsOverNoisyText(t *testing.T) {
	t.Parallel()
	g := fakeGrounder{els: []grounder.Element{
		{ID: 0, Box: rect(0, 0, 10, 10), Label: "Submit", Text: "Submit your order now immediately please", Interactable: true, Confidence: 0.9},
		{ID: 1, Box: rect(20, 20, 30, 30), Label: "", Text: "Submit", Interactable: true, Confidence: 0.9},
	}}
	p := bench.GrounderPointer(g, 0.5)

	pt, err := p.PredictPoint(context.Background(), action.Image{}, "submit")
	if err != nil {
		t.Fatal(err)
	}
	want := rect(0, 0, 10, 10).Center()
	if pt != want {
		t.Errorf("point = %v, want %v (exact label match should win outright)", pt, want)
	}
}

func TestGrounderPointerNoMatchErrors(t *testing.T) {
	t.Parallel()
	g := fakeGrounder{els: []grounder.Element{
		{ID: 0, Box: rect(0, 0, 10, 10), Label: "Cancel", Interactable: true, Confidence: 0.9},
	}}
	p := bench.GrounderPointer(g, 0.5)

	if _, err := p.PredictPoint(context.Background(), action.Image{}, "open settings menu"); err == nil {
		t.Error("expected an error when no element matches")
	}
}

func TestGrounderPointerFiltersConfidence(t *testing.T) {
	t.Parallel()
	g := fakeGrounder{els: []grounder.Element{
		{ID: 0, Box: rect(0, 0, 10, 10), Label: "Submit", Interactable: true, Confidence: 0.2},
	}}
	p := bench.GrounderPointer(g, 0.5) // 0.2 < 0.5 -> filtered out

	if _, err := p.PredictPoint(context.Background(), action.Image{}, "submit"); err == nil {
		t.Error("expected an error: the only element is below minConfidence")
	}
}

func TestGrounderPointerDetectError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("vision down")
	g := fakeGrounder{err: wantErr}
	p := bench.GrounderPointer(g, 0.5)

	if _, err := p.PredictPoint(context.Background(), action.Image{}, "x"); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want it to wrap %v", err, wantErr)
	}
}
