// Package hosts implements a Listener that writes /etc/hosts from the Consul catalog.
package hosts

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/lib"
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
// Service IDs and published domain names are added as aliases when they occur on
// exactly one node. The local node gets all of its aliases regardless of uniqueness.
func (l Listener) Notify(ctx context.Context, state *consul.State) error {
	hosts := buildHosts(state)

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

func buildHosts(state *consul.State) hosts {
	self := state.Nodes[state.Self]
	hosts := make(hosts)
	aliases := uniqueAliases(state)
	aliases[self.ID] = nodeAliases(state, self)
	for _, node := range state.Nodes {
		address := node.Address
		if self.ID == node.ID {
			address = LocalIP
		}

		hosts.addCanonical(address, node.Name)
		for alias := range aliases[node.ID] {
			hosts.add(address, alias)
		}
	}

	return hosts
}

func nodeAliases(state *consul.State, node consul.Node) lib.Set[string] {
	aliases := make(lib.Set[string])
	aliases.Add(domainAliases(GetDomainNames(node.Meta))...)
	for _, service := range node.Services {
		if service.ID != "" {
			aliases.Add(service.ID)
		}
		aliases.Add(getHTTPDomainAliases(state, service.Meta)...)
	}

	return aliases
}

func uniqueAliases(state *consul.State) map[string]lib.Set[string] {
	owners := make(map[string]lib.Set[string])
	for _, node := range state.Nodes {
		for _, domain := range domainAliases(GetDomainNames(node.Meta)) {
			addOwner(owners, domain, node.ID)
		}
		for _, service := range node.Services {
			addOwner(owners, service.ID, node.ID)
			for _, domain := range domainAliases(GetDomainNames(service.Meta)) {
				addOwner(owners, domain, node.ID)
			}
		}
	}

	aliases := make(map[string]lib.Set[string])
	for alias, nodes := range owners {
		if len(nodes) != 1 {
			continue
		}

		for node := range nodes {
			if aliases[node] == nil {
				aliases[node] = make(lib.Set[string])
			}
			aliases[node].Add(alias)
		}
	}

	return aliases
}

func getHTTPDomainAliases(state *consul.State, meta map[string]string) []string {
	return domainAliases(GetHTTPDomainNames(state, meta))
}

func domainAliases(domains []string) []string {
	aliases := make([]string, 0, len(domains))
	for _, domain := range domains {
		parsed, err := url.Parse("//" + domain)
		if err != nil {
			continue
		}

		host := parsed.Hostname()
		if host == "" || net.ParseIP(host) != nil {
			continue
		}
		aliases = append(aliases, host)
	}

	return aliases
}

func addOwner(owners map[string]lib.Set[string], alias string, node string) {
	if alias == "" {
		return
	}

	if owners[alias] == nil {
		owners[alias] = make(lib.Set[string])
	}
	owners[alias].Add(node)
}
