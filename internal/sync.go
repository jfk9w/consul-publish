package internal

import (
	"context"
	"iter"

	"github.com/jfk9w/consul-resource-sync/internal/api"
	"github.com/jfk9w/consul-resource-sync/internal/log"
	"github.com/jfk9w/consul-resource-sync/internal/target"
	"golang.org/x/sync/errgroup"

	"github.com/pkg/errors"
)

func Sync(ctx context.Context, domain api.Domain, local *api.Node, nodes iter.Seq[*api.Node], targets target.Registry) error {
	ctx = log.With(ctx, "local", local.Name)
	var eg errgroup.Group
	for name, factory := range targets {
		eg.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = errors.Errorf("panic: %v", r)
				}
			}()

			target, err := factory(ctx)
			if err != nil {
				return errors.Wrapf(err, "create target %s", name)
			}

			ctx := log.With(ctx, "target", name)
			for node := range nodes {
				ctx := log.With(ctx, "node", node.Name)
				println(node.Name)
				if err := target.Node(ctx, domain.Node, local, node); err != nil {
					return errors.Wrapf(err, "update node %s", node.Name)
				}

				for i := range node.Services {
					service := &node.Services[i]
					println(service.ID)
					ctx := log.With(ctx, "service", service.Name)
					if err := target.Service(ctx, domain.Node, local, node, service); err != nil {
						return errors.Wrapf(err, "update service %s", service.ID)
					}
				}
			}

			if err := target.Sync(ctx, domain); err != nil {
				return errors.Wrap(err, "sync")
			}

			return nil
		})
	}

	return eg.Wait()
}
