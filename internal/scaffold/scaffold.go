// Package scaffold writes starter blueprints, modules and resource instances.
package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/blueprint"
	"github.com/Dhananjay-JSR/Plinth/internal/module"
)

// Gitignore keeps generated workspaces and local state, which can hold
// secrets, out of git.
const Gitignore = "build/\nstate/\n"

// Write creates every file under dir, or none: if any already exists it
// refuses and lists them, so hand-written work is never overwritten.
func Write(dir string, files map[string]string) ([]string, error) {
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	slices.Sort(paths)

	var existing []string
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			existing = append(existing, filepath.Join(dir, rel))
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("refusing to overwrite existing files:\n  %s", strings.Join(existing, "\n  "))
	}

	for _, rel := range paths {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(files[rel]), 0o644); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// Blueprint is an empty blueprint with one environment.
func Blueprint() map[string]string {
	return map[string]string{
		"blueprint.yaml": "# Marks the root of a Plinth blueprint.\n",
		".plinth/project.yaml": `# Catalogs this project draws modules from: a local name → a folder holding
# modules/<kind>/<flavor>/<version>/. The name is only a nickname.
catalogs:
  core: ../catalog
`,
		".plinth/environments/dev.yaml": `# Sparse changes for this environment, by resource instance address:
# overrides:
#   application/checkout-api:
#     spec: { replicas: 2 }
#   network/nat:
#     disabled: true
overrides: {}
`,
		".plinth/.gitignore": Gitignore,
	}
}

// Module is a module that validates and applies as generated: a terraform_data
// placeholder for the author to replace with real resources.
func Module(kind, flavor, version string) map[string]string {
	return map[string]string{
		"module.yaml": fmt.Sprintf(`kind: %s
flavor: %s
version: "%s"

# JSON Schema for what a resource instance may set in its "spec".
spec:
  type: object
  required: [message]
  additionalProperties: false
  properties:
    message: { type: string }

# Other resource instances this module needs, matched by output type:
# inputs:
#   network: { type: core/network }

# What this module provides. Other modules' inputs link to it by this type.
outputs:
  default: { type: core/%s }
`, kind, flavor, version, kind),
		"main.tf": `# Replace terraform_data with the real resources this module manages.
# Plinth passes every module four variables (no variables.tf needed):
#   var.instance       kind, flavor, version and the effective spec
#   var.instance_name  this resource instance's name
#   var.environment    the environment, e.g. { name = "dev" }
#   var.inputs         connected inputs' outputs, by input name
resource "terraform_data" "this" {
  input = {
    message     = var.instance.spec.message
    environment = var.environment.name
  }
}

output "default" {
  value = {
    attributes = terraform_data.this.output # facts other modules may read
    interfaces = {}                         # how to connect, e.g. { http = { host, port } }
  }
}
`,
	}
}

// Instance is a resource instance pre-filled from a module's contract: each
// required spec field gets its schema default, first enum value, or an empty
// value of its type. It returns the fields still to fill in.
func Instance(m *module.Module, inputs map[string]blueprint.InputRef) (string, []string) {
	spec := map[string]any{}
	var todo []string
	props, _ := m.Spec["properties"].(map[string]any)
	required, _ := m.Spec["required"].([]any)
	for _, r := range required {
		name, _ := r.(string)
		prop, _ := props[name].(map[string]any)
		typ, _ := prop["type"].(string)
		switch enum, _ := prop["enum"].([]any); {
		case prop["default"] != nil:
			spec[name] = prop["default"]
		case len(enum) > 0:
			spec[name] = enum[0]
		default:
			spec[name] = zero(typ)
			todo = append(todo, fmt.Sprintf("spec.%s (%s)", name, typ))
		}
	}
	inst := struct {
		Kind    string                        `json:"kind"`
		Flavor  string                        `json:"flavor"`
		Version string                        `json:"version"`
		Inputs  map[string]blueprint.InputRef `json:"inputs,omitempty"`
		Spec    map[string]any                `json:"spec"`
	}{m.Kind, m.Flavor, m.Version, inputs, spec}
	b, _ := json.MarshalIndent(inst, "", "  ") // only plain values: cannot fail
	return string(b) + "\n", todo
}

func zero(typ string) any {
	switch typ {
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "object":
		return map[string]any{}
	case "array":
		return []any{}
	}
	return ""
}
