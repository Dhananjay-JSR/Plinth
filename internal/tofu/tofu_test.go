package tofu

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestAtLeast(t *testing.T) {
	for _, tc := range []struct {
		have string
		want bool
	}{
		{"1.6.0", true}, {"1.13.1", true}, {"2.0.0", true}, {"v1.6.2", true},
		{"1.5.9", false}, {"1.6.0-beta1", true}, {"0.9.0", false}, {"1.10", true},
	} {
		if got := atLeast(tc.have, "1.6.0"); got != tc.want {
			t.Errorf("atLeast(%q, 1.6.0) = %v, want %v", tc.have, got, tc.want)
		}
	}
}

func TestFindAndRun(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	var stdout, stderr bytes.Buffer
	r, err := Find(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	out, code, err := r.Output(t.TempDir(), "version")
	if err != nil || code != 0 || !strings.Contains(string(out), "OpenTofu") {
		t.Fatalf("version: code %d, err %v, out %q", code, err, out)
	}
	if code, err := r.Run(t.TempDir(), "no-such-command"); err != nil || code == 0 {
		t.Errorf("bad command: code %d, err %v; want non-zero exit and no start error", code, err)
	}
}

func TestFindWithoutTofu(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(nil, nil); err == nil || !strings.Contains(err.Error(), "tofu not found on PATH") {
		t.Errorf("err = %v", err)
	}
}
