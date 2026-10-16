// Package module loads a module contract (module.yaml) and validates specs against it.
package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"gopkg.in/yaml.v3"
)

// ContractFile is the module contract's file name inside a module directory.
const ContractFile = "module.yaml"

var (
	// No "__" in a kind: generated module calls are named <kind>__<name>.
	KindPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	IdentPattern      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	VersionPattern    = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*$`)
	OutputTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*/[a-z][a-z0-9_]*$`)
)

type Input struct {
	Type     string `yaml:"type"`
	Optional bool   `yaml:"optional"`
}

type Output struct {
	Type string `yaml:"type"`
}

type Module struct {
	Kind    string            `yaml:"kind"`
	Flavor  string            `yaml:"flavor"`
	Version string            `yaml:"version"`
	Spec    map[string]any    `yaml:"spec"` // JSON Schema for the spec
	Inputs  map[string]Input  `yaml:"inputs"`
	Outputs map[string]Output `yaml:"outputs"`

	Dir    string `yaml:"-"`
	schema *jsonschema.Schema
}

// ID is the module's kind/flavor/version identity.
func (m *Module) ID() string { return m.Kind + "/" + m.Flavor + "/" + m.Version }

// Load reads and checks dir/module.yaml.
func Load(dir string) (*Module, error) {
	path := filepath.Join(dir, ContractFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Module
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Dir = dir
	if err := m.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := checkReservedVariables(dir); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return &m, nil
}

func (m *Module) check() error {
	var errs []error
	if !KindPattern.MatchString(m.Kind) {
		errs = append(errs, fmt.Errorf("kind %q must be lower_snake_case without double underscores", m.Kind))
	}
	if !IdentPattern.MatchString(m.Flavor) {
		errs = append(errs, fmt.Errorf("flavor %q must be lower_snake_case", m.Flavor))
	}
	if !VersionPattern.MatchString(m.Version) {
		errs = append(errs, fmt.Errorf("version %q is invalid", m.Version))
	}
	for name, in := range m.Inputs {
		if !IdentPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("input %q must be lower_snake_case", name))
		}
		if !OutputTypePattern.MatchString(in.Type) {
			errs = append(errs, fmt.Errorf("input %q: type %q must be <catalog>/<name>", name, in.Type))
		}
	}
	for name, out := range m.Outputs {
		if !IdentPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("output %q must be lower_snake_case", name))
		}
		if !OutputTypePattern.MatchString(out.Type) {
			errs = append(errs, fmt.Errorf("output %q: type %q must be <catalog>/<name>", name, out.Type))
		}
	}
	if len(errs) > 0 {
		return joinSorted(errs)
	}

	schemaDoc := any(m.Spec)
	if m.Spec == nil {
		schemaDoc = map[string]any{"type": "object"}
	}
	doc, err := Normalize(schemaDoc)
	if err != nil {
		return fmt.Errorf("spec schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("spec.json", doc); err != nil {
		return fmt.Errorf("spec schema: %w", err)
	}
	if m.schema, err = c.Compile("spec.json"); err != nil {
		return fmt.Errorf("spec schema: %w", err)
	}
	return nil
}

// Normalize round-trips v through JSON so values from YAML and JSON compare alike
// (map[string]any, []any, json.Number).
func Normalize(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

var printer = message.NewPrinter(language.English)

// ValidateSpec checks spec against the module's schema. Fields whose value is a
// string containing a reference ("${...}") are only known after apply, so schema
// errors located at such a field are ignored.
func (m *Module) ValidateSpec(spec any) error {
	doc, err := Normalize(spec)
	if err != nil {
		return err
	}
	err = m.schema.Validate(doc)
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var errs []error
	for _, leaf := range leaves(ve) {
		if holdsReference(doc, leaf.InstanceLocation) {
			continue
		}
		errs = append(errs, fmt.Errorf("spec/%s: %s", strings.Join(leaf.InstanceLocation, "/"), leaf.ErrorKind.LocalizedString(printer)))
	}
	if len(errs) == 0 {
		return nil
	}
	return joinSorted(errs)
}

func leaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}

func holdsReference(doc any, loc []string) bool {
	v := doc
	for _, key := range loc {
		switch t := v.(type) {
		case map[string]any:
			v = t[key]
		case []any:
			var i int
			if _, err := fmt.Sscan(key, &i); err != nil || i < 0 || i >= len(t) {
				return false
			}
			v = t[i]
		default:
			return false
		}
	}
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
}

// joinSorted joins errors in a stable order so output is deterministic.
func joinSorted(errs []error) error {
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errors.Join(errs...)
}
