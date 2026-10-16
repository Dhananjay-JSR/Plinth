// Command plinth validates blueprints and renders them into OpenTofu workspace bundles.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/Dhananjay-JSR/Plinth/internal/generate"
	"github.com/Dhananjay-JSR/Plinth/internal/module"
	"github.com/Dhananjay-JSR/Plinth/internal/tofu"
)

const usage = `usage:
  plinth validate [-blueprint DIR] -env NAME
  plinth render   [-blueprint DIR] -env NAME -out DIR
  plinth plan     [-blueprint DIR] -env NAME [-out FILE]
  plinth apply    [-blueprint DIR] -env NAME [-plan FILE] [-auto-approve]
  plinth destroy  [-blueprint DIR] -env NAME [-auto-approve]
  plinth drift    [-blueprint DIR] -env NAME
  plinth module validate DIR
  plinth --version
`

// version is set at release build time with -ldflags "-X main.version=<v>".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run returns the exit code: 0 ok, 1 invalid configuration or failure, 2 bad
// usage (or, for drift, drift found).
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "--version", "version":
		fmt.Fprintln(stdout, "plinth", version)
		return 0
	case "validate", "render":
		return blueprintCommand(args[0], args[1:], stdout, stderr)
	case "plan":
		return planCommand(args[1:], stdout, stderr)
	case "apply":
		return applyCommand(args[1:], stdin, stdout, stderr)
	case "destroy":
		return destroyCommand(args[1:], stdin, stdout, stderr)
	case "drift":
		return driftCommand(args[1:], stdout, stderr)
	case "module":
		if len(args) == 3 && args[1] == "validate" {
			return moduleValidate(args[2], stdout, stderr)
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func blueprintCommand(cmd string, args []string, stdout, stderr io.Writer) int {
	fs, dir, env := envFlags(cmd, stderr)
	out := fs.String("out", "", "output directory for the workspace bundle (render)")
	if err := fs.Parse(args); err != nil {
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

// moduleValidate checks a module's contract, then runs tofu validate on a copy
// with Plinth's generated variables added, as it would be in a bundle.
func moduleValidate(dir string, stdout, stderr io.Writer) int {
	m, err := module.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	tf, err := tofu.Find(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	tmp, err := os.MkdirTemp("", "plinth-module-")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer os.RemoveAll(tmp)
	if err := generate.CopyDir(dir, tmp); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(tmp, module.VariablesFile), []byte(module.Variables), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// init output is noise unless it fails; validate's own output is the result.
	if out, code, err := tf.Output(tmp, "init", "-backend=false", "-input=false", "-no-color"); err != nil || code != 0 {
		fmt.Fprintf(stderr, "%s%stofu init failed\n", out, errText(err))
		return 1
	}
	if code, err := tf.Run(tmp, "validate", "-no-color"); err != nil || code != 0 {
		fmt.Fprintf(stderr, "%s%s: OpenTofu code is invalid\n", errText(err), m.ID())
		return 1
	}
	fmt.Fprintf(stdout, "%s: module valid\n", m.ID())
	return 0
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error() + "\n"
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
