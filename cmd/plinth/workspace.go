package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/Dhananjay-JSR/Plinth/internal/generate"
	"github.com/Dhananjay-JSR/Plinth/internal/scaffold"
	"github.com/Dhananjay-JSR/Plinth/internal/tofu"
)

const (
	planFile         = "tfplan"
	manifestSuffix   = ".manifest.json"         // saved next to a plan file to detect staleness
	defaultStatePath = "../../state/%s.tfstate" // relative to .plinth/build/<env>
)

// workspace is a rendered, initialized environment ready for tofu commands.
type workspace struct {
	env, dir string
	tf       *tofu.Runner
}

// prepare renders the environment into .plinth/build/<env> and runs tofu init.
func prepare(bpDir, env string, stdout, stderr io.Writer) (*workspace, error) {
	cfg, err := resolve(bpDir, env)
	if err != nil {
		return nil, err
	}
	tf, err := tofu.Find(stdout, stderr)
	if err != nil {
		return nil, err
	}
	if cfg.State == nil {
		cfg.State = &blueprint.State{Backend: "local", Config: map[string]any{"path": fmt.Sprintf(defaultStatePath, env)}}
	}
	plinthDir := filepath.Join(bpDir, ".plinth")
	// State can hold secrets: keep it and the build output out of git.
	if _, err := os.Stat(filepath.Join(plinthDir, ".gitignore")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(plinthDir, ".gitignore"), []byte(scaffold.Gitignore), 0o644); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(plinthDir, "state"), 0o755); err != nil {
		return nil, err
	}

	dir := filepath.Join(plinthDir, "build", env)
	next := dir + ".next"
	if err := os.RemoveAll(next); err != nil {
		return nil, err
	}
	if err := generate.Render(cfg, next); err != nil {
		return nil, err
	}
	// Keep tofu's provider cache and lock file across renders.
	for _, keep := range []string{".terraform", ".terraform.lock.hcl"} {
		if err := os.Rename(filepath.Join(dir, keep), filepath.Join(next, keep)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.Rename(next, dir); err != nil {
		return nil, err
	}

	out, code, err := tf.Output(dir, "init", "-input=false", "-no-color")
	if err != nil || code != 0 {
		return nil, fmt.Errorf("%s%stofu init failed", out, errText(err))
	}
	return &workspace{env: env, dir: dir, tf: tf}, nil
}

// plan writes a saved plan into the workspace and reports whether it has changes.
func (w *workspace) plan(extra ...string) (changes bool, err error) {
	args := append([]string{"plan", "-input=false", "-detailed-exitcode", "-out=" + planFile}, extra...)
	code, err := w.tf.Run(w.dir, args...)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return false, nil
	case code == 2:
		return true, nil
	default:
		return false, errors.New("tofu plan failed")
	}
}

func (w *workspace) applySaved() error {
	code, err := w.tf.Run(w.dir, "apply", "-input=false", planFile)
	if err != nil {
		return err
	}
	if code != 0 {
		return errors.New("tofu apply failed")
	}
	return nil
}

func (w *workspace) manifest() ([]byte, error) {
	return os.ReadFile(filepath.Join(w.dir, filepath.FromSlash(generate.ManifestFile)))
}

