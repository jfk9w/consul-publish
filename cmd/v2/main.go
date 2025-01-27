package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/davecgh/go-spew/spew"
	capi "github.com/hashicorp/consul/api"
	"github.com/jfk9w-go/confi"
	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners"
	"github.com/jfk9w/consul-publish/internal/listeners/hosts"
)

type Config struct {
	Address string            `yaml:"address,omitempty" doc:"Consul address" default:"127.0.0.1:8500"`
	Token   string            `yaml:"token" doc:"Consul token"`
	Domains listeners.Domains `yaml:"domain"`

	Hosts struct {
		Enabled      bool `yaml:"enabled,omitempty" doc:"Enable hosts target"`
		hosts.Config `yaml:",inline"`
	} `yaml:"hosts,omitempty" doc:"Hosts target settings"`
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

	cfg, _, err := confi.Get[Config](ctx, "consul-publish")
	if err != nil {
		panic(err)
	}

	client, err := newConsulClient(cfg.Address, cfg.Token)
	if err != nil {
		panic(err)
	}

	slog.SetDefault(newLogger())
	defer slog.Info("shutdown")

	var listeners []consul.Listener

	if cfg.Hosts.Enabled {
		listeners = append(listeners, hosts.New(cfg.Domains, cfg.Hosts.Config))
	}

	if err := consul.Watch(ctx, client, listeners...); err != nil {
		panic(err)
	}
}

type DebugListener struct{}

func (DebugListener) Keys() []string {
	return []string{
		"caddy/service",
	}
}

func (DebugListener) Notify(ctx context.Context, state *consul.State) error {
	spew.Dump(state)
	return nil
}
