package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// all: keeps dotfiles such as .plinth/ in the embedded examples.
//
//go:embed all:examples
var examples embed.FS

// Examples lists the sample projects plinth init can write.
func Examples() []string {
	entries, _ := examples.ReadDir("examples")
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// Example is a complete, runnable project: a blueprint with its own catalog.
func Example(name string) (map[string]string, error) {
	if !slices.Contains(Examples(), name) {
		return nil, fmt.Errorf("unknown example %q; choose one of: %s", name, strings.Join(Examples(), ", "))
	}
	root := "examples/" + name
	files := map[string]string{}
	err := fs.WalkDir(examples, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := examples.ReadFile(path)
		files[strings.TrimPrefix(path, root+"/")] = string(data)
		return err
	})
	return files, err
}
