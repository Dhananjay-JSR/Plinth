package blueprint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "../../testdata/blueprints/checkout"

func resolve(t *testing.T, dir, env string) (*Config, error) {
	t.Helper()
	bp, err := Load(dir)
	if err != nil {
		return nil, err
	}
	e, err := LoadEnvironment(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	return Resolve(bp, e)
}

func find(cfg *Config, addr string) *Resolved {
	for _, r := range cfg.Instances {
		if r.Addr == addr {
			return r
		}
	}
	return nil
}

func TestResolveFixture(t *testing.T) {
	prod, err := resolve(t, fixture, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(prod.Instances) != 2 || prod.Instances[0].Addr != "application/checkout-api" {
		t.Fatalf("instances not sorted by address: %v", prod.Instances)
	}
	app := find(prod, "application/checkout-api")
	spec := app.Spec.(map[string]any)
	env := spec["env"].(map[string]any)
	if spec["replicas"] != json.Number("5") || spec["port"] != json.Number("8080") || env["LOG_LEVEL"] != "warn" {
		t.Errorf("prod spec not merged: %v", spec)
	}
	if env["CLUSTER"] != "${kubernetes_cluster.default.out.default.attributes.cluster_name}" {
		t.Errorf("reference lost in merge: %v", env["CLUSTER"])
	}
	if b := app.Inputs["kubernetes_details"]; b == nil || *b != (Binding{"kubernetes_cluster/default", "default"}) {
		t.Errorf("kubernetes_details binding = %+v", b)
	}
	if b, ok := app.Inputs["cache"]; !ok || b != nil {
		t.Errorf("optional unconnected input should be present and nil, got %+v (present %v)", b, ok)
	}

	dev, err := resolve(t, fixture, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := find(dev, "application/checkout-api").Spec.(map[string]any)["replicas"]; got != json.Number("2") {
		t.Errorf("dev replicas = %v, want blueprint value 2", got)
	}
}

// scenario copies the fixture blueprint and applies file edits (empty content deletes).
func scenario(t *testing.T, edits map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	err := filepath.Walk(fixture, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(fixture, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == projectFile {
			abs, _ := filepath.Abs("../../testdata/catalogs/core")
			data = []byte("catalogs:\n  core: " + abs + "\n")
		}
		return writeFile(filepath.Join(dir, rel), string(data))
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, body := range edits {
		if body == "" {
			os.Remove(filepath.Join(dir, rel))
			continue
		}
		if err := writeFile(filepath.Join(dir, rel), body); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

const (
	appFile     = "application/instances/checkout-api.json"
	clusterFile = "kubernetes_cluster/instances/default.json"
	prodFile    = ".plinth/environments/prod.yaml"
)

func app(inputs, spec string) string {
	return `{"kind":"application","flavor":"fake","version":"1.0","inputs":` + inputs + `,"spec":` + spec + `}`
}

const (
	clusterInput = `{"kubernetes_details":{"kind":"kubernetes_cluster","name":"default"}}`
	okSpec       = `{"image":"a","port":80}`
)

func TestResolveErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		edits map[string]string
		want  string
	}{
		"override adds an instance": {
			map[string]string{prodFile: "overrides:\n  application/ghost:\n    spec: {}\n"},
			"application/ghost: override for unknown resource instance",
		},
		"depends on disabled": {
			map[string]string{prodFile: "overrides:\n  kubernetes_cluster/default:\n    disabled: true\n"},
			"application/checkout-api: depends on kubernetes_cluster/default, which is disabled in environment prod",
		},
		"required input missing": {
			map[string]string{appFile: app(`{}`, okSpec)},
			`required input "kubernetes_details" (core/kubernetes_cluster) is not connected`,
		},
		"undeclared input": {
			map[string]string{appFile: app(`{"kubernetes_details":{"kind":"kubernetes_cluster","name":"default"},"db":{"kind":"kubernetes_cluster","name":"default"}}`, okSpec)},
			`input "db" is not declared by module application/fake/1.0`,
		},
		"wrong output type": {
			map[string]string{
				"application/instances/other.json": app(clusterInput, okSpec),
				appFile:                            app(`{"kubernetes_details":{"kind":"application","name":"other"}}`, okSpec),
			},
			"needs core/kubernetes_cluster, but application/other produces [core/application]",
		},
		"named output of wrong type": {
			map[string]string{appFile: app(`{"kubernetes_details":{"kind":"kubernetes_cluster","name":"default","output":"nope"}}`, okSpec)},
			`kubernetes_cluster/default has no output "nope"`,
		},
		"unknown producer": {
			map[string]string{appFile: app(`{"kubernetes_details":{"kind":"kubernetes_cluster","name":"missing"}}`, okSpec)},
			"depends on unknown resource instance kubernetes_cluster/missing",
		},
		"invalid spec": {
			map[string]string{appFile: app(clusterInput, `{"image":"a","port":0}`)},
			"application/checkout-api: spec/port",
		},
		"bad reference syntax": {
			map[string]string{appFile: app(clusterInput, `{"image":"${kubernetes_cluster.default.attributes}","port":80}`)},
			"invalid reference ${kubernetes_cluster.default.attributes}",
		},
		"reference to unknown output": {
			map[string]string{appFile: app(clusterInput, `{"image":"${kubernetes_cluster.default.out.extra.x}","port":80}`)},
			`kubernetes_cluster/default has no output "extra"`,
		},
		"cycle": {
			map[string]string{clusterFile: `{"kind":"kubernetes_cluster","flavor":"fake","version":"1.0","spec":{"cluster_name":"${application.checkout-api.out.default.attributes.name}"}}`},
			"dependency cycle: application/checkout-api -> kubernetes_cluster/default -> application/checkout-api",
		},
		"unknown module": {
			map[string]string{clusterFile: `{"kind":"kubernetes_cluster","flavor":"fake","version":"9.9","spec":{}}`},
			"module kubernetes_cluster/fake/9.9 not found",
		},
		"kind does not match directory": {
			map[string]string{"network/instances/x.json": `{"kind":"application","flavor":"fake","version":"1.0"}`},
			`kind "application" does not match its directory "network"`,
		},
		"unknown instance field": {
			map[string]string{appFile: `{"kind":"application","flavor":"fake","version":"1.0","intent":"x"}`},
			`unknown field "intent"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolve(t, scenario(t, tc.edits), "prod")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v\nwant containing %q", err, tc.want)
			}
		})
	}
}

func TestResolveReportsAllErrors(t *testing.T) {
	_, err := resolve(t, scenario(t, map[string]string{
		appFile:  app(`{}`, `{"image":"a","port":0}`),
		prodFile: "overrides:\n  application/ghost:\n    disabled: true\n",
	}), "prod")
	if err == nil || strings.Count(err.Error(), "\n") < 2 {
		t.Fatalf("want three errors together, got: %v", err)
	}
}

func TestParseRef(t *testing.T) {
	r, err := ParseRef("kubernetes_cluster.default.out.default.attributes.cluster_name")
	if err != nil || r.Addr() != "kubernetes_cluster/default" || r.Output != "default" || strings.Join(r.Path, ".") != "attributes.cluster_name" {
		t.Errorf("got %+v, %v", r, err)
	}
	if r, err := ParseRef("network.main.out.default"); err != nil || r.Path != nil {
		t.Errorf("no path: %+v, %v", r, err)
	}
	for _, bad := range []string{"blueprint.self.variables.x", "a.b", "A.b.out.c", "a.b.out."} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) should fail", bad)
		}
	}
}

func TestLoadEnvironmentRejectsPathTraversal(t *testing.T) {
	if _, err := LoadEnvironment(fixture, "../../x"); err == nil {
		t.Error("want error")
	}
}
