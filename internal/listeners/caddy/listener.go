package caddy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

var (
	lineStart = regexp.MustCompile(`(?m)^`)
)

type Config struct {
	KV      string `yaml:"kv"`
	Service *File  `yaml:"service,omitempty"`
	Node    *File  `yaml:"node,omitempty"`
	Exec    string `yaml:"exec"`
	Auth    string `yaml:"auth,omitempty"`
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

	var changedService bool
	if l.cfg.Service != nil {
		changedService, err = l.writeService(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write Service")
		}
	}

	var changedNode bool
	if l.cfg.Node != nil {
		changedNode, err = l.writeNode(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write path")
		}
	}

	if changedService || changedNode {
		err := exec.CommandContext(ctx, "sh", "-c", l.cfg.Exec).Run()
		if err != nil {
			slog.Error("failed to exec", "error", err)
		}
	}

	return nil
}

func (l *Listener) writeNode(
	state *consul.State,
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	return l.cfg.Node.Write(func(file io.Writer) error {
		domains := make(map[string][]Instance)
		for _, id := range slices.Sorted(maps.Keys(services)) {
			for _, instance := range services[id] {
				if _, ok := definitions[id]; !ok {
					continue
				}

				if !state.InGroup(instance.Service.Meta, PublishPathKey, state.Self) {
					continue
				}

				domain, ok := GetDomainName(instance.Node.Meta)
				if !ok {
					domain = "http://" + instance.Node.Name
				}

				domains[domain] = append(domains[domain], instance)
			}
		}

		for i, domain := range slices.Sorted(maps.Keys(domains)) {
			if i > 0 {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(file, "%s {", domain); err != nil {
				return err
			}

			for _, instance := range domains[domain] {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}

				id := instance.Service.ID
				tmpl, err := l.tmpl(state, definitions, instance)
				if err != nil {
					return err
				}

				if err := tmpl.Execute(file, instance); err != nil {
					return errors.Wrapf(err, "execute template for %s", id)
				}

				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(file, "}\n"); err != nil {
				return err
			}
		}

		return nil
	})
}

func (l *Listener) writeService(
	state *consul.State,
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	return l.cfg.Service.Write(func(file io.Writer) error {
		for i, id := range slices.Sorted(maps.Keys(definitions)) {
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

			tmpl, err := l.tmpl(state, definitions, instances[0])
			if err != nil {
				return err
			}

			domain, _ := GetDomainName(instances[0].Service.Meta)

			if i > 0 {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(file, "%s {\n", domain); err != nil {
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

func (l *Listener) auth(state *consul.State, instance Instance, indent int) string {
	if l.cfg.Auth == "" {
		return ""
	}

	for _, service := range instance.Node.Services {
		if service.ID == l.cfg.Auth {
			address := GetLocalAddress(state.Nodes[state.Self], service)
			text := fmt.Sprintf(`forward_auth %s:%d { 
	uri /api/authz/forward-auth
	copy_headers Remote-User Remote-Groups Remote-Email Remote-Name
}`, address, service.Port)

			pad := strings.Repeat(" ", indent)
			return strings.Replace(text, "\n", "\n"+pad, -1) + "\n"
		}
	}

	return ""
}

func (l *Listener) tmpl(state *consul.State, definitions map[string]consul.Value, instance Instance) (*template.Template, error) {
	id := instance.Service.ID
	funcs := template.FuncMap{
		"ForwardAuth": func(indent int) string { return l.auth(state, instance, indent) },
	}

	definition := strings.Trim(string(definitions[id]), " \n\t\v")
	definition = lineStart.ReplaceAllString(definition, "    ")
	tmpl, err := template.New(id).Delims("[[", "]]").Funcs(funcs).Parse(definition)
	if err != nil {
		return nil, errors.Wrapf(err, "parse template for %s", id)
	}

	return tmpl, nil
}

type Instance struct {
	Node    consul.Node
	Service consul.Service
}
