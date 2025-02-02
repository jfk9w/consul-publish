package consul

import "context"

type Listener interface {
	KV() []string
	Notify(ctx context.Context, state *State) error
}
