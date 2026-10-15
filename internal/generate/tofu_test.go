package generate

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tofu runs OpenTofu in dir and returns stdout and the exit code.
func tofu(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("tofu", append([]string{"-chdir=" + dir}, args...)...)
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=1", "TF_INPUT=0")
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		t.Fatalf("tofu %v: %v", args, err)
	}
	return string(out), 0
}

func mustTofu(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, code := tofu(t, dir, args...)
	if code != 0 {
		t.Fatalf("tofu %v exited %d:\n%s", args, code, out)
	}
	return out
}

// TestBundleAppliesWithOpenTofu is the Phase 0 end-to-end proof: a rendered
// bundle applies, wires outputs into inputs, converges, and an environment
// change becomes an in-place update.
func TestBundleAppliesWithOpenTofu(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH (run via: mise exec -- go test ./...)")
	}
	prod := render(t, "prod")
	mustTofu(t, prod, "init")
	mustTofu(t, prod, "apply", "-auto-approve")

	if out, code := tofu(t, prod, "plan", "-detailed-exitcode"); code != 0 {
		t.Fatalf("second plan is not clean (exit %d):\n%s", code, out)
	}

	var state struct {
		Values struct {
			RootModule struct {
				ChildModules []struct {
					Address   string `json:"address"`
					Resources []struct {
						Values struct {
							Output map[string]any `json:"output"`
						} `json:"values"`
					} `json:"resources"`
				} `json:"child_modules"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(mustTofu(t, prod, "show", "-json")), &state); err != nil {
		t.Fatal(err)
	}
	var deployment map[string]any
	for _, m := range state.Values.RootModule.ChildModules {
		if m.Address == "module.application__checkout-api" && len(m.Resources) == 1 {
			deployment = m.Resources[0].Values.Output
		}
	}
	env, _ := deployment["env"].(map[string]any)
	for key, want := range map[string]any{
		"cluster":  "https://checkout.prod.fake", // input wired from the cluster's output
		"replicas": float64(5),                   // prod override applied
		"cache":    "none",                       // optional input left unconnected
	} {
		if deployment[key] != want {
			t.Errorf("deployment %s = %v, want %v", key, deployment[key], want)
		}
	}
	if env["CLUSTER"] != "checkout" || env["LOG_LEVEL"] != "warn" {
		t.Errorf("deployment env = %v (reference or override not applied)", env)
	}

	// Same state, dev's effective configuration: everything changes in place.
	dev := render(t, "dev")
	data, err := os.ReadFile(filepath.Join(prod, "terraform.tfstate"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "terraform.tfstate"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	mustTofu(t, dev, "init")
	if out, code := tofu(t, dev, "plan", "-detailed-exitcode", "-out=plan.bin"); code != 2 {
		t.Fatalf("plan after environment change exited %d, want 2 (changes):\n%s", code, out)
	}
	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(mustTofu(t, dev, "show", "-json", "plan.bin")), &plan); err != nil {
		t.Fatal(err)
	}
	updates := 0
	for _, rc := range plan.ResourceChanges {
		switch a := rc.Change.Actions; {
		case len(a) == 1 && a[0] == "update":
			updates++
		case len(a) == 1 && a[0] == "no-op":
		default:
			t.Errorf("%s: actions %v, want in-place update only", rc.Address, a)
		}
	}
	if updates == 0 {
		t.Error("environment change produced no updates")
	}
}
