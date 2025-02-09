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
	HTTP *File  `yaml:"http,omitempty"`
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

func (l *Listener) Notify(ctx context.Context, state *consul.State) (err error) {
	definitions, ok := state.KV.Get(l.cfg.KV).(consul.Folder)
	if !ok {
		return errors.Errorf("%s is not a folder", l.cfg.KV)
	}

	self := state.Nodes[state.Self]
	services := make(map[string][]Instance)
	for _, node := range state.Nodes {
		for _, service := range node.Services {
			service.Address = GetLocalAddress(self, service)
			services[service.ID] = append(services[service.ID], Instance{
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

	var changedHTTP bool
	if l.cfg.HTTP != nil {
		changedHTTP, err = l.writeHTTP(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write HTTP")
		}
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
	state *consul.State,
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	return l.cfg.HTTP.Write(func(file io.Writer) error {
		for _, id := range slices.Sorted(maps.Keys(definitions)) {
			var instances []Instance
			for _, instance := range services[id] {
				if _, ok := GetDomainName(instance.Service.Meta); !ok {
					continue
				}

				if !state.InGroup(instance.Service.Meta, PublishHTTPKey, state.Self) {
					continue
				}

				instances = append(instances, instance)
			}

			if len(instances) == 0 {
				continue
			}

			definition := strings.Trim(string(definitions[id]), " \n\t\v")
			definition = lineStart.ReplaceAllString(definition, "    ")

			tmpl, err := template.New(id).Delims("[[", "]]").Parse(definition)
			if err != nil {
				return errors.Wrapf(err, "parse template for %s", id)
			}

			domain, _ := GetDomainName(instances[0].Service.Meta)

			if _, err := fmt.Fprintf(file, "\n%s {\n", domain); err != nil {
				return errors.Wrapf(err, "write start template for %s", id)
			}

			if err := tmpl.Execute(file, instances); err != nil {
				return errors.Wrapf(err, "execute template for %s", id)
			}

			if _, err := fmt.Fprintln(file, "\n}"); err != nil {
				return errors.Wrapf(err, "write end template for %s", id)
			}
		}

		return nil
	})
}

type Instance struct {
	Node    consul.Node
	Service consul.Service
}
