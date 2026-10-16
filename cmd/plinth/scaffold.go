package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/Dhananjay-JSR/Plinth/internal/catalog"
	"github.com/Dhananjay-JSR/Plinth/internal/module"
	"github.com/Dhananjay-JSR/Plinth/internal/scaffold"
)

func initCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	example := fs.String("example", "", "write a runnable sample project: "+strings.Join(scaffold.Examples(), " or "))
	if err := fs.Parse(args); err != nil || fs.NArg() > 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	files := scaffold.Blueprint()
	if *example != "" {
		var err error
		if files, err = scaffold.Example(*example); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	created, err := scaffold.Write(dir, files)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printCreated(stdout, dir, created)
	if *example != "" {
		fmt.Fprintf(stdout, "\nNext:\n  cd %s\n  plinth apply -env dev\n", dir)
		if *example == "docker" {
			fmt.Fprintln(stdout, "  (needs Docker; set DOCKER_HOST=ssh://user@host to use a remote one. Uses 127.0.0.1:18081 and :18082.)")
		}
		return 0
	}
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

// addCommand scaffolds <kind>/instances/<name>.json for a module, connecting
// each input to the one existing instance that produces its type, if there is one.
func addCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("blueprint", ".", "blueprint directory")
	mod := fs.String("module", "", "module as <flavor>/<version>, of the instance's kind")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || *mod == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	kind, name, ok := strings.Cut(fs.Arg(0), "/")
	flavor, version, ok2 := strings.Cut(*mod, "/")
	switch {
	case !ok || !module.KindPattern.MatchString(kind) || !blueprint.NamePattern.MatchString(name):
		fmt.Fprintf(stderr, "%q is not a resource instance address <kind>/<name>\n", fs.Arg(0))
		return 2
	case !ok2:
		fmt.Fprintf(stderr, "-module %q must be <flavor>/<version>\n", *mod)
		return 2
	}

	bp, err := blueprint.Load(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	m, err := catalog.Resolve(bp.Catalogs, kind, flavor, version)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// Which output types each existing instance produces.
	produces := map[string][]string{} // output type → instance addresses
	for addr, inst := range bp.Instances {
		if r, err := catalog.Resolve(bp.Catalogs, inst.Kind, inst.Flavor, inst.Version); err == nil {
			for _, out := range r.Module.Outputs {
				if !slices.Contains(produces[out.Type], addr) {
					produces[out.Type] = append(produces[out.Type], addr)
				}
			}
		}
	}
	inputs := map[string]blueprint.InputRef{}
	var notes []string
	names := slices.Sorted(maps.Keys(m.Module.Inputs))
	for _, in := range names {
		decl := m.Module.Inputs[in]
		candidates := produces[decl.Type]
		slices.Sort(candidates)
		switch {
		case len(candidates) == 1:
			k, n, _ := strings.Cut(candidates[0], "/")
			inputs[in] = blueprint.InputRef{Kind: k, Name: n}
			notes = append(notes, fmt.Sprintf("inputs.%s connected to %s", in, candidates[0]))
		case decl.Optional:
			notes = append(notes, fmt.Sprintf("inputs.%s is optional (%s); left unconnected", in, decl.Type))
		case len(candidates) == 0:
			inputs[in] = blueprint.InputRef{Kind: "<kind>", Name: "<name>"}
			notes = append(notes, fmt.Sprintf("fill in: inputs.%s — nothing produces %s yet", in, decl.Type))
		default:
			inputs[in] = blueprint.InputRef{Kind: "<kind>", Name: "<name>"}
			notes = append(notes, fmt.Sprintf("fill in: inputs.%s — choose one of [%s]", in, strings.Join(candidates, ", ")))
		}
	}

	body, todo := scaffold.Instance(m.Module, inputs)
	rel := filepath.Join(kind, "instances", name+".json")
	if _, err := scaffold.Write(*dir, map[string]string{rel: body}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "created %s\n", filepath.Join(*dir, rel))
	for _, n := range notes {
		fmt.Fprintf(stdout, "  %s\n", n)
	}
	for _, f := range todo {
		fmt.Fprintf(stdout, "  fill in: %s\n", f)
	}
	return 0
}
