package target

import (
	"context"
	"encoding/json"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/target/hosts"
	"github.com/jfk9w/consul-publish/internal/target/porkbun"
)

type Registry map[string]Factory

func (r Registry) Add(enabled bool, name string, factory Factory) Registry {
	if enabled {
		r[name] = factory
	}

	return r
}

type Factory func(ctx context.Context) (api.Target, error)

func Hosts(cfg hosts.Config) Factory {
	return func(ctx context.Context) (api.Target, error) {
		return hosts.New(cfg), nil
	}
}

func Porkbun(cfg porkbun.Config, consul api.Consul, key string) Factory {
	return func(ctx context.Context) (api.Target, error) {
		data, err := consul.Key(ctx, key)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(data, &cfg.Credentials); err != nil {
			return nil, err
		}

		return porkbun.New(cfg), nil
	}
}
