// Command plinth validates blueprints and renders them into OpenTofu workspace bundles.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/Dhananjay-JSR/Plinth/internal/generate"
)

const usage = `usage:
  plinth validate [-blueprint DIR] -env NAME
  plinth render   [-blueprint DIR] -env NAME -out DIR
  plinth --version
`

// version is set at release build time with -ldflags "-X main.version=<v>".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run returns the exit code: 0 ok, 1 invalid configuration or failure, 2 bad usage.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintln(stdout, "plinth", version)
		return 0
	}
	if len(args) == 0 || (args[0] != "validate" && args[0] != "render") {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("blueprint", ".", "blueprint directory")
	env := fs.String("env", "", "environment name")
	out := fs.String("out", "", "output directory for the workspace bundle (render)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *env == "" || (cmd == "render" && *out == "") || fs.NArg() > 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	cfg, err := resolve(*dir, *env)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if cmd == "validate" {
		fmt.Fprintf(stdout, "%s: %d resource instances valid\n", *env, len(cfg.Instances))
		return 0
	}
	if err := generate.Render(cfg, *out); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: rendered %d resource instances to %s\n", *env, len(cfg.Instances), *out)
	return 0
}

func resolve(dir, env string) (*blueprint.Config, error) {
	bp, err := blueprint.Load(dir)
	if err != nil {
		return nil, err
	}
	e, err := blueprint.LoadEnvironment(dir, env)
	if err != nil {
		return nil, err
	}
	return blueprint.Resolve(bp, e)
}
