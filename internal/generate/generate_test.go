package generate

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/hashicorp/hcl/v2/hclparse"
)

var update = flag.Bool("update", false, "rewrite golden files")

const (
	fixture = "../../testdata/blueprints/checkout"
	golden  = "../../testdata/golden/checkout-prod"
)

func render(t *testing.T, env string) string {
	t.Helper()
	bp, err := blueprint.Load(fixture)
	if err != nil {
		t.Fatal(err)
	}
	e, err := blueprint.LoadEnvironment(fixture, env)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := blueprint.Resolve(bp, e)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle")
	if err := Render(cfg, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRenderMatchesGolden(t *testing.T) {
	got := readTree(t, render(t, "prod"))
	if *update {
		os.RemoveAll(golden)
		for rel, body := range got {
			if err := writeFile(filepath.Join(golden, rel), []byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	want := readTree(t, golden)
	for rel := range want {
		if _, ok := got[rel]; !ok {
			t.Errorf("missing %s", rel)
		}
	}
	for rel, body := range got {
		if want[rel] != body {
			t.Errorf("%s differs from golden (run: go test ./internal/generate -update)\n--- got ---\n%s", rel, body)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, b := readTree(t, render(t, "prod")), readTree(t, render(t, "prod"))
	if len(a) != len(b) {
		t.Fatal("file sets differ")
	}
	for rel := range a {
		if a[rel] != b[rel] {
			t.Errorf("%s differs between renders", rel)
		}
	}
	dev := readTree(t, render(t, "dev"))
	if dev[ManifestFile] == a[ManifestFile] {
		t.Error("different environments produced the same manifest")
	}
}

func TestRenderedHCLParses(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(render(t, "prod"), "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, diags := hclparse.NewParser().ParseHCL(src, "main.tf"); diags.HasErrors() {
		t.Fatal(diags)
	}
	for _, want := range []string{
		`module "application__checkout-api" {`,
		`"kubernetes_details" = module.kubernetes_cluster__default.default`,
		`"cache"              = null`,
		`"CLUSTER"   = "${module.kubernetes_cluster__default.default.attributes.cluster_name}"`,
		`"replicas" = 5`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.tf missing %q\n%s", want, src)
		}
	}
}

func TestRenderRefusesNonEmptyDir(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "keep.txt"), []byte("x"), 0o644)
	if err := Render(&blueprint.Config{}, out); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("err = %v", err)
	}
}

func TestQuoteEscapesLiterals(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:          `"plain"`,
		`say "hi"\n`:     `"say \"hi\"\\n"`,
		"line\nbreak":    `"line\nbreak"`,
		`100%{x} and ${`: `"100%%{x} and $${"`,
		`$5 and %d`:      `"$5 and %d"`,
		`http://${network.main.out.default.attributes.host}:80`: `"http://${module.network__main.default.attributes.host}:80"`,
	} {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}
