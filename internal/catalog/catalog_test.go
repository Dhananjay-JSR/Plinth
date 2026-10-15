package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addModule(t *testing.T, catalogDir, kind, flavor, version string, extra map[string]string) string {
	t.Helper()
	dir := filepath.Join(catalogDir, "modules", kind, flavor, version)
	files := map[string]string{"module.yaml": "kind: " + kind + "\nflavor: " + flavor + "\nversion: \"" + version + "\"\n"}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestResolve(t *testing.T) {
	core, extra := t.TempDir(), t.TempDir()
	addModule(t, core, "network", "fake", "1.0", map[string]string{"main.tf": "# net"})
	addModule(t, extra, "cache", "fake", "1.0", nil)
	cats := []Catalog{{"core", core}, {"extra", extra}}

	r, err := Resolve(cats, "network", "fake", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if r.Catalog != "core" || !strings.HasPrefix(r.Digest, "sha256:") {
		t.Errorf("got %+v", r)
	}
	if r, err := Resolve(cats, "cache", "fake", "1.0"); err != nil || r.Catalog != "extra" {
		t.Errorf("cache: %+v, %v", r, err)
	}

	for name, tc := range map[string]struct{ kind, flavor, version, want string }{
		"missing":        {"network", "fake", "2.0", "not found in catalogs [core, extra]"},
		"path traversal": {"network", "..", "1.0", "invalid module identity"},
		"traversal in v": {"network", "fake", "../1.0", "invalid module identity"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Resolve(cats, tc.kind, tc.flavor, tc.version)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}

	addModule(t, extra, "network", "fake", "1.0", nil)
	if _, err := Resolve(cats, "network", "fake", "1.0"); err == nil || !strings.Contains(err.Error(), "more than one catalog") {
		t.Errorf("ambiguous: %v", err)
	}
}

func TestResolveRejectsMismatchedContract(t *testing.T) {
	core := t.TempDir()
	dir := addModule(t, core, "network", "fake", "1.0", nil)
	os.WriteFile(filepath.Join(dir, "module.yaml"), []byte("kind: network\nflavor: other\nversion: \"1.0\"\n"), 0o644)
	if _, err := Resolve([]Catalog{{"core", core}}, "network", "fake", "1.0"); err == nil || !strings.Contains(err.Error(), "declares network/other/1.0") {
		t.Errorf("err = %v", err)
	}
}

func TestDigest(t *testing.T) {
	a := addModule(t, t.TempDir(), "network", "fake", "1.0", map[string]string{"main.tf": "x", "sub/b.tf": "y"})
	b := addModule(t, t.TempDir(), "network", "fake", "1.0", map[string]string{"sub/b.tf": "y", "main.tf": "x"})
	da, _ := Digest(a)
	db, _ := Digest(b)
	if da != db {
		t.Errorf("same content, different digests: %s vs %s", da, db)
	}
	os.WriteFile(filepath.Join(b, "main.tf"), []byte("z"), 0o644)
	if dz, _ := Digest(b); dz == da {
		t.Error("content change did not change digest")
	}
	// Moving content between files must change the digest too.
	c := addModule(t, t.TempDir(), "network", "fake", "1.0", map[string]string{"main.tf": "xy", "sub/b.tf": ""})
	if dc, _ := Digest(c); dc == da {
		t.Error("file boundary change did not change digest")
	}

	if err := os.Symlink("/etc/hosts", filepath.Join(a, "link.tf")); err != nil {
		t.Fatal(err)
	}
	if _, err := Digest(a); err == nil || !strings.Contains(err.Error(), "only regular files") {
		t.Errorf("symlink: %v", err)
	}
}
