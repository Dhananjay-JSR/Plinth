package merge

import (
	"reflect"
	"testing"
)

type m = map[string]any

func TestMerge(t *testing.T) {
	for name, tc := range map[string]struct{ base, override, want any }{
		"objects merge recursively": {
			m{"replicas": 2, "resources": m{"cpu": "250m", "memory": "2Gi"}},
			m{"replicas": 5, "resources": m{"memory": "8Gi"}},
			m{"replicas": 5, "resources": m{"cpu": "250m", "memory": "8Gi"}},
		},
		"absent keys inherit":    {m{"a": 1, "b": 2}, m{}, m{"a": 1, "b": 2}},
		"new keys are added":     {m{"a": 1}, m{"b": 2}, m{"a": 1, "b": 2}},
		"arrays are replaced":    {m{"ports": []any{80, 443}}, m{"ports": []any{8080}}, m{"ports": []any{8080}}},
		"null is a value":        {m{"cache": m{"size": 1}}, m{"cache": nil}, m{"cache": nil}},
		"scalar replaces object": {m{"x": m{"y": 1}}, m{"x": "flat"}, m{"x": "flat"}},
		"object replaces scalar": {m{"x": "flat"}, m{"x": m{"y": 1}}, m{"x": m{"y": 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Merge(tc.base, tc.override); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeDoesNotModifyInputs(t *testing.T) {
	base := m{"resources": m{"memory": "2Gi"}}
	override := m{"resources": m{"memory": "8Gi"}}
	Merge(base, override)
	if base["resources"].(m)["memory"] != "2Gi" || override["resources"].(m)["memory"] != "8Gi" {
		t.Errorf("inputs modified: %v %v", base, override)
	}
}
