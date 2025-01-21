package publish

import (
	"context"
	"iter"
	"maps"
	"reflect"
	"time"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"
)

func Watch(ctx context.Context, consul api.Consul, interval time.Duration) iter.Seq2[iter.Seq[*api.Node], error] {
	return func(yield func(iter.Seq[*api.Node], error) bool) {
		var prev map[string]*api.Node
		for {
			nodes := make(map[string]*api.Node)
			for node, err := range consul.Nodes(ctx) {
				if err != nil {
					if !yield(nil, err) {
						return
					}
				}

				nodes[node.ID] = node
			}

			if !reflect.DeepEqual(prev, nodes) {
				log.Info(ctx, "detected changes, starting sync")
				if !yield(maps.Values(nodes), nil) {
					return
				}

				prev = nodes
			}

			if interval <= 0 {
				return
			}

			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return
			}
		}
	}
}
