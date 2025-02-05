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
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			break
		}

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
	var (
		init = new(sync.WaitGroup)
		once sync.Once
	)

	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() (err error) {
		log := slog.Default()
		log.Info("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Info("watcher stopped")
			} else {
				log.Warn("watcher error", "error", err)
			}

			once.Do(w.init.Done)
		}()

		for nodes, err := range watch(ctx, client, nodes()) {
			if err != nil {
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
		log := slog.With("node", node)
		log.Info("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Info("watcher stopped")
			} else {
				log.Warn("watcher error", "error", err)
			}

			cancel()
			if init != nil {
				init.Done()
			}
		}()

		for services, err := range watch(ctx, client, services(node)) {
			if err != nil {
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

		return
	}))
}

func (w *watcher) watchKeys(ctx context.Context, client *capi.Client, prefix string) {
	var once sync.Once
	w.init.Add(1)
	w.work.Go(cancellable(ctx, func() (err error) {
		log := slog.With("prefix", prefix)
		log.Info("watcher started")
		defer func() {
			if isCanceled(ctx, err) {
				log.Info("watcher stopped")
			} else {
				log.Warn("watcher error", "error", err)
			}

			once.Do(w.init.Done)
		}()

		for keys, err := range watch(ctx, client, keys(prefix)) {
			if err != nil {
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
				return ctx.Err()
			}
		}

		return
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
		if isCanceled(ctx, err) {
			return nil
		}

		return err
	}
}

func isCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && ctx.Err() != nil
}
