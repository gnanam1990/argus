// Package skills loads reusable guidance ("skills") that is prepended to the
// agent's system prompt for a task. A skill is a SKILL.md file with YAML-style
// frontmatter (name, description) and a markdown body. Built-in skills are
// embedded in the binary, so the single static binary ships with them; an
// operator can add their own from a directory.
//
// Skills are prompt-level guidance, not enforcement: the approval and injection
// middleware are what actually gate risky actions. The computer-use-safety
// skill tells the model when to pause; the middleware makes sure it must.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtin/*/SKILL.md
var builtinFS embed.FS

// Skill is a parsed SKILL.md: frontmatter plus the markdown body.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// FileReader reads a named file; os.ReadFile satisfies it. It is injectable so
// external-directory loading is testable without a filesystem.
type FileReader func(path string) ([]byte, error)

// List returns the built-in skills (name + description, no body), sorted by
// name — for `argus skills`.
func List() ([]Skill, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, fmt.Errorf("skills: read builtin: %w", err)
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := builtinFS.ReadFile(path("builtin", e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		s, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("skills: %s: %w", e.Name(), err)
		}
		out = append(out, Skill{Name: s.Name, Description: s.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Resolve loads the named skills and returns their combined guidance, ready to
// prepend to a system prompt. A name is looked up first in extraDir (when set)
// as <extraDir>/<name>/SKILL.md, then among the built-ins. Unknown names error.
// read defaults to os.ReadFile when nil (injected in tests).
func Resolve(names []string, extraDir string, read FileReader) (string, error) {
	if len(names) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("# Skills\n\nThe following skills give you guidance for this task. Follow them.\n")
	for _, name := range names {
		s, err := load(name, extraDir, read)
		if err != nil {
			return "", err
		}
		b.WriteString("\n---\n\n")
		b.WriteString(strings.TrimSpace(s.Body))
		b.WriteString("\n")
	}
	return b.String(), nil
}

func load(name, extraDir string, read FileReader) (Skill, error) {
	if extraDir != "" && read != nil {
		if data, err := read(filepath.Join(extraDir, name, "SKILL.md")); err == nil {
			return parse(data)
		}
	}
	data, err := builtinFS.ReadFile(path("builtin", name, "SKILL.md"))
	if err != nil {
		return Skill{}, fmt.Errorf("skills: unknown skill %q (see \"argus skills\")", name)
	}
	return parse(data)
}

// parse splits YAML-style frontmatter (name, description) from the body. It is
// intentionally minimal — only the two scalar keys it needs — so the package
// carries no YAML dependency.
func parse(data []byte) (Skill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, fmt.Errorf("skills: missing frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Skill{}, fmt.Errorf("skills: unterminated frontmatter")
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	var s Skill
	for _, line := range strings.Split(front, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = strings.TrimSpace(val)
		case "description":
			s.Description = strings.TrimSpace(val)
		}
	}
	if s.Name == "" {
		return Skill{}, fmt.Errorf("skills: frontmatter missing name")
	}
	s.Body = body
	return s, nil
}

// path joins with forward slashes for embed.FS (always slash-separated,
// regardless of OS).
func path(parts ...string) string { return strings.Join(parts, "/") }
