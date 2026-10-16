package main

import (
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shop")
	plinth("", "init", dir).expect(t, 0, "created "+filepath.Join(dir, "blueprint.yaml"))
	plinth("", "validate", "-blueprint", dir, "-env", "dev").expect(t, 0, "dev: 0 resource instances valid")
	plinth("", "init", dir).expect(t, 1, "refusing to overwrite existing files")
}
