package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRefusesWithoutWritingAnything(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("mine"), 0o644)

	_, err := Write(dir, map[string]string{"a.txt": "new", "b.txt": "new", "sub/c.txt": "new"})
	if err == nil || !strings.Contains(err.Error(), "b.txt") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "mine" {
		t.Error("existing file overwritten")
	}
	for _, f := range []string{"a.txt", "sub/c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s written although the write was refused", f)
		}
	}
}
