package hosts

import (
	"iter"
	"maps"
	"slices"
	"sort"

	"github.com/jfk9w/consul-publish/internal/lib"
)

type hosts map[string]lib.Set[string]

func (h hosts) add(address string, name string) {
	names, ok := h[address]
	if !ok {
		names = make(lib.Set[string])
		h[address] = names
	}

	names.Add(name)
}

func (h hosts) iter() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		addresses := slices.Collect(maps.Keys(h))
		sort.Strings(addresses)
		for _, address := range addresses {
			names := h[address].Sort()
			if !yield(address, names) {
				return
			}
		}
	}
}
