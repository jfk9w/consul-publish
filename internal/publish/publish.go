package publish

import (
	"context"
	"iter"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"
	"github.com/jfk9w/consul-publish/internal/target"

	"github.com/pkg/errors"
	"go.uber.org/multierr"
)

func Run(ctx context.Context, domain api.Domain, local *api.Node, nodes iter.Seq[*api.Node], registry target.Registry) (errs error) {
	targets := make(map[string]api.Target)
	for name, factory := range registry {
		target, err := factory(ctx)
		if isError(&errs, err, "create target %s", name) {
			continue
		}

		targets[name] = target
	}

	for node := range nodes {
		ctx := log.With(ctx, "node", node.Name)

	targetLoop:
		for name, target := range targets {
			ctx := log.With(ctx, "target", name)
			if err := target.Node(ctx, domain.Node, local, node); isError(&errs, err, "update node %s in %s", node.Name, name) {
				delete(targets, name)
				continue targetLoop
			}

			for i := range node.Services {
				service := &node.Services[i]
				ctx := log.With(ctx, "service", service.Name)
				if err := target.Service(ctx, domain.Service, local, node, service); isError(&errs, err, "update service %s on %s in %s", service.ID, node.Name, name) {
					delete(targets, name)
					continue targetLoop
				}
			}
		}
	}

	for name, target := range targets {
		ctx := log.With(ctx, "target", name)
		if err := target.Commit(ctx, domain); isError(&errs, err, "commit %s", name) {
			continue
		}
	}

	return
}

func isError(errs *error, err error, msg string, args ...any) bool {
	return multierr.AppendInto(errs, errors.Wrapf(err, msg, args...))
}