func planCommand(args []string, stdout, stderr io.Writer) int {
	fs, dir, env := envFlags("plan", stderr)
	out := fs.String("out", "", "also save the plan to this file, for plinth apply -plan")
	if !parseEnv(fs, args, env, stderr) {
		return 2
	}
	w, err := prepare(*dir, *env, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := w.plan(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *out == "" {
		return 0
	}
	// The manifest pins the effective configuration this plan was made from.
	manifest, err := w.manifest()
	if err == nil {
		err = copyFile(filepath.Join(w.dir, planFile), *out)
	}
	if err == nil {
		err = os.WriteFile(*out+manifestSuffix, manifest, 0o644)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "\nSaved plan to %s. Apply it with: plinth apply -env %s -plan %s\n", *out, *env, *out)
	return 0
}

func applyCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir, env := envFlags("apply", stderr)
	saved := fs.String("plan", "", "apply a plan saved by plinth plan -out")
	auto := fs.Bool("auto-approve", false, "skip the confirmation prompt")
	if !parseEnv(fs, args, env, stderr) {
		return 2
	}
	w, err := prepare(*dir, *env, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if *saved != "" {
		// ADR 0004: apply exactly what was reviewed, or nothing.
		then, err := os.ReadFile(*saved + manifestSuffix)
		if err != nil {
			fmt.Fprintf(stderr, "%s was not saved by plinth plan -out: %v\n", *saved, err)
			return 1
		}
		now, err := w.manifest()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !bytes.Equal(then, now) {
			fmt.Fprintf(stderr, "the blueprint or environment changed since %s was made; run plinth plan again\n", *saved)
			return 1
		}
		if err := copyFile(*saved, filepath.Join(w.dir, planFile)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return exitOn(w.applySaved(), stderr)
	}

	changes, err := w.plan()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !changes {
		fmt.Fprintf(stdout, "\n%s is up to date.\n", *env)
		return 0
	}
	if !*auto && !confirm(stdin, stdout, fmt.Sprintf("\nApply these changes to %s? Only 'yes' will be accepted: ", *env), "yes") {
		fmt.Fprintln(stderr, "Apply cancelled.")
		return 1
	}
	return exitOn(w.applySaved(), stderr)
}

func envFlags(cmd string, stderr io.Writer) (fs *flag.FlagSet, dir, env *string) {
	fs = flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("blueprint", ".", "blueprint directory"), fs.String("env", "", "environment name")
}

func parseEnv(fs *flag.FlagSet, args []string, env *string, stderr io.Writer) bool {
	if err := fs.Parse(args); err != nil {
		return false
	}
	if *env == "" || fs.NArg() > 0 {
		fmt.Fprint(stderr, usage)
		return false
	}
	return true
}

// confirm reads one line and reports whether it is exactly want.
func confirm(stdin io.Reader, stdout io.Writer, prompt, want string) bool {
	fmt.Fprint(stdout, prompt)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == want
}

func exitOn(err error, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600) // plans can contain secrets
}

func destroyCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir, env := envFlags("destroy", stderr)
	auto := fs.Bool("auto-approve", false, "skip the confirmation prompt")
	if !parseEnv(fs, args, env, stderr) {
		return 2
	}
	w, err := prepare(*dir, *env, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	changes, err := w.plan("-destroy")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !changes {
		fmt.Fprintf(stdout, "\nNothing to destroy in %s.\n", *env)
		return 0
	}
	// Typing the environment's name is deliberate in a way a habitual "yes" is not.
	if !*auto && !confirm(stdin, stdout, fmt.Sprintf("\nDestroy everything above in %s? Type the environment name to confirm: ", *env), *env) {
		fmt.Fprintln(stderr, "Destroy cancelled.")
		return 1
	}
	return exitOn(w.applySaved(), stderr)
}

// driftCommand compares real infrastructure with the environment's state without
// changing either: exit 0 in sync, 2 drifted, 1 on error.
func driftCommand(args []string, stdout, stderr io.Writer) int {
	fs, dir, env := envFlags("drift", stderr)
	if !parseEnv(fs, args, env, stderr) {
		return 2
	}
	w, err := prepare(*dir, *env, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	out, code, err := w.tf.Output(w.dir, "plan", "-refresh-only", "-input=false", "-detailed-exitcode", "-no-color", "-out=drift.tfplan")
	switch {
	case err != nil:
		fmt.Fprintln(stderr, err)
		return 1
	case code == 0:
		fmt.Fprintf(stdout, "%s is in sync with its state.\n", *env)
		return 0
	case code != 2:
		fmt.Fprintf(stderr, "%stofu plan -refresh-only failed\n", out)
		return 1
	}
	planJSON, code, err := w.tf.Output(w.dir, "show", "-json", "drift.tfplan")
	if err != nil || code != 0 {
		fmt.Fprintf(stderr, "%stofu show failed\n", errText(err))
		return 1
	}
	report, err := driftReport(planJSON)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(report) == 0 {
		fmt.Fprintf(stdout, "%s is in sync with its state.\n", *env)
		return 0
	}
	for _, entry := range report {
		fmt.Fprintln(stdout, entry)
	}
	fmt.Fprintf(stdout, "%s has drifted: real infrastructure differs from its state.\n", *env)
	return 2
}
