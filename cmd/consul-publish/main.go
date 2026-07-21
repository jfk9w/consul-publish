package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/AlekSi/pointer"
	"github.com/coreos/go-systemd/v22/daemon"
	capi "github.com/hashicorp/consul/api"
	"github.com/jfk9w-go/confi"
	"golang.org/x/sync/errgroup"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners/caddy"
	"github.com/jfk9w/consul-publish/internal/listeners/hosts"
	"github.com/jfk9w/consul-publish/internal/listeners/metrics"
	"github.com/jfk9w/consul-publish/internal/listeners/mikrotik"
)

type Config struct {
	Dump *struct {
		Schema bool `yaml:"schema,omitempty" doc:"Dump configuration schema in JSON"`
		Values bool `yaml:"values,omitempty" doc:"Dump configuration values in JSON"`
	} `yaml:"dump,omitempty" doc:"Dump configuration info"`

	Address string `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string `yaml:"token" doc:"Consul token"`

	Hosts struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable hosts target"`
		hosts.Config `yaml:",inline"`
	} `yaml:"hosts,omitempty" doc:"Hosts target settings"`

	Caddy struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable caddy target"`
		caddy.Config `yaml:",inline"`
	} `yaml:"caddy,omitempty" doc:"Caddy target settings"`

	Mikrotik struct {
		Enabled                 bool `yaml:"enabled,omitempty" doc:"Enable MikroTik DNS target"`
		mikrotik.ListenerConfig `yaml:",inline"`
	} `yaml:"mikrotik,omitempty" doc:"MikroTik DNS target settings"`

	Metrics struct {
		Enabled        bool `yaml:"enabled,omitempty" doc:"Enable Prometheus metrics exporter"`
		metrics.Config `yaml:",inline"`
	} `yaml:"metrics,omitempty" doc:"Prometheus metrics exporter settings"`
}

func newConsulClient(address, token string) (*capi.Client, error) {
	cfg := capi.DefaultConfig()
	cfg.Address = address
	cfg.Token = token
	return capi.NewClient(cfg)
}

func newLogger() *slog.Logger {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "source" {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					fn := source.Function
					slash := strings.LastIndex(fn, "/")
					if slash > 0 {
						fn = fn[slash+1:]
					}

					return slog.String("caller", fn)
				}
			}

			return a
		},
	})

	return slog.New(handler)
}

var exit = []os.Signal{
	syscall.SIGHUP,
	syscall.SIGINT,
	syscall.SIGQUIT,
	syscall.SIGABRT,
	syscall.SIGTERM,
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), exit...)
	defer cancel()

	cfg, schema, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		panic(err)
	}

	switch {
	case pointer.Get(cfg.Dump).Schema:
		dump(schema, confi.JSON)
		return
	case pointer.Get(cfg.Dump).Values:
		cfg.Dump = nil
		dump(cfg, confi.JSON)
		return
	}

	client, err := newConsulClient(cfg.Address, cfg.Token)
	if err != nil {
		panic(err)
	}

	slog.SetDefault(newLogger())

	var listeners []consul.Listener

	if cfg.Hosts.Enabled {
		listeners = append(listeners, hosts.New(cfg.Hosts.Config))
	}

	if cfg.Caddy.Enabled {
		listeners = append(listeners, caddy.New(cfg.Caddy.Config))
	}

	if cfg.Mikrotik.Enabled {
		listeners = append(listeners, mikrotik.NewListener(cfg.Mikrotik.ListenerConfig))
	}

	var metricsListener *metrics.Listener
	if cfg.Metrics.Enabled {
		metricsListener = metrics.New(cfg.Metrics.Config)
		listeners = append(listeners, metricsListener)
	}

	listeners = append(listeners, new(systemdListener))

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return consul.Watch(ctx, client, listeners...) })
	if metricsListener != nil {
		eg.Go(func() error { return metricsListener.ListenAndServe(ctx) })
	}

	if err := eg.Wait(); err != nil {
		panic(err)
	}

	slog.Info("shutdown")
}

type systemdListener struct {
	once sync.Once
}

func (l *systemdListener) KV() []string {
	return nil
}

func (l *systemdListener) Notify(ctx context.Context, state *consul.State) error {
	l.once.Do(func() { notify(daemon.SdNotifyReady) })
	return nil
}

func notify(state string) {
	if _, err := daemon.SdNotify(false, state); err != nil {
		slog.Warn("failed to notify systemd", "error", err)
	}
}

func dump(value any, codec confi.Codec) {
	if err := codec.Marshal(value, os.Stdout); err != nil {
		panic(err)
	}
}
