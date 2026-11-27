package pulumicli

import (
	"slices"

	r "github.com/lambdawalker/go.infra.composer/reporting"
)

// identityMoves contains only old/new URN pairs explicitly matched by Pulumi.
// These are logical moves, not evidence of new or replaced physical resources.
type identityMoves map[string]string

func (m identityMoves) key(key string) string {
	original := key
	for range len(m) {
		next, ok := m[key]
		if !ok {
			return key
		}
		key = next
	}
	if _, cycle := m[key]; cycle {
		return original
	}
	return key
}

func (m identityMoves) keys(keys []string) []string {
	if keys == nil {
		return nil
	}
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = m.key(key)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (m identityMoves) artifacts(as []r.Artifact) []r.Artifact {
	// A previous preview can already have added a placeholder at the new key.
	// Prefer recorded physical identity over that placeholder. If the new key
	// already has a physical identity, keep it instead of reviving stale evidence.
	rank := func(a r.Artifact, key string) int {
		if a.ProviderID != "" {
			if a.Key == key {
				return 3
			}
			return 2
		}
		if !a.Observation.ObservedAt.IsZero() {
			return 1
		}
		return 0
	}
	var out []r.Artifact
	index := map[string]int{}
	for _, a := range as {
		key := m.key(a.Key)
		if i, ok := index[key]; ok {
			if rank(a, key) > rank(out[i], key) {
				out[i] = a
			}
		} else {
			index[key] = len(out)
			out = append(out, a)
		}
	}
	for i := range out {
		a := &out[i]
		key := m.key(a.Key)
		if key != a.Key && a.Name == name(a.Key) {
			a.Name = name(key)
		}
		a.Key = key
		a.Parent = m.key(a.Parent)
		a.Dependencies = m.keys(a.Dependencies)
	}
	return out
}
