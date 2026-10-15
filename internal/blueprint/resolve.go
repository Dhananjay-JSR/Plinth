package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/catalog"
	"github.com/Dhananjay-JSR/Plinth/internal/merge"
	"github.com/Dhananjay-JSR/Plinth/internal/module"
)

// Binding is the producer output connected to one input.
type Binding struct {
	Producer string // address
	Output   string
}

// Resolved is one enabled resource instance in an effective configuration.
type Resolved struct {
	Addr   string
	Name   string
	Module *catalog.Resolved
	Spec   any                 // effective spec, normalized
	Inputs map[string]*Binding // every declared input; nil = optional and unconnected
}

// Config is an effective configuration: the blueprint with one environment applied.
type Config struct {
	Environment string
	Instances   []*Resolved // sorted by address
}

// Ref is a reference ${<kind>.<name>.out.<output>[.<path>...]} inside a spec string.
type Ref struct {
	Kind, Name, Output string
	Path               []string
}

func (r Ref) Addr() string { return r.Kind + "/" + r.Name }

var (
	// RefPattern finds every ${...} in a string.
	RefPattern = regexp.MustCompile(`\$\{([^}]*)\}`)
	refBody    = regexp.MustCompile(`^([a-z][a-z0-9_]*)\.([a-z0-9][a-z0-9_-]*)\.out\.([a-z][a-z0-9_]*)((?:\.[A-Za-z0-9_-]+)*)$`)
)

// ParseRef parses the inside of a ${...}.
func ParseRef(body string) (Ref, error) {
	m := refBody.FindStringSubmatch(body)
	if m == nil {
		return Ref{}, fmt.Errorf("invalid reference ${%s}: want ${<kind>.<name>.out.<output>[.<path>]}", body)
	}
	r := Ref{Kind: m[1], Name: m[2], Output: m[3]}
	if m[4] != "" {
		r.Path = strings.Split(m[4][1:], ".")
	}
	return r, nil
}

