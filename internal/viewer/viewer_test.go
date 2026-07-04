package viewer

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

const fixtureTask = "reconcile the monthly invoice"

// pngBytes builds a decodable w×h PNG, mirroring pkg/agent/runner_test.go's
// pngOf helper, so the fixture's screenshot is a real image rather than
// opaque bytes.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// newFixture records a 2-step trajectory through the real trajectory.Disk
// writer (NewDisk + Append) rather than hand-writing manifest.json/steps.jsonl,
// so the test also proves the viewer reads back exactly what the writer
// produces. Step 0 carries a screenshot and a click result; step 1 carries no
// screenshot but a run_command result with output text.
func newFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	m := trajectory.NewManifest(fixtureTask)
	m.Model = "claude-opus-4-8"
	m.ConfigHash = "cafef00d"

	rec, err := trajectory.NewDisk(dir, m)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}

	err = rec.Append(trajectory.Step{
		Index:      0,
		Screenshot: action.Image{MIME: action.MIMEPNG, Data: pngBytes(t, 20, 10)},
		Text:       "clicking the submit button",
		Actions:    []action.Action{{Type: action.Click, Button: action.Left, Point: action.Point{X: 5, Y: 6}, Mark: action.NoMark}},
		Results:    []action.Result{{}},
		Usage:      model.Usage{InputTokens: 50, OutputTokens: 10},
	})
	if err != nil {
		t.Fatalf("append step 0: %v", err)
	}

	err = rec.Append(trajectory.Step{
		Index:   1,
		Text:    "checking the command output",
		Actions: []action.Action{{Type: action.RunCommand, Text: "echo hello", Mark: action.NoMark}},
		Results: []action.Result{{Output: "hello\n"}},
		Usage:   model.Usage{InputTokens: 20, OutputTokens: 5},
	})
	if err != nil {
		t.Fatalf("append step 1: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return dir
}

func TestNewMissingDir(t *testing.T) {
	t.Parallel()
	_, err := New(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("New on a missing directory: got nil error, want a clear failure")
	}
	if !strings.Contains(err.Error(), "viewer") {
		t.Errorf("error = %q, want it to identify the viewer package", err)
	}
}

func TestNewInvalidManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir); err == nil {
		t.Fatal("New with an invalid manifest.json: got nil error, want a clear failure")
	}
}

func TestIndexServesTaskName(t *testing.T) {
	t.Parallel()
	s, err := New(newFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), fixtureTask) {
		t.Errorf("body does not contain task name %q:\n%s", fixtureTask, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), taskPlaceholder) {
		t.Error("body still contains the unsubstituted task placeholder")
	}
}

func TestAPITrajectory(t *testing.T) {
	t.Parallel()
	s, err := New(newFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/trajectory", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/trajectory status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got trajectoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if got.Manifest.Task != fixtureTask {
		t.Errorf("manifest.task = %q, want %q", got.Manifest.Task, fixtureTask)
	}
	if got.Manifest.Model != "claude-opus-4-8" {
		t.Errorf("manifest.model = %q", got.Manifest.Model)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(got.Steps))
	}
	if got.Steps[0].ScreenshotFile != "step-0.png" {
		t.Errorf("steps[0].screenshot_file = %q, want step-0.png", got.Steps[0].ScreenshotFile)
	}
	if got.Steps[1].ScreenshotFile != "" {
		t.Errorf("steps[1].screenshot_file = %q, want empty", got.Steps[1].ScreenshotFile)
	}
	if len(got.Steps[1].Results) != 1 || got.Steps[1].Results[0].Output != "hello\n" {
		t.Errorf("steps[1].results = %+v, want one run_command result with output", got.Steps[1].Results)
	}
	if got.Steps[0].Usage.InputTokens != 50 || got.Steps[1].Usage.InputTokens != 20 {
		t.Errorf("per-step usage = %+v / %+v", got.Steps[0].Usage, got.Steps[1].Usage)
	}
}

func TestShotsServesPNG(t *testing.T) {
	t.Parallel()
	dir := newFixture(t)
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(dir, "step-0.png"))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shots/step-0.png", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("served %d bytes, want %d matching bytes", rec.Body.Len(), len(want))
	}
}

// TestShotsRejectsTraversalAndUnknownFiles covers path-traversal probes
// (literal ".." segments, which net/http would otherwise 30x-redirect rather
// than reject - see the Handler doc comment), files outside the
// step-<N>.png shape, and a missing-but-well-formed filename.
func TestShotsRejectsTraversalAndUnknownFiles(t *testing.T) {
	t.Parallel()
	s, err := New(newFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	paths := []string{
		"/shots/../manifest.json",
		"/shots/../steps.jsonl",
		"/shots/evil.txt",
		"/shots/step-0.png.bak",
		"/shots/step-x.png",
		"/shots/",
		"/shots/step-99.png", // well-formed name, no such file
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", p, rec.Code)
			}
		})
	}
}

func TestUnknownRouteNotFound(t *testing.T) {
	t.Parallel()
	s, err := New(newFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
