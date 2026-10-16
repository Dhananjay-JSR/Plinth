package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// driftReport turns `tofu show -json <refresh-only plan>` into one entry per
// drifted resource, under the blueprint's own <kind>/<name> addresses.
// Values OpenTofu marks sensitive are never printed.
func driftReport(planJSON []byte) ([]string, error) {
	var plan struct {
		ResourceDrift []struct {
			Address       string `json:"address"`
			ModuleAddress string `json:"module_address"`
			Type          string `json:"type"`
			Name          string `json:"name"`
			Change        struct {
				Actions         []string `json:"actions"`
				Before          any      `json:"before"`
				After           any      `json:"after"`
				BeforeSensitive any      `json:"before_sensitive"`
				AfterSensitive  any      `json:"after_sensitive"`
			} `json:"change"`
		} `json:"resource_drift"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, fmt.Errorf("reading tofu plan JSON: %w", err)
	}
	var report []string
	for _, d := range plan.ResourceDrift {
		where := instanceAddr(d.ModuleAddress, d.Address)
		res := d.Type + "." + d.Name
		c := d.Change
		if slices.Equal(c.Actions, []string{"delete"}) {
			report = append(report, fmt.Sprintf("- %s  %s: deleted outside Plinth", where, res))
			continue
		}
		var lines []string
		diff("", c.Before, c.After, c.BeforeSensitive, c.AfterSensitive, &lines)
		entry := fmt.Sprintf("~ %s  %s", where, res)
		for _, l := range lines {
			entry += "\n    " + l
		}
		report = append(report, entry)
	}
	slices.Sort(report)
	return report, nil
}

// instanceAddr maps a generated module address back to a resource instance
// address: module.application__checkout-api → application/checkout-api.
func instanceAddr(moduleAddr, resourceAddr string) string {
	call, ok := strings.CutPrefix(moduleAddr, "module.")
	if !ok {
		return resourceAddr // not inside a Plinth module call
	}
	call, _, _ = strings.Cut(call, ".") // nested modules belong to the top-level call
	call, _, _ = strings.Cut(call, "[")
	kind, name, ok := strings.Cut(call, "__")
	if !ok {
		return resourceAddr
	}
	return kind + "/" + name
}

// diff appends "path: before → after" for every changed leaf. Objects are
// compared key by key; anything else is compared whole.
func diff(path string, before, after, sensBefore, sensAfter any, out *[]string) {
	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		slices.Sort(sorted)
		for _, k := range sorted {
			p := k
			if path != "" {
				p = path + "." + k
			}
			diff(p, bm[k], am[k], child(sensBefore, k), child(sensAfter, k), out)
		}
		return
	}
	if reflect.DeepEqual(before, after) {
		return
	}
	if anySensitive(sensBefore) || anySensitive(sensAfter) {
		*out = append(*out, path+": (sensitive value changed)")
		return
	}
	*out = append(*out, fmt.Sprintf("%s: %s → %s", path, show(before), show(after)))
}

func child(sens any, key string) any {
	if m, ok := sens.(map[string]any); ok {
		return m[key]
	}
	return sens // true covers the whole subtree
}

// anySensitive reports whether OpenTofu's sensitivity marker covers any part
// of a value, so a partly secret list or object is redacted whole.
func anySensitive(sens any) bool {
	switch t := sens.(type) {
	case bool:
		return t
	case map[string]any:
		for _, v := range t {
			if anySensitive(v) {
				return true
			}
		}
	case []any:
		for _, v := range t {
			if anySensitive(v) {
				return true
			}
		}
	}
	return false
}

func show(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
