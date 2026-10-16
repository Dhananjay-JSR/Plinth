// Package blueprint loads a blueprint and an environment from disk and resolves
// them into an effective configuration.
package blueprint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/catalog"
	"github.com/Dhananjay-JSR/Plinth/internal/module"
	"gopkg.in/yaml.v3"
)

const (
	RootFile        = "blueprint.yaml"
	projectFile     = ".plinth/project.yaml"
	environmentsDir = ".plinth/environments"
)

// NamePattern matches resource instance and environment names.
var NamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// InputRef points an input at a producing resource instance; Output is only
// needed when the producer has several outputs of the input's type.
type InputRef struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Output string `json:"output,omitempty"`
}

type Instance struct {
	Kind    string              `json:"kind"`
	Flavor  string              `json:"flavor"`
	Version string              `json:"version"`
	Inputs  map[string]InputRef `json:"inputs,omitempty"`
	Spec    map[string]any      `json:"spec,omitempty"`

	Name string `json:"-"`
}

// Addr is the instance's address, <kind>/<name>.
func (i *Instance) Addr() string { return i.Kind + "/" + i.Name }

type Blueprint struct {
	Dir       string
	Catalogs  []catalog.Catalog    // sorted by name
	Instances map[string]*Instance // by address
}

type Override struct {
	Spec     map[string]any `yaml:"spec"`
	Disabled bool           `yaml:"disabled"`
}

type Environment struct {
	Name      string
	Overrides map[string]Override `yaml:"overrides"` // by instance address
	State     *State              `yaml:"state"`     // nil: no backend declared
}

// State names an OpenTofu backend and its configuration, passed through as-is.
type State struct {
	Backend string         `yaml:"backend" json:"backend"`
	Config  map[string]any `yaml:"config" json:"config,omitempty"`
}

// Load reads the blueprint rooted at dir: its project file and every
// <kind>/instances/<name>.json.
func Load(dir string) (*Blueprint, error) {
	if _, err := os.Stat(filepath.Join(dir, RootFile)); err != nil {
		return nil, fmt.Errorf("%s is not a blueprint: %w", dir, err)
	}
	bp := &Blueprint{Dir: dir, Instances: map[string]*Instance{}}

	var project struct {
		Catalogs map[string]string `yaml:"catalogs"`
	}
	if err := decodeYAML(filepath.Join(dir, projectFile), &project); err != nil {
		return nil, err
	}
	for name, path := range project.Catalogs {
		if !module.IdentPattern.MatchString(name) {
			return nil, fmt.Errorf("%s: catalog name %q must be lower_snake_case", projectFile, name)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		bp.Catalogs = append(bp.Catalogs, catalog.Catalog{Name: name, Dir: path})
	}
	slices.SortFunc(bp.Catalogs, func(a, b catalog.Catalog) int { return strings.Compare(a.Name, b.Name) })

	// Every .json outside hidden folders must be a resource instance at
	// <kind>/instances/<name>.json; a misplaced one is an error, not silently ignored.
	var errs []error
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if parts := strings.Split(filepath.ToSlash(rel), "/"); len(parts) != 3 || parts[1] != "instances" {
			errs = append(errs, fmt.Errorf("%s: resource instances must live at <kind>/instances/<name>.json", rel))
			return nil
		}
		inst, err := loadInstance(path)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		bp.Instances[inst.Addr()] = inst
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, joinSorted(errs)
	}
	return bp, nil
}

func loadInstance(path string) (*Instance, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var inst Instance
	if err := dec.Decode(&inst); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	dirKind := filepath.Base(filepath.Dir(filepath.Dir(path)))
	inst.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	switch {
	case inst.Kind != dirKind:
		return nil, fmt.Errorf("%s: kind %q does not match its directory %q", path, inst.Kind, dirKind)
	case !NamePattern.MatchString(inst.Name):
		return nil, fmt.Errorf("%s: name %q must match %s", path, inst.Name, NamePattern)
	}
	return &inst, nil
}

// LoadEnvironment reads .plinth/environments/<name>.yaml.
func LoadEnvironment(dir, name string) (*Environment, error) {
	if !NamePattern.MatchString(name) {
		return nil, fmt.Errorf("environment name %q must match %s", name, NamePattern)
	}
	env := &Environment{Name: name}
	path := filepath.Join(dir, environmentsDir, name+".yaml")
	if err := decodeYAML(path, env); err != nil {
		return nil, err
	}
	if env.State != nil && !module.IdentPattern.MatchString(env.State.Backend) {
		return nil, fmt.Errorf("%s: state.backend %q must be an OpenTofu backend name such as local or s3", path, env.State.Backend)
	}
	return env, nil
}

func decodeYAML(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// joinSorted joins errors in a stable order, dropping repeats: an instance that
// reaches a bad producer through both an input and a reference reports it once.
func joinSorted(errs []error) error {
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	errs = slices.CompactFunc(errs, func(a, b error) bool { return a.Error() == b.Error() })
	return errors.Join(errs...)
}
