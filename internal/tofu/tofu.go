// Package tofu runs the OpenTofu CLI.
package tofu

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// MinVersion is the oldest OpenTofu plinth supports.
const MinVersion = "1.6.0"

type Runner struct {
	Bin            string
	Stdout, Stderr io.Writer
}

// Find locates tofu on PATH and checks its version.
func Find(stdout, stderr io.Writer) (*Runner, error) {
	bin, err := exec.LookPath("tofu")
	if err != nil {
		return nil, errors.New("tofu not found on PATH; install OpenTofu: https://opentofu.org/docs/intro/install/")
	}
	out, err := exec.Command(bin, "version", "-json").Output()
	if err != nil {
		return nil, fmt.Errorf("running %s version: %w", bin, err)
	}
	var v struct {
		Version string `json:"terraform_version"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("reading tofu version: %w", err)
	}
	if !atLeast(v.Version, MinVersion) {
		return nil, fmt.Errorf("OpenTofu %s found, plinth needs %s or newer", v.Version, MinVersion)
	}
	return &Runner{Bin: bin, Stdout: stdout, Stderr: stderr}, nil
}

// Run runs tofu in dir with output streamed through, and returns its exit code.
// An error means tofu could not be started at all.
func (r *Runner) Run(dir string, args ...string) (int, error) {
	return r.run(dir, r.Stdout, args...)
}

// Output runs tofu in dir and returns what it wrote to stdout.
func (r *Runner) Output(dir string, args ...string) ([]byte, int, error) {
	var buf bytes.Buffer
	code, err := r.run(dir, &buf, args...)
	return buf.Bytes(), code, err
}

func (r *Runner) run(dir string, stdout io.Writer, args ...string) (int, error) {
	cmd := exec.Command(r.Bin, append([]string{"-chdir=" + dir}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = r.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// atLeast compares dotted numeric versions; a pre-release suffix is ignored.
func atLeast(have, want string) bool {
	h, w := parts(have), parts(want)
	for i := range w {
		if h[i] != w[i] {
			return h[i] > w[i]
		}
	}
	return true
}

func parts(v string) [3]int {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
