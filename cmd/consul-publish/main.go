package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/AlekSi/pointer"
	"github.com/coreos/go-systemd/daemon"
	"github.com/jfk9w-go/confi"
	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"
	"github.com/jfk9w/consul-publish/internal/publish"
	"github.com/jfk9w/consul-publish/internal/target"
	"github.com/jfk9w/consul-publish/internal/target/hosts"
	"github.com/jfk9w/consul-publish/internal/target/porkbun"
)

type Config struct {
	Dump *struct {
		Schema bool `yaml:"schema,omitempty" doc:"Dump configuration schema in YAML."`
		Values bool `yaml:"values,omitempty" doc:"Dump configuration values in YAML."`
	} `yaml:"dump,omitempty" doc:"Dump info in stdout."`

	Address string `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string `yaml:"token" doc:"Consul token"`

	Once     bool          `yaml:"once,omitempty" doc:"Execute once and exit"`
	Interval time.Duration `yaml:"interval,omitempty" doc:"Consul watch interval" default:"1m"`
	Domain   api.Domain    `yaml:"domain" doc:"Domain settings"`

	Hosts struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable hosts target"`
		hosts.Config `yaml:",inline"`
	} `yaml:"hosts,omitempty" doc:"Hosts target settings"`

	Porkbun struct {
		Enabled        bool   `yaml:"enabled,omitempty" doc:"Enable Porkbun target"`
		CredentialsKey string `yaml:"credentialsKey,omitempty" doc:"Consul key for Porkbun credentials"`
		porkbun.Config `yaml:",inline"`
	} `yaml:"porkbun,omitempty" doc:"Porkbun target settings"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background())
	defer cancel()

	go func() {
		<-ctx.Done()
		notify(daemon.SdNotifyStopping)
	}()

	cfg, schema, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		log.Error(ctx, "read config", err)
		os.Exit(1)
	}

	if pointer.Get(cfg.Dump).Schema {
		dump(schema, confi.YAML)
		return
	}

	if pointer.Get(cfg.Dump).Values {
		cfg.Dump = nil
		dump(cfg, confi.YAML)
		return
	}

	defer log.Info(ctx, "shutdown")

	if err := run(ctx, cfg); err != nil {
		for _, err := range multierr.Errors(err) {
			log.Error(ctx, err.Error())
		}

		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *Config) error {
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

		ready()
	}

	return nil
}

func dump(value any, codec confi.Codec) {
	if err := codec.Marshal(value, os.Stdout); err != nil {
		panic(err)
	}
}

var ready = sync.OnceFunc(func() { notify(daemon.SdNotifyReady) })

func notify(state string) {
	if _, err := daemon.SdNotify(false, state); err != nil {
		log.Warn(context.Background(), "failed to notify systemd", err)
	}
}
