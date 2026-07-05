// Package approval is the persistent per-app approval store for computer use:
// which apps the agent is allowed to drive. It is deny-by-default — an unknown
// app is Pending, never Approved — and writes atomically so a crash can't leave
// a half-written file.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Decision is the stored approval state for one app.
type Decision string

const (
	// Approved means the agent may drive the app.
	Approved Decision = "approved"
	// Denied means the agent must not drive the app.
	Denied Decision = "denied"
	// Pending is the default for any app with no recorded decision.
	Pending Decision = "pending"
)

// Record is one persisted approval decision.
type Record struct {
	BundleIdentifier string    `json:"bundle_identifier"`
	Decision         Decision  `json:"decision"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Store reads and writes per-app approval decisions.
type Store interface {
	Get(ctx context.Context, bundleID string) (Decision, error)
	Set(ctx context.Context, bundleID string, d Decision) error
	Remove(ctx context.Context, bundleID string) error
	List(ctx context.Context) ([]Record, error)
}

// FileStore is a JSON-file-backed Store with an in-memory cache. now is
// injectable for tests.
type FileStore struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	records map[string]Record
	loaded  bool
	modTime time.Time // mtime of the file at last load, to detect external writes
}

// Option configures a FileStore.
type Option func(*FileStore)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) Option { return func(s *FileStore) { s.now = now } }

// DefaultPath returns the default approvals file, <config>/argus/cu-approvals.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("approval: user config dir: %w", err)
	}
	return filepath.Join(dir, "argus", "cu-approvals.json"), nil
}

// NewFileStore builds a store backed by path (created on first write).
func NewFileStore(path string, opts ...Option) *FileStore {
	s := &FileStore{path: path, now: time.Now, records: map[string]Record{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

var _ Store = (*FileStore)(nil)

// Get returns the decision for bundleID, or Pending if none is recorded.
func (s *FileStore) Get(_ context.Context, bundleID string) (Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Pending, err
	}
	if r, ok := s.records[bundleID]; ok {
		return r.Decision, nil
	}
	return Pending, nil
}

// Set records a decision and persists the store.
func (s *FileStore) Set(_ context.Context, bundleID string, d Decision) error {
	if d != Approved && d != Denied && d != Pending {
		return fmt.Errorf("approval: invalid decision %q", d)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	s.records[bundleID] = Record{BundleIdentifier: bundleID, Decision: d, UpdatedAt: s.now().UTC()}
	return s.save()
}

// Remove deletes a decision (the app reverts to Pending).
func (s *FileStore) Remove(_ context.Context, bundleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	delete(s.records, bundleID)
	return s.save()
}

// List returns all records, sorted by bundle id.
func (s *FileStore) List(_ context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BundleIdentifier < out[j].BundleIdentifier })
	return out, nil
}

// MigrateFrom imports records from a legacy file if the current store is empty
// and the legacy file exists. It never overwrites existing decisions.
func (s *FileStore) MigrateFrom(legacyPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	if len(s.records) > 0 {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("approval: read legacy: %w", err)
	}
	var recs []Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return fmt.Errorf("approval: parse legacy: %w", err)
	}
	for _, r := range recs {
		if r.BundleIdentifier != "" {
			s.records[r.BundleIdentifier] = r
		}
	}
	return s.save()
}

// load reads the file into the cache, re-reading whenever the file's mtime has
// changed since the last load so a long-running process (e.g. the argus-mcp
// server) observes approvals/revocations made by a separate `argus cu
// approvals` invocation instead of trusting a stale startup snapshot — approval
// is the human safety gate, so a mid-session revoke must take effect. A missing
// file is an empty store (and stays watched, so a later first write is picked
// up). The file write is atomic (temp + rename, see save), so a concurrent
// reader never sees a partial file.
func (s *FileStore) load() error {
	info, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing file: empty store. Reset any prior cache so a revoke that
			// deletes the file is honored, and keep watching for (re)creation.
			s.records = map[string]Record{}
			s.modTime = time.Time{}
			s.loaded = true
			return nil
		}
		return fmt.Errorf("approval: stat: %w", err)
	}
	if s.loaded && info.ModTime().Equal(s.modTime) {
		return nil // cache still reflects the file on disk
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("approval: read: %w", err)
	}
	var recs []Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return fmt.Errorf("approval: parse: %w", err)
	}
	fresh := make(map[string]Record, len(recs))
	for _, r := range recs {
		if r.BundleIdentifier != "" {
			fresh[r.BundleIdentifier] = r
		}
	}
	s.records = fresh
	s.modTime = info.ModTime()
	s.loaded = true
	return nil
}

// save writes the cache atomically (temp file + rename) at 0600.
func (s *FileStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("approval: mkdir: %w", err)
	}
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].BundleIdentifier < recs[j].BundleIdentifier })
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("approval: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("approval: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("approval: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("approval: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("approval: close: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("approval: rename: %w", err)
	}
	// Record the mtime of what we just wrote so the next load() doesn't need to
	// re-read our own write; an external writer will still bump it past this.
	if info, err := os.Stat(s.path); err == nil {
		s.modTime = info.ModTime()
	}
	return nil
}
