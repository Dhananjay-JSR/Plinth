package main

import (
	"bytes"
	"os"
	"os/exec"
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

func TestModuleValidate(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	const fixtureModule = "../../testdata/catalogs/core/modules/application/fake/1.0"
	broken := t.TempDir()
	os.WriteFile(filepath.Join(broken, "module.yaml"), []byte("kind: app\nflavor: f\nversion: \"1\"\n"), 0o644)
	os.WriteFile(filepath.Join(broken, "main.tf"), []byte("output \"x\" {\n  value = var.not_declared\n}\n"), 0o644)
	badContract := t.TempDir()
	os.WriteFile(filepath.Join(badContract, "module.yaml"), []byte("kind: Bad\n"), 0o644)

	for name, tc := range map[string]struct {
		args                   []string
		code                   int
		wantStdout, wantStderr string
	}{
		"valid module":     {[]string{"module", "validate", fixtureModule}, 0, "application/fake/1.0: module valid", ""},
		"invalid tofu":     {[]string{"module", "validate", broken}, 1, "", "app/f/1: OpenTofu code is invalid"},
		"invalid contract": {[]string{"module", "validate", badContract}, 1, "", "must be lower_snake_case"},
		"missing dir":      {[]string{"module", "validate"}, 2, "", "usage:"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.code || !strings.Contains(stdout.String(), tc.wantStdout) || !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
}
