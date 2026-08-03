package hosts

import (
	"iter"
	"maps"
	"slices"
	"sort"

	"github.com/jfk9w/consul-publish/internal/lib"
)

type hostNames struct {
	canonical string
	aliases   lib.Set[string]
}

type hosts map[string]*hostNames

func (h hosts) add(address string, name string) {
	names := h.names(address)
	if names.canonical == name {
		return
	}
	names.aliases.Add(name)
}

func (h hosts) addCanonical(address string, name string) {
	names := h.names(address)
	names.canonical = name
	delete(names.aliases, name)
}

func (h hosts) names(address string) *hostNames {
	names, ok := h[address]
	if !ok {
		names = &hostNames{
			aliases: make(lib.Set[string]),
		}
		h[address] = names
	}

	return names
}

func (h hosts) iter() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		addresses := slices.Collect(maps.Keys(h))
		sort.Strings(addresses)
		for _, address := range addresses {
			names := h[address].aliases.Sort()
			if canonical := h[address].canonical; canonical != "" {
				names = append([]string{canonical}, names...)
			}
			if !yield(address, names) {
				return
			}
		}
	}
}
