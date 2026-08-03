// Package homepage generates Homepage's services.yaml from Consul services and KV templates.
package homepage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/jfk9w/consul-publish/internal/consul"
	. "github.com/jfk9w/consul-publish/internal/listeners"
)

type Config struct {
	KV       string `yaml:"kv" doc:"Consul KV prefix that holds Homepage service templates"`
	Exec     string `yaml:"exec" doc:"Command to run after the Homepage configuration changes"`
	Services File   `yaml:"services" doc:"Homepage services.yaml output file settings"`
}

type Listener struct {
	cfg Config
}

func New(cfg Config) *Listener {
	return &Listener{cfg: cfg}
}

func (l *Listener) KV() []string {
	return []string{l.cfg.KV}
}

func (l *Listener) Notify(ctx context.Context, state *consul.State) error {
	definitions, ok := state.KV.Get(l.cfg.KV).(consul.Folder)
	if !ok {
		return errors.Errorf("%s is not a folder", l.cfg.KV)
	}

	changed, err := l.cfg.Services.Write(func(file io.Writer) error {
		return l.write(state, file, maps.Collect(definitions.Values()))
	})
	if err != nil {
		return errors.Wrap(err, "write Homepage services configuration")
	}

	slog.Debug("rendered Homepage configuration", "listener", "homepage", "self", state.Self, "changed", changed)
	if changed && l.cfg.Exec != "" {
		slog.Info("Homepage configuration changed, reloading", "listener", "homepage")
		if err := exec.CommandContext(ctx, "sh", "-c", l.cfg.Exec).Run(); err != nil {
			slog.Error("failed to reload Homepage", "listener", "homepage", "error", err)
			return errors.Wrap(err, "reload Homepage")
		}
		slog.Info("Homepage reloaded", "listener", "homepage")
	}

	return nil
}

type Instance struct {
	Node    consul.Node
	Service consul.Service
}

type placement struct {
	name      string
	serviceID string
	instances []Instance
}

func (l *Listener) write(state *consul.State, file io.Writer, definitions map[string]consul.Value) error {
	self := state.Nodes[state.Self]
	services := make(map[string][]Instance)
	for _, node := range state.Nodes {
		for _, service := range node.Services {
			if _, ok := definitions[service.ID]; !ok {
				continue
			}
			if !state.InGroup(service.Meta, PublishHomepageKey, state.Self) {
				continue
			}

			service.Address = GetLocalAddress(self, service)
			services[service.ID] = append(services[service.ID], Instance{Node: node, Service: service})
		}
	}

	for _, instances := range services {
		sort.Slice(instances, func(i, j int) bool {
			if instances[i].Service.Address == instances[j].Service.Address {
				return instances[i].Node.Name < instances[j].Node.Name
			}
			return instances[i].Service.Address < instances[j].Service.Address
		})
	}

	groups := make(map[string][]placement)
	seen := make(map[string]struct{})
	for _, id := range slices.Sorted(maps.Keys(services)) {
		instances := services[id]
		for _, instance := range instances {
			for _, value := range strings.Fields(instance.Service.Meta[HomepagePathKey]) {
				group, name, ok := strings.Cut(value, "/")
				if !ok || group == "" || name == "" {
					return errors.Errorf("invalid %s value %q for service %s: expected group/service-name", HomepagePathKey, value, id)
				}

				key := group + "\x00" + name + "\x00" + id
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				groups[group] = append(groups[group], placement{name: name, serviceID: id, instances: instances})
			}
		}
	}

	for _, entries := range groups {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].name == entries[j].name {
				return entries[i].serviceID < entries[j].serviceID
			}
			return entries[i].name < entries[j].name
		})
	}

	for groupIndex, group := range slices.Sorted(maps.Keys(groups)) {
		if groupIndex > 0 {
			if _, err := fmt.Fprintln(file); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(file, "- %s:\n", yamlScalar(group)); err != nil {
			return err
		}

		for _, entry := range groups[group] {
			if _, err := fmt.Fprintf(file, "    - %s:\n", yamlScalar(entry.name)); err != nil {
				return err
			}

			tmpl, err := template.New(entry.serviceID).Delims("[[", "]]").Parse(strings.TrimSpace(string(definitions[entry.serviceID])))
			if err != nil {
				return errors.Wrapf(err, "parse template for %s", entry.serviceID)
			}

			var rendered strings.Builder
			if err := tmpl.Execute(&rendered, entry.instances); err != nil {
				return errors.Wrapf(err, "execute template for %s", entry.serviceID)
			}

			content := strings.TrimSpace(rendered.String())
			if content != "" {
				content = strings.ReplaceAll(content, "\n", "\n        ")
				if _, err := fmt.Fprintf(file, "        %s\n", content); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func yamlScalar(value string) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		panic(err) // strings are always representable as YAML scalars
	}
	return strings.TrimSpace(string(data))
}
