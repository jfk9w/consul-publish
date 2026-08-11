package consul

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/tiendc/go-deepcopy"
	"golang.org/x/sync/errgroup"
)

type watcher struct {
	change chan change
	state  *State
	nodes  map[string]context.CancelFunc
	init   sync.WaitGroup
	work   *errgroup.Group
}

// Watch starts the Consul polling loop and blocks until ctx is cancelled or a fatal error occurs.
// It subscribes to node, service, and KV changes via blocking queries, debounces updates by 5 s,
// and calls Notify on every registered listener whenever the state actually changes.
func Watch(ctx context.Context, client *capi.Client, listeners ...Listener) error {
	self, err := client.Agent().NodeName()
	if err != nil {
		return fmt.Errorf("get local Consul node name: %w", err)
	}

	eg, ctx := errgroup.WithContext(ctx)
	w := &watcher{
		change: make(chan change, 999),
		state: &State{
			Self:  self,
			Nodes: make(map[string]Node),
			KV:    make(Folder),
		},
		nodes: make(map[string]context.CancelFunc),
		work:  eg,
	}

	keys := make(map[string]bool)
	for _, listener := range listeners {
		for _, prefix := range listener.KV() {
			prefix = strings.Trim(prefix, "/")
			prefix += "/"
			if _, ok := keys[prefix]; ok {
				continue
			}

			w.watchKeys(ctx, client, prefix)
			keys[prefix] = true
		}
	}

	w.watchNodes(ctx, client)

	w.work.Go(cancellable(ctx, func() error {
		w.init.Wait()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			break
		}

		log := slog.With("component", "consul")
		log.Info("watchers initialized", "consul_node", self, "kv_prefixes", len(keys))

		var (
			prev   *State
			change change
			ts     time.Time
			mu     sync.Mutex
		)

		for {
			select {
			case change = <-w.change:
				break
			case <-ctx.Done():
				return ctx.Err()
			}

			mu.Lock()
			change.change(w.state)
			ts = time.Now()
			mu.Unlock()

			if reflect.DeepEqual(w.state, prev) {
				continue
			}

			prev = new(State)
			if err := deepcopy.Copy(prev, w.state); err != nil {
				return err
			}

			tts := ts
			w.work.Go(cancellable(ctx, func() error {
				<-time.After(5 * time.Second)
				mu.Lock()
				defer mu.Unlock()
				if tts != ts {
					log.Debug("notification superseded by a newer change")
					return nil
				}

				nodes, services, kv := stateCounts(w.state)
				log.Info("catalog state changed",
					"nodes", nodes,
					"services", services,
					"kv_entries", kv,
					"listeners", len(listeners),
				)
				return notifyListeners(ctx, w.state, listeners)
			}))
		}
	}))

	return w.work.Wait()
}

func notifyListeners(ctx context.Context, source *State, listeners []Listener) error {
	observers := make([]ListenerObserver, 0, len(listeners))
	for _, listener := range listeners {
		if observer, ok := listener.(ListenerObserver); ok {
			observers = append(observers, observer)
		}
	}

	for _, listener := range listeners {
		name := listener.Name()
		log := slog.With("component", "listener", "listener", name)
		started := time.Now()

		var state State
		if err := deepcopy.Copy(&state, source); err != nil {
			return fmt.Errorf("copy state for listener %q: %w", name, err)
		}

		err := listener.Notify(ctx, &state)
		for _, observer := range observers {
			observer.ObserveListener(name, err)
		}
		if err != nil {
			log.Error("listener failed", "duration", time.Since(started), "error", err)
			continue
		}

		log.Debug("listener completed", "duration", time.Since(started))
	}

	return nil
}

func (w *watcher) watchNodes(ctx context.Context, client *capi.Client) {
	var (
		init = new(sync.WaitGroup)
		once sync.Once
	)

	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() (err error) {
		log := slog.With("component", "consul", "watcher", "nodes")
		log.Debug("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Debug("watcher stopped")
			}

			once.Do(w.init.Done)
		}()

		for nodes, err := range watch(ctx, client, nodes()) {
			if err != nil {
				return fmt.Errorf("watch Consul nodes: %w", err)
			}
			log.Debug("catalog nodes received", "nodes", len(nodes))

			actual := make(map[string]bool)
			for _, node := range nodes {
				w.watchServices(ctx, client, node.Node, init)
				actual[node.Node] = true
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case w.change <- nodeChange(nodes):
				break
			}

			for node, cancel := range w.nodes {
				if actual[node] {
					continue
				}

				cancel()
				delete(w.nodes, node)

				select {
				case <-ctx.Done():
					return ctx.Err()
				case w.change <- nodeDelete(node):
					break
				}
			}

			if init != nil {
				init.Wait()
				init = nil
				once.Do(w.init.Done)
			}
		}

		return
	}))
}

