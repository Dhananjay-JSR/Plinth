// Package merge applies an environment override to a blueprint spec.
package merge

// Merge returns override merged over base: objects merge recursively, while
// arrays, scalars and null in the override replace the base value. Keys absent
// from the override keep the base value. Neither input is modified.
func Merge(base, override any) any {
	b, bok := base.(map[string]any)
	o, ook := override.(map[string]any)
	if !bok || !ook {
		return override
	}
	out := make(map[string]any, len(b)+len(o))
	for k, v := range b {
		out[k] = v
	}
	for k, v := range o {
		if bv, ok := b[k]; ok {
			out[k] = Merge(bv, v)
		} else {
			out[k] = v
		}
	}
	return out
}
