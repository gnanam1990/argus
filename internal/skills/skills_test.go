package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListBuiltins(t *testing.T) {
	t.Parallel()
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, s := range list {
		names[s.Name] = s.Description
		if s.Description == "" {
			t.Errorf("skill %q has no description", s.Name)
		}
	}
	for _, want := range []string{"macos-basics", "computer-use-safety", "web-basics"} {
		if _, ok := names[want]; !ok {
			t.Errorf("built-in skill %q missing from List()", want)
		}
	}
}

func TestResolveWebBasics(t *testing.T) {
	t.Parallel()
	got, err := Resolve([]string{"web-basics"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"web browser", "wait for the page", "Login walls"} {
		if !strings.Contains(got, want) {
			t.Errorf("web-basics guidance missing %q", want)
		}
	}
}

func TestResolveBuiltins(t *testing.T) {
	t.Parallel()
	got, err := Resolve([]string{"computer-use-safety", "macos-basics"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Combined guidance carries both skills' bodies.
	for _, want := range []string{"Acting safely", "prompt-injection", "Operating macOS", "cmd+space"} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved guidance missing %q", want)
		}
	}
}

func TestResolveEmpty(t *testing.T) {
	t.Parallel()
	got, err := Resolve(nil, "", nil)
	if err != nil || got != "" {
		t.Errorf("empty names should give empty guidance, got %q / %v", got, err)
	}
}

func TestResolveUnknownErrors(t *testing.T) {
	t.Parallel()
	_, err := Resolve([]string{"does-not-exist"}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("unknown skill should error naming it, got %v", err)
	}
}

func TestResolveExternalDirWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: custom\ndescription: a custom skill\n---\n\nDo the custom thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "custom", "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve([]string{"custom"}, dir, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Do the custom thing") {
		t.Errorf("external skill not loaded: %q", got)
	}

	// A name only in extraDir must still resolve; a bad extraDir falls back to
	// built-ins.
	got, err = Resolve([]string{"macos-basics"}, dir, os.ReadFile)
	if err != nil || !strings.Contains(got, "Operating macOS") {
		t.Errorf("built-in should still resolve when not in extraDir: %v", err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	t.Parallel()
	s, err := parse([]byte("---\nname: x\ndescription: hi there\n---\n\nBody text here.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "x" || s.Description != "hi there" || strings.TrimSpace(s.Body) != "Body text here." {
		t.Errorf("parse = %+v", s)
	}

	for _, bad := range []string{"no frontmatter", "---\nname: x\nunterminated", "---\ndescription: no name\n---\nbody"} {
		if _, err := parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
