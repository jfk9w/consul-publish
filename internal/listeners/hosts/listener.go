package hosts

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"

	"github.com/jfk9w/consul-publish/internal/consul"
	. "github.com/jfk9w/consul-publish/internal/listeners"
)

type Config struct {
	File File `yaml:",inline"`
}

type Listener struct {
	cfg Config
}

func New(cfg Config) Listener {
	return Listener{
		cfg: cfg,
	}
}

func (l Listener) KV() []string {
	return nil
}

func (l Listener) Notify(ctx context.Context, state *consul.State) error {
	self := state.Nodes[state.Self]
	hosts := make(hosts)
	for _, node := range state.Nodes {
		address := node.Address
		if self.ID == node.ID {
			address = LocalIP
			if domain, ok := GetDomainName(node.Meta); ok {
				hosts.add(address, domain)
			}
		}

		hosts.add(address, node.Name)
	}

	_, err := l.cfg.File.Write(func(file io.Writer) error {
		for address, names := range hosts.iter() {
			if _, err := fmt.Fprintln(file, address, strings.Join(names, " ")); err != nil {
				return errors.Wrap(err, "write to temp file")
			}
		}

		return nil
	})

	return err
}
