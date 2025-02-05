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
	Path *File  `yaml:"path,omitempty"`
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

	var changedHTTP bool
	if l.cfg.HTTP != nil {
		changedHTTP, err = l.writeHTTP(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write HTTP")
		}
	}

	var changedPath bool
	if l.cfg.Path != nil {
		changedPath, err = l.writePath(state, services)
		if err != nil {
			return errors.Wrap(err, "write path")
		}
	}

	if changedHTTP || changedPath {
		err := exec.CommandContext(ctx, "sh", "-c", l.cfg.Exec).Run()
		if err != nil {
			return errors.Wrap(err, "exec")
		}
	}

	return nil
}

func (l *Listener) writePath(
	state *consul.State,
	services map[string][]Instance,
) (bool, error) {
	return l.cfg.Path.Write(func(file io.Writer) error {
		var instances []Instance
		for _, all := range services {
			for _, instance := range all {
				if !state.InGroup(instance.Service.Meta, PublishPathKey, state.Self) {
					continue
				}

				instances = append(instances, instance)
			}
		}

		sort.Slice(instances, func(i, j int) bool { return instances[i].Service.Name < instances[j].Service.Name })

		for _, instance := range instances {
			name, port := instance.Service.Name, instance.Service.Port
			if _, err := fmt.Fprintf(file,
				"\nredir /%s /%s/\nhandle /%s/* {\n    reverse_proxy 127.0.0.1:%d\n}\n",
				name, name, name, port,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (l *Listener) writeHTTP(
	state *consul.State,
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	return l.cfg.HTTP.Write(func(file io.Writer) error {
		for _, name := range slices.Sorted(maps.Keys(definitions)) {
			var instances []Instance
			for _, instance := range services[name] {
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
