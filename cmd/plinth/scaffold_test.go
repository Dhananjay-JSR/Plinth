package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shop")
	plinth("", "init", dir).expect(t, 0, "created "+filepath.Join(dir, "blueprint.yaml"))
	plinth("", "validate", "-blueprint", dir, "-env", "dev").expect(t, 0, "dev: 0 resource instances valid")
	plinth("", "init", dir).expect(t, 1, "refusing to overwrite existing files")
}

func TestModuleInit(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "catalog/modules/cache/redis_docker/1.0")
	plinth("", "module", "init", dir).expect(t, 0, "created "+filepath.Join(dir, "module.yaml"))
	plinth("", "module", "validate", dir).expect(t, 0, "cache/redis_docker/1.0: module valid")
	if out, err := exec.Command("tofu", "fmt", "-check", dir).CombinedOutput(); err != nil {
		t.Errorf("generated main.tf is not tofu fmt-clean: %s", out)
	}
	plinth("", "module", "init", dir).expect(t, 1, "refusing to overwrite")

	elsewhere := filepath.Join(root, "scratch")
	plinth("", "module", "init", elsewhere).expect(t, 2, "set -kind, -flavor and -version")
	plinth("", "module", "init", "-kind", "queue", "-flavor", "sqs", "-version", "2.0", elsewhere).expect(t, 0, "created")
	plinth("", "module", "validate", elsewhere).expect(t, 0, "queue/sqs/2.0: module valid")
	plinth("", "module", "init", "-kind", "Bad", "-flavor", "x", "-version", "1", filepath.Join(root, "bad")).expect(t, 1, "lower_snake_case")
}

func TestAdd(t *testing.T) {
	bp := copyBlueprint(t)
	r := plinth("", "add", "-blueprint", bp, "-module", "fake/1.0", "application/admin")
	r.expect(t, 0, "inputs.kubernetes_details connected to kubernetes_cluster/default")
	r.expect(t, 0, "inputs.cache is optional (core/redis); left unconnected")
	r.expect(t, 0, "fill in: spec.image (string)")
	r.expect(t, 0, "fill in: spec.port (integer)")

	got, err := os.ReadFile(filepath.Join(bp, "application/instances/admin.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"flavor": "fake"`, `"kind": "kubernetes_cluster"`, `"name": "default"`, `"image": ""`, `"port": 0`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("admin.json missing %s:\n%s", want, got)
		}
	}
	// Unfilled, it points at exactly what is left to do.
	plinth("", "validate", "-blueprint", bp, "-env", "dev").expect(t, 1, "application/admin: spec/port")
	write(t, filepath.Join(bp, "application/instances/admin.json"), strings.Replace(strings.Replace(string(got), `"image": ""`, `"image": "admin:1.0"`, 1), `"port": 0`, `"port": 3000`, 1))
	plinth("", "validate", "-blueprint", bp, "-env", "dev").expect(t, 0, "dev: 3 resource instances valid")

	plinth("", "add", "-blueprint", bp, "-module", "fake/1.0", "application/admin").expect(t, 1, "refusing to overwrite")

	// Two clusters: Plinth can't guess, so it says which to choose from.
	write(t, filepath.Join(bp, "kubernetes_cluster/instances/backup.json"), `{"kind":"kubernetes_cluster","flavor":"fake","version":"1.0","spec":{"cluster_name":"b"}}`)
	plinth("", "add", "-blueprint", bp, "-module", "fake/1.0", "application/other").expect(t, 0, "choose one of [kubernetes_cluster/backup, kubernetes_cluster/default]")

	plinth("", "add", "-blueprint", bp, "-module", "fake/9.9", "application/x").expect(t, 1, "module application/fake/9.9 not found")
	plinth("", "add", "-blueprint", bp, "-module", "fake/1.0", "Application/x").expect(t, 2, "is not a resource instance address")
	plinth("", "add", "-blueprint", bp, "application/x").expect(t, 2, "usage:")
}

func TestInitExamples(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	for _, name := range []string{"sim", "docker"} {
		dir := filepath.Join(t.TempDir(), name)
		plinth("", "init", "-example", name, dir).expect(t, 0, "plinth apply -env dev")
		for _, env := range []string{"dev", "prod"} {
			plinth("", "validate", "-blueprint", dir, "-env", env).expect(t, 0, env+": 2 resource instances valid")
		}
		if out, err := exec.Command("tofu", "fmt", "-check", "-recursive", dir).CombinedOutput(); err != nil {
			t.Errorf("%s example is not tofu fmt-clean: %s", name, out)
		}
	}

	// sim needs nothing but OpenTofu, so it runs end to end here.
	dir := filepath.Join(t.TempDir(), "sim")
	plinth("", "init", "-example", "sim", dir).expect(t, 0, "created")
	env := []string{"-blueprint", dir, "-env", "prod"}
	plinth("", append([]string{"apply", "-auto-approve"}, env...)...).expect(t, 0, "Resources: 2 added")
	plinth("", append([]string{"drift"}, env...)...).expect(t, 0, "prod is in sync with its state.")
	plinth("", append([]string{"destroy", "-auto-approve"}, env...)...).expect(t, 0, "2 destroyed")

	plinth("", "init", "-example", "nope", t.TempDir()).expect(t, 2, "unknown example \"nope\"; choose one of: docker, sim")
}
