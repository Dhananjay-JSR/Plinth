package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

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
