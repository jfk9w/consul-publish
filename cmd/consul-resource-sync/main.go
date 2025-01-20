package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/jfk9w/consul-resource-sync/internal"
	"github.com/jfk9w/consul-resource-sync/internal/api"
	"github.com/jfk9w/consul-resource-sync/internal/log"
	"github.com/jfk9w/consul-resource-sync/internal/target"
	"github.com/jfk9w/consul-resource-sync/internal/target/hosts"
	"github.com/jfk9w/consul-resource-sync/internal/target/porkbun"

	"github.com/jfk9w-go/confi"
	"github.com/pkg/errors"
)

type Config struct {
	Consul struct {
		Address string `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
		Token   string `yaml:"token,omitempty" doc:"Consul token"`
	} `yaml:"consul" doc:"Consul connection settings"`

	Interval time.Duration `yaml:"interval,omitempty" doc:"Consul watch interval" default:"1m"`
	Domain   api.Domain    `yaml:"domain" doc:"Domain settings"`

	Hosts struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable hosts target"`
		hosts.Config `yaml:",inline"`
	} `yaml:"hosts" doc:"Hosts target settings"`

	Porkbun struct {
		Enabled        bool   `yaml:"enabled,omitempty" doc:"Enable Porkbun target"`
		CredentialsKey string `yaml:"credentialsKey" doc:"Consul key for Porkbun credentials"`
		porkbun.Config `yaml:",inline"`
	} `yaml:"porkbun" doc:"Porkbun target settings"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background())
	defer cancel()

	err := run(ctx)
	switch {
	case isContextRelated(ctx, err):
		return
	case err != nil:
		log.Error(ctx, "failed to run", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, _, err := confi.Get[Config](ctx, "github.com/jfk9w/consul-resource-sync")
	if err != nil {
		return errors.Wrap(err, "read config")
	}

	consul, err := api.NewConsul(cfg.Consul.Address, cfg.Consul.Token)
	if err != nil {
		return errors.Wrap(err, "create consul client")
	}

	targets := make(target.Registry).
		Add(cfg.Hosts.Enabled, "hosts", target.Hosts(cfg.Hosts.Config)).
		Add(cfg.Porkbun.Enabled, "porkbun", target.Porkbun(cfg.Porkbun.Config, consul, cfg.Porkbun.CredentialsKey))

	for nodes, err := range internal.Watch(ctx, consul, cfg.Interval) {
		if err != nil || nodes == nil {
			return errors.Wrap(err, "watch consul")
		}

		local, err := consul.Local(ctx)
		if err != nil {
			return errors.Wrap(err, "get local node")
		}

		if err := internal.Sync(ctx, cfg.Domain, local, nodes, targets); err != nil {
			return errors.Wrap(err, "sync targets")
		}
	}

	return nil
}

func isContextRelated(ctx context.Context, err error) bool {
	return (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() != nil
}
