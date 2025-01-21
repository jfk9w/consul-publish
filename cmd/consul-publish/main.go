package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"
	"github.com/jfk9w/consul-publish/internal/publish"
	"github.com/jfk9w/consul-publish/internal/target"
	"github.com/jfk9w/consul-publish/internal/target/hosts"
	"github.com/jfk9w/consul-publish/internal/target/porkbun"
	"go.uber.org/multierr"

	"github.com/jfk9w-go/confi"
	"github.com/pkg/errors"
)

type Config struct {
	Address string `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string `yaml:"token,omitempty" doc:"Consul token"`

	Once     bool          `yaml:"once,omitempty" doc:"Execute once and exit"`
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

	defer log.Info(ctx, "shutdown")

	if err := run(ctx); err != nil {
		for _, err := range multierr.Errors(err) {
			log.Error(ctx, err.Error())
		}

		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, _, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		return errors.Wrap(err, "read config")
	}

	consul, err := api.NewConsul(cfg.Address, cfg.Token)
	if err != nil {
		return errors.Wrap(err, "create consul client")
	}

	interval := cfg.Interval
	if cfg.Once {
		interval = -1
	}

	targets := make(target.Registry).
		Add(cfg.Hosts.Enabled, "hosts", target.Hosts(cfg.Hosts.Config)).
		Add(cfg.Porkbun.Enabled, "porkbun", target.Porkbun(cfg.Porkbun.Config, consul, cfg.Porkbun.CredentialsKey))

	for nodes, err := range publish.Watch(ctx, consul, interval) {
		if err != nil || nodes == nil {
			return errors.Wrap(err, "watch consul")
		}

		local, err := consul.Local(ctx)
		if err != nil {
			return errors.Wrap(err, "get local node")
		}

		ctx := log.With(ctx, "local", local.Name)
		if err := publish.Run(ctx, cfg.Domain, local, nodes, targets); err != nil {
			return err
		}
	}

	return nil
}
