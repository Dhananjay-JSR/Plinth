package main

import (
	"os/exec"
	"path/filepath"
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
