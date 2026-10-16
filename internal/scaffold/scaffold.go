// Package scaffold writes starter blueprints, modules and resource instances.
package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Gitignore keeps generated workspaces and local state, which can hold
// secrets, out of git.
const Gitignore = "build/\nstate/\n"

// Write creates every file under dir, or none: if any already exists it
// refuses and lists them, so hand-written work is never overwritten.
func Write(dir string, files map[string]string) ([]string, error) {
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	slices.Sort(paths)

	var existing []string
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			existing = append(existing, filepath.Join(dir, rel))
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("refusing to overwrite existing files:\n  %s", strings.Join(existing, "\n  "))
	}

	for _, rel := range paths {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(files[rel]), 0o644); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// Blueprint is an empty blueprint with one environment.
func Blueprint() map[string]string {
	return map[string]string{
		"blueprint.yaml": "# Marks the root of a Plinth blueprint.\n",
		".plinth/project.yaml": `# Catalogs this project draws modules from: a local name → a folder holding
# modules/<kind>/<flavor>/<version>/. The name is only a nickname.
catalogs:
  core: ../catalog
`,
		".plinth/environments/dev.yaml": `# Sparse changes for this environment, by resource instance address:
# overrides:
#   application/checkout-api:
#     spec: { replicas: 2 }
#   network/nat:
#     disabled: true
overrides: {}
`,
		".plinth/.gitignore": Gitignore,
	}
}
