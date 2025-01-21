package api

import (
	"context"
	"iter"
	"sort"

	capi "github.com/hashicorp/consul/api"
	"github.com/pkg/errors"
)

type Consul interface {
	Local(ctx context.Context) (*Node, error)
	Nodes(ctx context.Context) iter.Seq2[*Node, error]
	Key(ctx context.Context, key string) ([]byte, error)
}

type consul struct {
	client *capi.Client
}

func NewConsul(address, token string) (Consul, error) {
	cfg := capi.DefaultConfig()
	cfg.Address = address
	cfg.Token = token

	client, err := capi.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &consul{
		client: client,
	}, nil
}

func (c *consul) Local(ctx context.Context) (*Node, error) {
	name, err := c.client.Agent().NodeName()
	if err != nil {
		return nil, errors.Wrap(err, "get consul agent node name")
	}

	node, _, err := c.client.Catalog().Node(name, new(capi.QueryOptions).WithContext(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "get consul agent node")
	}

	return NewNode(node.Node), nil
}

func (c *consul) Nodes(ctx context.Context) iter.Seq2[*Node, error] {
	return func(yield func(*Node, error) bool) {
		nodes, _, err := c.client.Catalog().Nodes(new(capi.QueryOptions).WithContext(ctx))
		if err != nil {
			yield(nil, errors.Wrap(err, "list consul catalog nodes"))
			return
		}

		for _, node := range nodes {
			catalogNode, _, err := c.client.Catalog().Node(node.Node, new(capi.QueryOptions).WithContext(ctx))
			if err != nil {
				yield(nil, errors.Wrap(err, "get consul catalog node"))
				return
			}

			node := NewNode(node)
			for _, service := range catalogNode.Services {
				node.Services = append(node.Services, *NewService(service))
			}

			sort.Slice(node.Services, func(i, j int) bool { return node.Services[i].ID < node.Services[j].ID })

			if !yield(node, nil) {
				return
			}
		}
	}
}

func (c *consul) Key(ctx context.Context, key string) ([]byte, error) {
	kv, _, err := c.client.KV().Get(key, new(capi.QueryOptions).WithContext(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "get consul key")
	}

	if kv == nil {
		return nil, nil
	}

	return kv.Value, nil
}