func (w *watcher) watchServices(ctx context.Context, client *capi.Client, node string, init *sync.WaitGroup) {
	if _, ok := w.nodes[node]; ok {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	w.nodes[node] = cancel

	if init != nil {
		init.Add(1)
	}

	w.work.Go(cancellable(ctx, func() (err error) {
		log := slog.With("component", "consul", "watcher", "services", "node", node)
		log.Debug("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Debug("watcher stopped")
			}

			cancel()
			if init != nil {
				init.Done()
			}
		}()

		for services, err := range watch(ctx, client, services(node)) {
			if err != nil {
				return fmt.Errorf("watch services on Consul node %q: %w", node, err)
			}
			log.Debug("node services received", "services", len(services.Services))

			select {
			case w.change <- (*serviceChange)(services):
				if init != nil {
					init.Done()
					init = nil
				}

			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return
	}))
}

func (w *watcher) watchKeys(ctx context.Context, client *capi.Client, prefix string) {
	var once sync.Once
	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() (err error) {
		log := slog.With("component", "consul", "watcher", "kv", "prefix", prefix)
		log.Debug("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Debug("watcher stopped")
			}

			once.Do(w.init.Done)
		}()

		for keys, err := range watch(ctx, client, keys(prefix)) {
			if err != nil {
				return fmt.Errorf("watch Consul KV prefix %q: %w", prefix, err)
			}
			log.Debug("KV entries received", "entries", len(keys))

			select {
			case w.change <- kvChange{
				prefix: prefix,
				kv:     keys,
			}:
				once.Do(w.init.Done)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return
	}))
}

func stateCounts(state *State) (nodes, services, kv int) {
	nodes = len(state.Nodes)
	for _, node := range state.Nodes {
		services += len(node.Services)
	}
	var countKV func(Folder)
	countKV = func(folder Folder) {
		for _, entry := range folder {
			switch entry := entry.(type) {
			case Folder:
				countKV(entry)
			case Value:
				kv++
			}
		}
	}
	countKV(state.KV)
	return nodes, services, kv
}

func nodes() WatchFunc[[]*capi.Node] {
	return func(client *capi.Client, options *capi.QueryOptions) ([]*capi.Node, *capi.QueryMeta, error) {
		return client.Catalog().Nodes(options)
	}
}

func services(node string) WatchFunc[*capi.CatalogNodeServiceList] {
	return func(client *capi.Client, options *capi.QueryOptions) (*capi.CatalogNodeServiceList, *capi.QueryMeta, error) {
		return client.Catalog().NodeServiceList(node, options)
	}
}

func keys(prefix string) WatchFunc[capi.KVPairs] {
	return func(client *capi.Client, options *capi.QueryOptions) (capi.KVPairs, *capi.QueryMeta, error) {
		return client.KV().List(prefix, options)
	}
}

// WatchFunc is a generic adapter over the Consul API blocking-query methods.
type WatchFunc[V any] func(client *capi.Client, options *capi.QueryOptions) (V, *capi.QueryMeta, error)

// watch returns an iterator that repeatedly issues a blocking query via fn and yields each
// new value as it arrives. It stops when ctx is cancelled or fn returns an error.
func watch[V any](ctx context.Context, client *capi.Client, fn WatchFunc[V]) iter.Seq2[V, error] {
	return func(yield func(V, error) bool) {
		var none V
		index := uint64(0)
		for {
			options := new(capi.QueryOptions)
			options.WaitIndex = index
			options.Datacenter = "dc1"

			value, meta, err := fn(client, options.WithContext(ctx))
			if err != nil {
				yield(none, err)
				return
			}

			if index == meta.LastIndex {
				continue
			}

			if !yield(value, nil) {
				return
			}

			index = meta.LastIndex
		}
	}
}

func cancellable(ctx context.Context, fn func() error) func() error {
	return func() error {
		err := fn()
		if isCanceled(ctx, err) {
			return nil
		}

		return err
	}
}

func isCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && ctx.Err() != nil
}
