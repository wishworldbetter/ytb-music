package ytm

import "strings"

// InnerTube answers in deep trees of renderers. These walk them as decoded
// by encoding/json into any: a path is of keys (string) and indices (int,
// negative from the end), and a step that doesn't fit gives nil.

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok {
				return nil
			}
			if k < 0 {
				k += len(a)
			}
			if k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		default:
			return nil
		}
	}
	return v
}

func str(v any, path ...any) string {
	s, _ := dig(v, path...).(string)
	return s
}

func arr(v any, path ...any) []any {
	a, _ := dig(v, path...).([]any)
	return a
}

func obj(v any, path ...any) map[string]any {
	m, _ := dig(v, path...).(map[string]any)
	return m
}

func num(v any, path ...any) (float64, bool) {
	f, ok := dig(v, path...).(float64)
	return f, ok
}

// text is what a text object says: its runs joined, or its simpleText.
func text(v any) string {
	if s := str(v, "simpleText"); s != "" {
		return s
	}
	var b strings.Builder
	for _, r := range arr(v, "runs") {
		b.WriteString(str(r, "text"))
	}
	return b.String()
}

// find returns the first value of key under v, depth first.
func find(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok {
			return x
		}
		for _, x := range t {
			if f := find(x, key); f != nil {
				return f
			}
		}
	case []any:
		for _, x := range t {
			if f := find(x, key); f != nil {
				return f
			}
		}
	}
	return nil
}

// findAll appends every value of key under v, not looking inside them.
func findAll(v any, key string, out []any) []any {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok {
			return append(out, x)
		}
		for _, x := range t {
			out = findAll(x, key, out)
		}
	case []any:
		for _, x := range t {
			out = findAll(x, key, out)
		}
	}
	return out
}

// only returns the name and the value of a one-key object, as each item
// of a list of renderers is.
func only(v any) (string, any) {
	m, _ := v.(map[string]any)
	for k, x := range m {
		return k, x
	}
	return "", nil
}
