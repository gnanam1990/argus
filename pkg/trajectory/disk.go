package trajectory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Disk is a Recorder that persists a run to a directory: a manifest.json with
// provenance, a steps.jsonl of step records, and one PNG per observation. The
// same layout reloads for replay and RL/training export. Secrets are masked at
// record time (a leaked credential must never reach disk); result payloads that
// duplicate the next observation are stripped to keep the log lean.
type Disk struct {
	mu     sync.Mutex
	dir    string
	file   *os.File
	enc    *json.Encoder
	mask   func(string) string
	closed bool
}

// DiskOption configures a Disk recorder.
type DiskOption func(*Disk)

// WithMask sets the secret-masking function applied to recorded text.
func WithMask(fn func(string) string) DiskOption {
	return func(d *Disk) { d.mask = fn }
}

// Record is the on-disk shape of a step: the screenshot is a file reference
// (not inline bytes), and secret-bearing text has been masked.
type Record struct {
	Index          int             `json:"index"`
	ScreenshotFile string          `json:"screenshot_file,omitempty"`
	Text           string          `json:"text,omitempty"`
	Actions        []action.Action `json:"actions,omitempty"`
	Usage          model.Usage     `json:"usage"`
}

// NewDisk creates the trajectory directory, writes the manifest, and opens the
// step log for appending.
func NewDisk(dir string, m Manifest, opts ...DiskOption) (*Disk, error) {
	d := &Disk{dir: dir, mask: func(s string) string { return s }}
	for _, o := range opts {
		o(d)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("trajectory: mkdir: %w", err)
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("trajectory: manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		return nil, fmt.Errorf("trajectory: write manifest: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "steps.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("trajectory: open steps: %w", err)
	}
	d.file = f
	d.enc = json.NewEncoder(f)
	return d, nil
}

var _ Recorder = (*Disk)(nil)

// Append masks, writes the screenshot to a PNG file, and appends the record.
func (d *Disk) Append(s Step) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}

	rec := Record{Index: s.Index, Text: d.mask(s.Text), Usage: s.Usage}
	if !s.Screenshot.Empty() {
		name := fmt.Sprintf("step-%d.png", s.Index)
		if err := os.WriteFile(filepath.Join(d.dir, name), s.Screenshot.Data, 0o644); err != nil {
			return fmt.Errorf("trajectory: write screenshot: %w", err)
		}
		rec.ScreenshotFile = name
	}
	for _, a := range s.Actions {
		a.Text = d.mask(a.Text) // typed text may contain secrets
		rec.Actions = append(rec.Actions, a)
	}
	if err := d.enc.Encode(rec); err != nil {
		return fmt.Errorf("trajectory: encode step: %w", err)
	}
	return nil
}

// Close finalizes the recording.
func (d *Disk) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	return d.file.Close()
}

// LoadDisk reloads a recorded trajectory's manifest and step records.
func LoadDisk(dir string) (Manifest, []Record, error) {
	var m Manifest
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, nil, fmt.Errorf("trajectory: read manifest: %w", err)
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		return m, nil, fmt.Errorf("trajectory: parse manifest: %w", err)
	}

	sb, err := os.ReadFile(filepath.Join(dir, "steps.jsonl"))
	if err != nil {
		return m, nil, fmt.Errorf("trajectory: read steps: %w", err)
	}
	var records []Record
	dec := json.NewDecoder(bytes.NewReader(sb))
	for dec.More() {
		var r Record
		if err := dec.Decode(&r); err != nil {
			return m, nil, fmt.Errorf("trajectory: decode step: %w", err)
		}
		records = append(records, r)
	}
	return m, records, nil
}

// Sample is the RL/training export shape: an observation (screenshot file), the
// actions taken, and an externally-assigned reward.
type Sample struct {
	Index      int             `json:"index"`
	Screenshot string          `json:"screenshot"`
	Actions    []action.Action `json:"actions"`
	Reward     float64         `json:"reward"`
}

// Samples converts step records to RL samples (rewards default to 0; a scorer
// assigns them).
func Samples(records []Record) []Sample {
	out := make([]Sample, len(records))
	for i, r := range records {
		out[i] = Sample{Index: r.Index, Screenshot: r.ScreenshotFile, Actions: r.Actions}
	}
	return out
}
