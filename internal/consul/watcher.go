package consul

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
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

func Watch(ctx context.Context, client *capi.Client, listeners ...Listener) error {
	self, err := client.Agent().NodeName()
	if err != nil {
		return err
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
		notify(daemon.SdNotifyReady)
		slog.Info("watcher init complete")

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
					slog.Debug("skipping notification")
					return nil
				}

				slog.Info("notifying listeners")
				for _, listener := range listeners {
					var state State
					if err := deepcopy.Copy(&state, w.state); err != nil {
						return err
					}

					if err := listener.Notify(ctx, &state); err != nil {
						return err
					}
				}

				return nil
			}))
		}
	}))

	return w.work.Wait()
}

func (w *watcher) watchNodes(ctx context.Context, client *capi.Client) {
	init := new(sync.WaitGroup)
	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() error {
		defer func() {
			if init != nil {
				w.init.Done()
			}
		}()

		log := slog.Default()
		log.Info("watcher started")
		defer log.Info("watcher stopped")

		for nodes, err := range watch(ctx, client, nodes()) {
			if err != nil {
				log.Error(err.Error())
				return err
			}

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
				w.init.Done()
			}
		}

		return nil
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

	w.work.Go(cancellable(ctx, func() error {
		defer cancel()

		log := slog.With("node", node)
		log.Info("watcher started")
		defer log.Info("watcher stopped")

		for services, err := range watch(ctx, client, services(node)) {
			if err != nil {
				if init != nil {
					init.Done()
				}

				log.Error(err.Error())
				return err
			}

			select {
			case w.change <- (*serviceChange)(services):
				if init != nil {
					init.Done()
					init = nil
				} else {
					log.Debug("watcher update")
				}

			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	}))
}

func (w *watcher) watchKeys(ctx context.Context, client *capi.Client, prefix string) {
	var once sync.Once
	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() error {
		log := slog.With("prefix", prefix)
		log.Info("watcher started")
		defer log.Info("watcher stopped")

		for keys, err := range watch(ctx, client, keys(prefix)) {
			if err != nil {
				log.Error(err.Error())
				return err
			}

			select {
			case w.change <- kvChange{
				prefix: prefix,
				kv:     keys,
			}:
				log.Debug("watcher update")
				once.Do(w.init.Done)
			case <-ctx.Done():
				return err
			}
		}

		return nil
	}))
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

type WatchFunc[V any] func(client *capi.Client, options *capi.QueryOptions) (V, *capi.QueryMeta, error)

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
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return nil
		}

		return err
	}
}

func notify(state string) {
	if _, err := daemon.SdNotify(false, state); err != nil {
		slog.Warn("failed to notify systemd", "error", err)
	}
}
