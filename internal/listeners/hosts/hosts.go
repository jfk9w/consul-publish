package hosts

import (
	"iter"
	"maps"
	"slices"
	"sort"
)

type hosts map[string]map[string]bool

func (h hosts) add(address string, name string) {
	names, ok := h[address]
	if !ok {
		names = make(map[string]bool)
		h[address] = names
	}

	names[name] = true
}

func (n hosts) iter() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		addresses := slices.Collect(maps.Keys(n))
		sort.Strings(addresses)
		for _, address := range addresses {
			names := slices.Collect(maps.Keys(n[address]))
			sort.Strings(names)
			if !yield(address, names) {
				return
			}
		}
	}
}
