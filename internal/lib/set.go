package lib

import (
	"cmp"
	"maps"
	"slices"
	"sort"
)

type Set[K cmp.Ordered] map[K]bool

func SetOf[K cmp.Ordered](keys ...K) Set[K] {
	set := make(Set[K])
	set.Add(keys...)
	return set
}

func (s Set[K]) Add(keys ...K) {
	for _, key := range keys {
		s[key] = true
	}
}

func (s Set[K]) Sort() []K {
	keys := slices.Collect(maps.Keys(s))
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
