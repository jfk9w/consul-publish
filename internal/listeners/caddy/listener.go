package caddy

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/pkg/errors"

	"github.com/jfk9w/consul-publish/internal/consul"
	. "github.com/jfk9w/consul-publish/internal/listeners"
)

var lineStart = regexp.MustCompile(`(?m)^`)

type Config struct {
	KV   string `yaml:"kv"`
	HTTP File   `yaml:"http"`
	Exec string `yaml:"exec"`
}

type Listener struct {
	cfg Config
}

func New(cfg Config) *Listener {
	return &Listener{
		cfg: cfg,
	}
}

func (l *Listener) KV() []string {
	return []string{
		l.cfg.KV,
	}
}

func (l *Listener) Notify(ctx context.Context, state *consul.State) error {
	definitions, ok := state.KV.Get(l.cfg.KV).(consul.Folder)
	if !ok {
		return errors.Errorf("%s is not a folder", l.cfg.KV)
	}

	self := state.Nodes[state.Self]
	services := make(map[string][]Instance)
	for _, node := range state.Nodes {
		for _, service := range node.Services {
			if _, ok := GetDomainName(service.Meta); !ok {
				continue
			}

			if !state.InGroup(service.Meta, PublishHTTPKey, self.Name) {
				continue
			}

			service.Address = GetLocalAddress(self, service)

			services[service.Name] = append(services[service.Name], Instance{
				Node:    node,
				Service: service,
			})
		}
	}

	for _, instances := range services {
		sort.Slice(instances, func(i, j int) bool {
			return instances[i].Service.Address < instances[j].Service.Address
		})
	}

	changedHTTP, err := l.writeHTTP(services, maps.Collect(definitions.Values()))
	if err != nil {
		return errors.Wrap(err, "write HTTP")
	}

	if changedHTTP {
		err := exec.CommandContext(ctx, "sh", "-c", l.cfg.Exec).Run()
		if err != nil {
			return errors.Wrap(err, "exec")
		}
	}

	return nil
}

func (l *Listener) writeHTTP(
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	return l.cfg.HTTP.Write(func(file io.Writer) error {
		for _, name := range slices.Sorted(maps.Keys(definitions)) {
			instances, ok := services[name]
			if !ok {
				continue
			}

			definition := strings.Trim(string(definitions[name]), " \n\t\v")
			definition = lineStart.ReplaceAllString(definition, "    ")

			tmpl, err := template.New(name).Delims("[[", "]]").Parse(definition)
			if err != nil {
				return errors.Wrapf(err, "parse template for %s", name)
			}

			domain, _ := GetDomainName(instances[0].Service.Meta)

			if _, err := fmt.Fprintf(file, "\n%s {\n", domain); err != nil {
				return errors.Wrapf(err, "write start template for %s", name)
			}

			if err := tmpl.Execute(file, instances); err != nil {
				return errors.Wrapf(err, "execute template for %s", name)
			}

			if _, err := fmt.Fprintln(file, "\n}"); err != nil {
				return errors.Wrapf(err, "write end template for %s", name)
			}
		}

		return nil
	})
}

type Instance struct {
	Node    consul.Node
	Service consul.Service
}
