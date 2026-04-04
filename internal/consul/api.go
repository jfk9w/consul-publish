// Package consul provides the Consul watcher and the data types it produces.
package consul

import "context"

// Listener is implemented by any target that wants to react to Consul state changes.
// KV returns the list of KV prefixes the listener needs; Watch subscribes to them.
// Notify is called after every debounced state change with a fresh deep copy of the state.
type Listener interface {
	KV() []string
	Notify(ctx context.Context, state *State) error
}
