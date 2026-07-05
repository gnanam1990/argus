package main

import (
	"fmt"
	"io"

	"github.com/gnanam1990/argus/internal/skills"
)

// skillsCmd lists the built-in guidance skills.
func skillsCmd(out io.Writer) error {
	list, err := skills.List()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "built-in skills (add to a config's agent.skills list):")
	for _, s := range list {
		fmt.Fprintf(out, "  %-22s %s\n", s.Name, s.Description)
	}
	fmt.Fprintln(out, "\nProvide your own from a directory via ARGUS_SKILLS_DIR=<dir> (<dir>/<name>/SKILL.md).")
	return nil
}
