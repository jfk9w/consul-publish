package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"github.com/AlekSi/pointer"
	"github.com/coreos/go-systemd/v22/daemon"
	capi "github.com/hashicorp/consul/api"
	"github.com/jfk9w-go/confi"
	"golang.org/x/sync/errgroup"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners/caddy"
	"github.com/jfk9w/consul-publish/internal/listeners/homepage"
	"github.com/jfk9w/consul-publish/internal/listeners/hosts"
	"github.com/jfk9w/consul-publish/internal/listeners/metrics"
	"github.com/jfk9w/consul-publish/internal/listeners/mikrotik"
)

type Config struct {
	Dump *struct {
		Schema bool `yaml:"schema,omitempty" doc:"Dump configuration schema in JSON"`
		Values bool `yaml:"values,omitempty" doc:"Dump configuration values in JSON"`
	} `yaml:"dump,omitempty" doc:"Dump configuration info"`

	Address string    `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string    `yaml:"token" doc:"Consul token"`
	Log     LogConfig `yaml:"log,omitempty" doc:"Logging settings"`

	Hosts struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable hosts target"`
		hosts.Config `yaml:",inline"`
	} `yaml:"hosts,omitempty" doc:"Hosts target settings"`

	Caddy struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable caddy target"`
		caddy.Config `yaml:",inline"`
	} `yaml:"caddy,omitempty" doc:"Caddy target settings"`

	Homepage struct {
		Enabled         bool `yaml:"enabled,omitempty" doc:"Enable Homepage target"`
		homepage.Config `yaml:",inline"`
	} `yaml:"homepage,omitempty" doc:"Homepage target settings"`

	Mikrotik struct {
		Enabled                 bool `yaml:"enabled,omitempty" doc:"Enable MikroTik DNS target"`
		mikrotik.ListenerConfig `yaml:",inline"`
	} `yaml:"mikrotik,omitempty" doc:"MikroTik DNS target settings"`

	Metrics struct {
		Enabled        bool `yaml:"enabled,omitempty" doc:"Enable Prometheus metrics exporter"`
		metrics.Config `yaml:",inline"`
	} `yaml:"metrics,omitempty" doc:"Prometheus metrics exporter settings"`
}

type LogConfig struct {
	Level  string `yaml:"level,omitempty"  default:"info" doc:"Minimum log level (debug, info, warn, or error)"`
	Format string `yaml:"format,omitempty" default:"text" doc:"Log output format (text or json)"`
	Source bool   `yaml:"source,omitempty"                doc:"Include source file and line in log records"`
}

func newConsulClient(address, token string) (*capi.Client, error) {
	cfg := capi.DefaultConfig()
	cfg.Address = address
	cfg.Token = token
	return capi.NewClient(cfg)
}

func newLogger(cfg LogConfig) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	options := &slog.HandlerOptions{AddSource: cfg.Source, Level: level}
	var handler slog.Handler
	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, options)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, options)
	default:
		return nil, fmt.Errorf("invalid log format %q: expected text or json", cfg.Format)
	}

	return slog.New(handler).With("service", "consul-publish"), nil
}

var exit = []os.Signal{
	syscall.SIGHUP,
	syscall.SIGINT,
	syscall.SIGQUIT,
	syscall.SIGABRT,
	syscall.SIGTERM,
}

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), exit...)
	defer cancel()

	cfg, schema, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	switch {
	case pointer.Get(cfg.Dump).Schema:
		return dump(schema, confi.JSON)
	case pointer.Get(cfg.Dump).Values:
		cfg.Dump = nil
		return dump(cfg, confi.JSON)
	}

	logger, err := newLogger(cfg.Log)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	client, err := newConsulClient(cfg.Address, cfg.Token)
	if err != nil {
		return fmt.Errorf("create Consul client: %w", err)
	}

	var listeners []consul.Listener

	if cfg.Hosts.Enabled {
		listeners = append(listeners, hosts.New(cfg.Hosts.Config))
	}

	if cfg.Caddy.Enabled {
		listeners = append(listeners, caddy.New(cfg.Caddy.Config))
	}

	if cfg.Homepage.Enabled {
		listeners = append(listeners, homepage.New(cfg.Homepage.Config))
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
	listenerNames := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		listenerNames = append(listenerNames, listener.Name())
	}
	slices.Sort(listenerNames)
	slog.Info("service starting", "consul_address", cfg.Address, "listeners", listenerNames)

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return consul.Watch(ctx, client, listeners...) })
	if metricsListener != nil {
		eg.Go(func() error { return metricsListener.ListenAndServe(ctx) })
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	slog.Info("service stopped")
	return nil
}

type systemdListener struct {
	once sync.Once
}

func (l *systemdListener) Name() string { return "systemd" }

func (l *systemdListener) KV() []string {
	return nil
}

func (l *systemdListener) Notify(ctx context.Context, state *consul.State) error {
	l.once.Do(func() {
		slog.Info("service ready", "consul_node", state.Self)
		notify(daemon.SdNotifyReady)
	})
	return nil
}

func notify(state string) {
	if _, err := daemon.SdNotify(false, state); err != nil {
		slog.Warn("failed to notify systemd", "component", "systemd", "error", err)
	}
}

func dump(value any, codec confi.Codec) error {
	if err := codec.Marshal(value, os.Stdout); err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	return nil
}
