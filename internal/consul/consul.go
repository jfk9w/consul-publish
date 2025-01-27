package consul

import (
	"path/filepath"
	"strings"
)

type State struct {
	Self  string
	Nodes map[string]Node
	KV    Folder
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
