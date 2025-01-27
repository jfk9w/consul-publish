package consul

import "context"

type Listener interface {
	Keys() []string
	Notify(ctx context.Context, state *State) error
}
