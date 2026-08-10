package consul

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotifyListenersContinuesAfterListenerError(t *testing.T) {
	wantErr := errors.New("reload failed")
	failing := &testListener{name: "caddy", err: wantErr}
	succeeding := &testListener{name: "homepage"}
	observer := &testObserver{testListener: testListener{name: "metrics"}}

	err := notifyListeners(t.Context(), &State{Nodes: map[string]Node{}, KV: Folder{}}, []Listener{
		failing,
		succeeding,
		observer,
	})
	require.NoError(t, err)
	require.Equal(t, 1, failing.calls)
	require.Equal(t, 1, succeeding.calls)
	require.Equal(t, map[string]bool{
		"caddy":    false,
		"homepage": true,
		"metrics":  true,
	}, observer.statuses)
}

type testListener struct {
	name  string
	err   error
	calls int
}

func (l *testListener) Name() string { return l.name }
func (l *testListener) KV() []string { return nil }
func (l *testListener) Notify(context.Context, *State) error {
	l.calls++
	return l.err
}

type testObserver struct {
	testListener
	statuses map[string]bool
}

func (o *testObserver) ObserveListener(name string, err error) {
	if o.statuses == nil {
		o.statuses = make(map[string]bool)
	}
	o.statuses[name] = err == nil
}