// Resolve applies env to bp, resolves every enabled instance's module, validates
// specs, connects inputs and references, and rejects dependency cycles. All
// problems found are returned together.
func Resolve(bp *Blueprint, env *Environment) (*Config, error) {
	var errs []error
	fail := func(addr, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{addr}, args...)...))
	}

	for addr := range env.Overrides {
		if bp.Instances[addr] == nil {
			fail(addr, "override for unknown resource instance (environments cannot add resource instances)")
		}
	}

	// Pass 1: modules and effective specs of enabled instances.
	resolved := map[string]*Resolved{}
	for addr, inst := range bp.Instances {
		ov := env.Overrides[addr]
		if ov.Disabled {
			continue
		}
		mod, err := catalog.Resolve(bp.Catalogs, inst.Kind, inst.Flavor, inst.Version)
		if err != nil {
			fail(addr, "%v", err)
			continue
		}
		spec, err := module.Normalize(merge.Merge(orEmpty(inst.Spec), orEmpty(ov.Spec)))
		if err != nil {
			fail(addr, "%v", err)
			continue
		}
		if err := mod.Module.ValidateSpec(spec); err != nil {
			fail(addr, "%v", err)
		}
		resolved[addr] = &Resolved{Addr: addr, Name: inst.Name, Module: mod, Spec: spec, Inputs: map[string]*Binding{}}
	}

	// producer reports why addr can't be depended on, or returns its resolution.
	producer := func(from, addr string) *Resolved {
		switch {
		case resolved[addr] != nil:
			return resolved[addr]
		case bp.Instances[addr] == nil:
			fail(from, "depends on unknown resource instance %s", addr)
		case env.Overrides[addr].Disabled:
			fail(from, "depends on %s, which is disabled in environment %s", addr, env.Name)
		}
		return nil // unresolvable producer: its own error is already reported
	}

	// Pass 2: inputs and references, collected as dependency edges.
	edges := map[string][]string{}
	for addr, r := range resolved {
		inst := bp.Instances[addr]
		for name := range inst.Inputs {
			if _, ok := r.Module.Module.Inputs[name]; !ok {
				fail(addr, "input %q is not declared by module %s", name, r.Module.Module.ID())
			}
		}
		for name, decl := range r.Module.Module.Inputs {
			ref, connected := inst.Inputs[name]
			if !connected {
				if !decl.Optional {
					fail(addr, "required input %q (%s) is not connected", name, decl.Type)
				}
				r.Inputs[name] = nil
				continue
			}
			p := producer(addr, ref.Kind+"/"+ref.Name)
			if p == nil {
				continue
			}
			out, err := selectOutput(p, decl.Type, ref.Output)
			if err != nil {
				fail(addr, "input %q: %v", name, err)
				continue
			}
			r.Inputs[name] = &Binding{Producer: p.Addr, Output: out}
			edges[addr] = append(edges[addr], p.Addr)
		}
		walkStrings(r.Spec, func(s string) {
			for _, m := range RefPattern.FindAllStringSubmatch(s, -1) {
				ref, err := ParseRef(m[1])
				if err != nil {
					fail(addr, "%v", err)
					continue
				}
				p := producer(addr, ref.Addr())
				if p == nil {
					continue
				}
				if _, ok := p.Module.Module.Outputs[ref.Output]; !ok {
					fail(addr, "reference ${%s}: %s has no output %q", m[1], p.Addr, ref.Output)
					continue
				}
				edges[addr] = append(edges[addr], p.Addr)
			}
		})
	}

	if cycle := findCycle(edges); cycle != nil {
		errs = append(errs, fmt.Errorf("dependency cycle: %s", strings.Join(cycle, " -> ")))
	}
	if len(errs) > 0 {
		return nil, joinSorted(errs)
	}

	cfg := &Config{Environment: env.Name}
	for _, r := range resolved {
		cfg.Instances = append(cfg.Instances, r)
	}
	slices.SortFunc(cfg.Instances, func(a, b *Resolved) int { return strings.Compare(a.Addr, b.Addr) })
	return cfg, nil
}

// selectOutput picks the producer output that satisfies an input of type want.
func selectOutput(p *Resolved, want, named string) (string, error) {
	outputs := p.Module.Module.Outputs
	if named != "" {
		out, ok := outputs[named]
		switch {
		case !ok:
			return "", fmt.Errorf("%s has no output %q", p.Addr, named)
		case out.Type != want:
			return "", fmt.Errorf("needs %s, but output %q of %s is %s", want, named, p.Addr, out.Type)
		}
		return named, nil
	}
	var matches, types []string
	for name, out := range outputs {
		if out.Type == want {
			matches = append(matches, name)
		}
		types = append(types, out.Type)
	}
	slices.Sort(matches)
	slices.Sort(types)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("needs %s, but %s produces [%s]", want, p.Addr, strings.Join(slices.Compact(types), ", "))
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%s has several outputs of type %s [%s]; set \"output\"", p.Addr, want, strings.Join(matches, ", "))
	}
}

// findCycle returns one dependency cycle as a path of addresses, or nil.
func findCycle(edges map[string][]string) []string {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(n string) []string
	visit = func(n string) []string {
		state[n] = visiting
		stack = append(stack, n)
		deps := slices.Clone(edges[n])
		slices.Sort(deps)
		for _, d := range deps {
			switch state[d] {
			case visiting:
				i := slices.Index(stack, d)
				return append(slices.Clone(stack[i:]), d)
			case 0:
				if c := visit(d); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return nil
	}
	nodes := make([]string, 0, len(edges))
	for n := range edges {
		nodes = append(nodes, n)
	}
	slices.Sort(nodes)
	for _, n := range nodes {
		if state[n] == 0 {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case map[string]any:
		for _, x := range t {
			walkStrings(x, fn)
		}
	case []any:
		for _, x := range t {
			walkStrings(x, fn)
		}
	}
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
