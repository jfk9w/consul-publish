package consul

import (
	"iter"
	"path/filepath"
	"strings"

	"github.com/jfk9w/consul-publish/internal/lib"
)

const (
	NodeGroupsKey = "groups"
)

type State struct {
	Self  string
	Nodes map[string]Node
	KV    Folder

	groups map[string]lib.Set[string]
}

func (s *State) InGroup(meta map[string]string, key string, name string) bool {
	for _, group := range strings.Fields(meta[key]) {
		group := s.Group(group)
		if group[name] {
			return true
		}
	}

	return false
}

func (s *State) Group(name string) lib.Set[string] {
	if s.groups != nil {
		return s.groups[name]
	}

	s.groups = map[string]lib.Set[string]{
		"all": make(lib.Set[string]),
	}

	for _, node := range s.Nodes {
		s.groups[node.Name] = lib.SetOf(node.Name)
		s.groups["all"].Add(node.Name)
		for group := range node.Groups {
			nodes := s.groups[group]
			if nodes == nil {
				nodes = make(lib.Set[string])
				s.groups[group] = nodes
			}

			nodes.Add(node.Name)
		}
	}

	return s.groups[name]
}

type Service struct {
	ID      string
	Name    string
	Address string
	Port    int
	Tags    map[string]bool
	Meta    map[string]string
}

type Node struct {
	ID       string
	Name     string
	Address  string
	Groups   lib.Set[string]
	Meta     map[string]string
	Services []Service
}

type KV interface {
	__kv()
}

type Folder map[string]KV

func (Folder) __kv() {}

func (f Folder) Get(path string) KV {
	var entry KV = f
	for _, key := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if key == "" {
			continue
		}

		switch curr := entry.(type) {
		case Folder:
			entry = curr[key]
		default:
			return nil
		}
	}

	return entry
}

func (f Folder) Values() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		for key, value := range f {
			value, ok := value.(Value)
			if !ok {
				continue
			}

			if !yield(key, value) {
				return
			}
		}
	}
}

func (f Folder) set(path string, entry KV) {
	path = filepath.Clean(path)
	dir, name := filepath.Split(path)
	parent := f
	for _, key := range strings.Split(dir, string(filepath.Separator)) {
		if key == "" {
			continue
		}

		curr := parent[key]
		if curr, ok := curr.(Folder); ok {
			parent = curr
			continue
		}

		entry := make(Folder)
		parent[key] = entry
		parent = entry
	}

	parent[name] = entry
}

type Value []byte

func (Value) __kv() {}
