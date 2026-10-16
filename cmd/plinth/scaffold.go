package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/module"
	"github.com/Dhananjay-JSR/Plinth/internal/scaffold"
)

func initCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil || fs.NArg() > 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	created, err := scaffold.Write(dir, scaffold.Blueprint())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printCreated(stdout, dir, created)
	fmt.Fprintf(stdout, "\nNext: point .plinth/project.yaml at your catalog, then add a resource instance:\n  plinth add -blueprint %s -module <flavor>/<version> <kind>/<name>\n", dir)
	return 0
}

func printCreated(stdout io.Writer, dir string, files []string) {
	for _, f := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.Join(dir, f))
	}
}

// moduleInit scaffolds a module. Kind, flavor and version come from flags, or
// from a path ending in modules/<kind>/<flavor>/<version>.
func moduleInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("module init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kind := fs.String("kind", "", "module kind (default: from the path)")
	flavor := fs.String("flavor", "", "module flavor (default: from the path)")
	version := fs.String("version", "", "module version (default: from the path)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	dir := fs.Arg(0)
	if *kind == "" && *flavor == "" && *version == "" {
		parts := strings.Split(filepath.ToSlash(filepath.Clean(dir)), "/")
		if n := len(parts); n >= 4 && parts[n-4] == "modules" {
			*kind, *flavor, *version = parts[n-3], parts[n-2], parts[n-1]
		}
	}
	switch {
	case *kind == "" || *flavor == "" || *version == "":
		fmt.Fprintln(stderr, "set -kind, -flavor and -version, or use a path ending in modules/<kind>/<flavor>/<version>")
		return 2
	case !module.KindPattern.MatchString(*kind):
		fmt.Fprintf(stderr, "kind %q must be lower_snake_case without double underscores\n", *kind)
		return 1
	case !module.IdentPattern.MatchString(*flavor):
		fmt.Fprintf(stderr, "flavor %q must be lower_snake_case\n", *flavor)
		return 1
	case !module.VersionPattern.MatchString(*version):
		fmt.Fprintf(stderr, "version %q is invalid\n", *version)
		return 1
	}
	created, err := scaffold.Write(dir, scaffold.Module(*kind, *flavor, *version))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printCreated(stdout, dir, created)
	fmt.Fprintf(stdout, "\nNext: edit module.yaml and main.tf, then: plinth module validate %s\n", dir)
	return 0
}
