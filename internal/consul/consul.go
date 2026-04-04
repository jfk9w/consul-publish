package consul

import (
	"iter"
	"path/filepath"
	"strings"

	"github.com/jfk9w/consul-publish/internal/lib"
)

// NodeGroupsKey is the node metadata key that holds a space-separated list of group names.
const (
	NodeGroupsKey = "groups"
)

// State is a snapshot of the Consul catalog at a point in time.
// Self is the name of the local node. Nodes is keyed by node name.
// KV holds the watched KV tree as a nested Folder.
type State struct {
	Self  string
	Nodes map[string]Node
	KV    Folder

	groups map[string]lib.Set[string]
}

// InGroup reports whether the current node (identified by name) belongs to any
// of the groups listed in meta[key]. Groups are resolved lazily and cached.
func (s *State) InGroup(meta map[string]string, key string, name string) bool {
	for _, group := range strings.Fields(meta[key]) {
		group := s.Group(group)
		if group[name] {
			return true
		}
	}

	return false
}

// Group returns the set of node names that belong to the named group.
// The special group "all" contains every node. Node names are also valid group names.
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

// Service represents a single Consul service registration on a node.
type Service struct {
	ID      string
	Name    string
	Address string
	Port    int
	Tags    map[string]bool
	Meta    map[string]string
}

// Node represents a Consul catalog node together with all its service registrations.
type Node struct {
	ID       string
	Name     string
	Address  string
	Groups   lib.Set[string]
	Meta     map[string]string
	Services []Service
}

// KV is the sealed interface for entries in the KV tree (either a Folder or a Value).
type KV interface {
	__kv()
}

// Folder is an intermediate node in the KV tree, mapping path components to child entries.
type Folder map[string]KV

func (Folder) __kv() {}

// Get traverses the KV tree along the given slash-separated path and returns the entry,
// or nil if any segment is missing or not a Folder.
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

// Values iterates over the direct Value children of this Folder, skipping nested Folders.
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

// Value is a leaf entry in the KV tree, holding the raw bytes of a Consul KV entry.
type Value []byte

func (Value) __kv() {}
