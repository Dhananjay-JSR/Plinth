package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyBlueprint copies the fixture blueprint into a temp dir, pointing its
// catalog at the fixture catalog by absolute path.
func copyBlueprint(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", fixture+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	abs, _ := filepath.Abs("../../testdata/catalogs/core")
	write(t, filepath.Join(dir, ".plinth/project.yaml"), "catalogs:\n  core: "+abs+"\n")
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type result struct {
	code           int
	stdout, stderr string
}

func plinth(stdin string, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return result{code, stdout.String(), stderr.String()}
}

func (r result) expect(t *testing.T, code int, contains string) {
	t.Helper()
	if r.code != code || !strings.Contains(r.stdout+r.stderr, contains) {
		t.Fatalf("exit %d (want %d), output missing %q:\n--- stdout\n%s\n--- stderr\n%s", r.code, code, contains, r.stdout, r.stderr)
	}
}

func TestPlanAndApply(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	bp := copyBlueprint(t)
	prodYAML := filepath.Join(bp, ".plinth/environments/prod.yaml")
	setReplicas := func(n string) {
		write(t, prodYAML, "overrides:\n  application/checkout-api:\n    spec:\n      replicas: "+n+"\n")
	}
	env := []string{"-blueprint", bp, "-env", "prod"}

	plinth("no\n", append([]string{"apply"}, env...)...).expect(t, 1, "Apply cancelled.")
	plinth("yes\n", append([]string{"apply"}, env...)...).expect(t, 0, "Apply complete! Resources: 2 added")

	if _, err := os.Stat(filepath.Join(bp, ".plinth/state/prod.tfstate")); err != nil {
		t.Errorf("default local state not written: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(bp, ".plinth/.gitignore")); string(got) != plinthGitignore {
		t.Errorf(".plinth/.gitignore = %q", got)
	}
	plinth("", append([]string{"apply"}, env...)...).expect(t, 0, "prod is up to date.")

	// Review-then-apply: a saved plan applies only if nothing changed since.
	saved := filepath.Join(t.TempDir(), "prod.plan")
	setReplicas("7")
	plinth("", append([]string{"plan", "-out", saved}, env...)...).expect(t, 0, "Saved plan to "+saved)
	if info, err := os.Stat(saved); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("saved plan: %v, mode %v", err, info.Mode().Perm())
	}
	setReplicas("8")
	plinth("", append([]string{"apply", "-plan", saved}, env...)...).expect(t, 1, "changed since")
	setReplicas("7")
	plinth("", append([]string{"apply", "-plan", saved}, env...)...).expect(t, 0, "Resources: 0 added, 1 changed, 0 destroyed")
	plinth("", append([]string{"apply"}, env...)...).expect(t, 0, "prod is up to date.")

	plinth("", append([]string{"apply", "-plan", filepath.Join(t.TempDir(), "nope")}, env...)...).expect(t, 1, "was not saved by plinth plan -out")
	plinth("", "plan", "-blueprint", bp).expect(t, 2, "usage:")
}
