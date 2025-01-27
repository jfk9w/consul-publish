package hosts

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/consul"
	. "github.com/jfk9w/consul-publish/internal/listeners"
	"github.com/pkg/errors"
)

type Config struct {
	File string `yaml:"file" default:"/etc/hosts" doc:"Hosts file path"`
}

type Listener struct {
	domains Domains
	cfg     Config
}

func New(domains Domains, cfg Config) Listener {
	return Listener{
		domains: domains,
		cfg:     cfg,
	}
}

func (l Listener) Keys() []string {
	return nil
}

func (l Listener) Notify(ctx context.Context, state *consul.State) error {
	self := state.Nodes[state.Self]
	hosts := make(hosts)
	for _, node := range state.Nodes {
		address := node.Address
		if self.ID == node.ID {
			address = LocalIP
			if GetVisibility(node) == Public {
				hosts.add(address, api.Subdomain(node.Name, l.domains.Node))
			}
		}

		hosts.add(address, node.Name)
	}

	if err := l.write(hosts); err != nil {
		return errors.Wrap(err, "write hosts")
	}

	return nil
}

func (l Listener) write(hosts hosts) error {
	file, err := os.CreateTemp(filepath.Dir(l.cfg.File), ".consul-publish-hosts-")
	if err != nil {
		return errors.Wrap(err, "create temp file")
	}

	if err := file.Chmod(0o644); err != nil {
		return errors.Wrap(err, "chmod temp file")
	}

	defer os.RemoveAll(file.Name())

	for address, names := range hosts.iter() {
		if _, err := file.WriteString(address + " " + strings.Join(names, " ") + "\n"); err != nil {
			return errors.Wrap(err, "write to temp file")
		}
	}

	if err := file.Close(); err != nil {
		return errors.Wrap(err, "close temp file")
	}

	if err := os.Rename(file.Name(), l.cfg.File); err != nil {
		return errors.Wrap(err, "rename temp file")
	}

	slog.Info("updated hosts file", "path", l.cfg.File)
	return nil
}
