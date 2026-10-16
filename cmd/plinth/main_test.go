package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "../../testdata/blueprints/checkout"

func TestRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	for name, tc := range map[string]struct {
		args       []string
		code       int
		wantStdout string
		wantStderr string
	}{
		"no command":         {nil, 2, "", "usage:"},
		"version":            {[]string{"--version"}, 0, "plinth dev", ""},
		"unknown command":    {[]string{"apply"}, 2, "", "usage:"},
		"missing env":        {[]string{"validate", "-blueprint", fixture}, 2, "", "usage:"},
		"render without out": {[]string{"render", "-blueprint", fixture, "-env", "prod"}, 2, "", "usage:"},
		"validate":           {[]string{"validate", "-blueprint", fixture, "-env", "prod"}, 0, "prod: 2 resource instances valid", ""},
		"unknown env":        {[]string{"validate", "-blueprint", fixture, "-env", "staging"}, 1, "", "staging.yaml"},
		"not a blueprint":    {[]string{"validate", "-blueprint", t.TempDir(), "-env", "prod"}, 1, "", "is not a blueprint"},
		"render":             {[]string{"render", "-blueprint", fixture, "-env", "prod", "-out", out}, 0, "rendered 2 resource instances", ""},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.code || !strings.Contains(stdout.String(), tc.wantStdout) || !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(out, "main.tf")); err != nil {
		t.Errorf("render wrote no main.tf: %v", err)
	}
}
