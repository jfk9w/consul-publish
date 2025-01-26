package main

import (
	"context"
	"iter"
	"os/signal"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	capi "github.com/hashicorp/consul/api"
	"github.com/jfk9w-go/confi"
)

type Config struct {
	Address string `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string `yaml:"token" doc:"Consul token"`
}

func newConsulClient(address, token string) (*capi.Client, error) {
	cfg := capi.DefaultConfig()
	cfg.Address = address
	cfg.Token = token
	return capi.NewClient(cfg)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGILL, syscall.SIGHUP)
	defer cancel()

	cfg, _, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		panic(err)
	}

	client, err := newConsulClient(cfg.Address, cfg.Token)
	if err != nil {
		panic(err)
	}

	for nodes, err := range watch(ctx, client, nodes()) {
		if err != nil {
			panic(err)
		}

		spew.Dump(nodes)
		spew.Dump(time.Now())
	}
}

func nodes() WatchFunc[[]*capi.Node] {
	return func(client *capi.Client, options *capi.QueryOptions) ([]*capi.Node, *capi.QueryMeta, error) {
		return client.Catalog().Nodes(options)
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
