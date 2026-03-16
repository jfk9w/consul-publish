// Package hosts implements a Listener that writes /etc/hosts from the Consul catalog.
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

// Config holds the file output settings for the hosts listener.
type Config struct {
	File File `yaml:",inline"`
}

// Listener writes /etc/hosts (or a custom path) based on the Consul node and service inventory.
type Listener struct {
	cfg Config
}

// New creates a Listener with the given configuration.
func New(cfg Config) Listener {
	return Listener{
		cfg: cfg,
	}
}

func (l Listener) KV() []string {
	return nil
}

// Notify regenerates the hosts file from the current Consul state.
// Each node is mapped to its IP address; the local node is mapped to 127.0.0.1.
// Services with a domain-name metadata key that are published via publish-http get
// an additional 127.0.0.1 entry for each domain name.
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

		for _, service := range node.Services {
			for _, domain := range GetHTTPDomainNames(state, service.Meta) {
				hosts.add(LocalIP, domain)
			}
		}
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
