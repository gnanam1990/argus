// Package viewer serves a self-contained replay UI for one recorded
// trajectory directory (see pkg/trajectory): a single HTML page that steps
// through the run's screenshots, reasoning, actions, and results, its JSON
// data feed, and the step screenshots themselves. It is self-contained
// (net/http + embed only), so the CLI can mount it as a standalone `argus
// view` server without pulling in a web framework.
package viewer

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gnanam1990/argus/pkg/trajectory"
)

// pageHTML is the single-page replay UI: inline CSS and JS, no CDN or
// external fonts. taskPlaceholder is substituted with the run's task before
// each response, so the task name is visible in the raw HTML (page title and
// header) without waiting on the JavaScript that renders everything else.
//
//go:embed viewer.html
var pageHTML string

const taskPlaceholder = "%%ARGUS_TASK%%"

// shotPattern is the only filename shape /shots/ will serve: exactly what
// trajectory.Disk writes for a step's screenshot. Anything else - including a
// path-traversal attempt - is rejected before the filesystem is touched.
var shotPattern = regexp.MustCompile(`^step-[0-9]+\.png$`)

// Server serves the replay UI for one recorded trajectory directory. Build
// one with New and mount its Handler.
type Server struct {
	dir      string
	manifest trajectory.Manifest
	records  []trajectory.Record
}

// New loads the manifest and step records from dir, as written by
// trajectory.Disk, and returns a Server ready to handle requests. It returns
// a clear, wrapped error when dir does not hold a valid trajectory (a
// missing or unparseable manifest.json or steps.jsonl).
func New(dir string) (*Server, error) {
	m, records, err := trajectory.LoadDisk(dir)
	if err != nil {
		return nil, fmt.Errorf("viewer: load trajectory from %s: %w", dir, err)
	}
	return &Server{dir: dir, manifest: m, records: records}, nil
}

// Handler returns the HTTP handler serving the replay page at "/", its JSON
// data feed at "/api/trajectory", and step screenshots under "/shots/".
//
// Routing is hand-rolled rather than http.ServeMux: ServeMux canonicalizes
// every request path before a registered handler ever sees it, and silently
// 30x-redirects any path that needed cleaning (e.g. one containing "..") -
// which would turn a path-traversal probe into a redirect instead of the
// flat 404 that "/shots/" must return.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.route)
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		s.handleIndex(w, r)
	case r.URL.Path == "/api/trajectory":
		s.handleTrajectory(w, r)
	case strings.HasPrefix(r.URL.Path, "/shots/"):
		s.handleShot(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleIndex serves the embedded replay page with the task name substituted
// in, HTML-escaped so an unusual task string can never break out of the
// title/header text it is placed into.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	task := s.manifest.Task
	if task == "" {
		task = "(untitled trajectory)"
	}
	page := strings.ReplaceAll(pageHTML, taskPlaceholder, html.EscapeString(task))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

// trajectoryResponse is the /api/trajectory shape: the run's provenance and
// every recorded step, in order. Per-step usage already lives on each
// element of Steps, so the client sums totals itself rather than this
// package duplicating that arithmetic server-side.
type trajectoryResponse struct {
	Manifest trajectory.Manifest `json:"manifest"`
	Steps    []trajectory.Record `json:"steps"`
}

// handleTrajectory serves the run's manifest and step records as JSON.
func (s *Server) handleTrajectory(w http.ResponseWriter, r *http.Request) {
	steps := s.records
	if steps == nil {
		steps = []trajectory.Record{}
	}
	w.Header().Set("Content-Type", "application/json")
	resp := trajectoryResponse{Manifest: s.manifest, Steps: steps}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "viewer: encode trajectory: "+err.Error(), http.StatusInternalServerError)
	}
}

// handleShot serves one screenshot file. name is validated against
// shotPattern - digits and a fixed prefix/suffix only, never a "/" or a ".."
// - so filepath.Join(s.dir, name) can never resolve outside s.dir.
func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/shots/")
	if !shotPattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.dir, name))
}
