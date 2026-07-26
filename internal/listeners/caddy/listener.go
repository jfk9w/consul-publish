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

var lineStart = regexp.MustCompile(`(?m)^`)

type Config struct {
	KV      string `yaml:"kv"`
	Service *File  `yaml:"service,omitempty"`
	Node    *File  `yaml:"node,omitempty"`
	Exec    string `yaml:"exec"`
	Auth    string `yaml:"auth,omitempty"`
	Common  string `yaml:"common,omitempty" doc:"Common Caddyfile directives added to every generated site block"`
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
	log := slog.With("listener", "caddy", "self", state.Self)
	log.Debug("processing state",
		"nodes", len(state.Nodes),
		"kv", l.cfg.KV,
		"service_output", l.cfg.Service != nil,
		"node_output", l.cfg.Node != nil,
		"auth_service", l.cfg.Auth,
	)

	definitions, ok := state.KV.Get(l.cfg.KV).(consul.Folder)
	if !ok {
		log.Error("caddy definitions are not a folder", "value_type", fmt.Sprintf("%T", state.KV.Get(l.cfg.KV)))
		return errors.Errorf("%s is not a folder", l.cfg.KV)
	}

	self := state.Nodes[state.Self]
	services := make(map[string][]Instance)
	for _, node := range state.Nodes {
		log.Debug("discovered node services", "node", node.Name, "address", node.Address, "services", serviceIDs(node.Services))
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
	log.Debug("prepared caddy inputs", "definitions", len(definitions), "service_ids", slices.Sorted(maps.Keys(services)))

	var changedService bool
	if l.cfg.Service != nil {
		log.Debug("rendering service caddyfile", "path", l.cfg.Service.Path)
		changedService, err = l.writeService(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write Service")
		}
		log.Debug("rendered service caddyfile", "path", l.cfg.Service.Path, "changed", changedService)
	}

	var changedNode bool
	if l.cfg.Node != nil {
		log.Debug("rendering node caddyfile", "path", l.cfg.Node.Path)
		changedNode, err = l.writeNode(state, services, maps.Collect(definitions.Values()))
		if err != nil {
			return errors.Wrap(err, "write path")
		}
		log.Debug("rendered node caddyfile", "path", l.cfg.Node.Path, "changed", changedNode)
	}

	if changedService || changedNode {
		log.Info("caddy configuration changed, executing reload", "command", l.cfg.Exec)
		err := exec.CommandContext(ctx, "sh", "-c", l.cfg.Exec).Run()
		if err != nil {
			log.Error("failed to reload caddy", "command", l.cfg.Exec, "error", err)
		} else {
			log.Info("reloaded caddy")
		}
	} else {
		log.Debug("caddy configuration unchanged, skipping reload")
	}

	return nil
}

func (l *Listener) writeNode(
	state *consul.State,
	services map[string][]Instance,
	definitions map[string]consul.Value,
) (bool, error) {
	log := slog.With("listener", "caddy", "output", "node", "self", state.Self)
	return l.cfg.Node.Write(func(file io.Writer) error {
		domains := make(map[string][]Instance)
		for _, id := range slices.Sorted(maps.Keys(services)) {
			for _, instance := range services[id] {
				if _, ok := definitions[id]; !ok {
					log.Debug("skipping service without caddy definition", "service", id, "node", instance.Node.Name)
					continue
				}

				if !state.InGroup(instance.Service.Meta, PublishPathKey, state.Self) {
					log.Debug("skipping service not published to local node",
						"service", id,
						"node", instance.Node.Name,
						"publish_path", instance.Service.Meta[PublishPathKey],
					)
					continue
				}

				domain, ok := GetDomainName(instance.Node.Meta)
				if !ok {
					domain = "http://" + instance.Node.Name
					log.Debug("using fallback node domain", "node", instance.Node.Name, "domain", domain)
				}

				domains[domain] = append(domains[domain], instance)
				log.Debug("selected service for node caddyfile", "service", id, "node", instance.Node.Name, "domain", domain)
			}
		}
		log.Debug("grouped node caddy entries", "domains", slices.Sorted(maps.Keys(domains)))

		for i, domain := range slices.Sorted(maps.Keys(domains)) {
			if i > 0 {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(file, "%s {\n", domain); err != nil {
				return err
			}
			if err := l.writeCommon(file); err != nil {
				return errors.Wrapf(err, "write common block for %s", domain)
			}

			for _, instance := range domains[domain] {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}

				id := instance.Service.ID
				log.Debug("rendering node service template", "service", id, "node", instance.Node.Name, "domain", domain)
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
	log := slog.With("listener", "caddy", "output", "service", "self", state.Self)
	return l.cfg.Service.Write(func(file io.Writer) error {
		type entry struct {
			id        string
			instances []Instance
		}

		// Group service entries by domain, preserving sorted order of IDs.
		domains := make(map[string][]entry)
		for _, id := range slices.Sorted(maps.Keys(definitions)) {
			var instances []Instance
			for _, instance := range services[id] {
				if len(GetHTTPDomainNames(state, instance.Service.Meta)) == 0 {
					log.Debug("skipping service instance without local HTTP publication",
						"service", id,
						"node", instance.Node.Name,
						"domain_name", instance.Service.Meta[DomainNameKey],
						"publish_http", instance.Service.Meta[PublishHTTPKey],
					)
					continue
				}

				instances = append(instances, instance)
				log.Debug("selected service instance", "service", id, "node", instance.Node.Name, "address", instance.Service.Address)
			}

			if len(instances) == 0 {
				log.Debug("skipping caddy definition without publishable instances", "service", id)
				continue
			}

			domain, _ := GetDomainName(instances[0].Service.Meta)
			domains[domain] = append(domains[domain], entry{id, instances})
			log.Debug("grouped service caddy entry",
				"service", id,
				"domain", domain,
				"instances", len(instances),
				"template_node", instances[0].Node.Name,
			)
		}
		log.Debug("grouped service caddy entries", "domains", slices.Sorted(maps.Keys(domains)))

		for i, domain := range slices.Sorted(maps.Keys(domains)) {
			if i > 0 {
				if _, err := fmt.Fprintf(file, "\n"); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(file, "%s {\n", domain); err != nil {
				return errors.Wrapf(err, "write start for %s", domain)
			}
			if err := l.writeCommon(file); err != nil {
				return errors.Wrapf(err, "write common block for %s", domain)
			}

			for _, e := range domains[domain] {
				log.Debug("rendering service template",
					"service", e.id,
					"domain", domain,
					"instances", len(e.instances),
					"template_node", e.instances[0].Node.Name,
				)
				tmpl, err := l.tmpl(state, definitions, e.instances[0])
				if err != nil {
					return err
				}

				if err := tmpl.Execute(file, e.instances); err != nil {
					return errors.Wrapf(err, "execute template for %s", e.id)
				}
			}

			if _, err := fmt.Fprintln(file, "\n}"); err != nil {
				return errors.Wrapf(err, "write end for %s", domain)
			}
		}

		return nil
	})
}

func (l *Listener) writeCommon(file io.Writer) error {
	common := strings.TrimSpace(l.cfg.Common)
	if common == "" {
		slog.Debug("skipping empty caddy common block", "listener", "caddy")
		return nil
	}

	slog.Debug("writing caddy common block", "listener", "caddy", "lines", strings.Count(common, "\n")+1)
	common = lineStart.ReplaceAllString(common, "    ")
	_, err := fmt.Fprintln(file, common)
	return err
}

func (l *Listener) auth(state *consul.State, instance Instance, indent int) string {
	log := slog.With(
		"listener", "caddy",
		"service", instance.Service.ID,
		"template_node", instance.Node.Name,
		"self", state.Self,
		"auth_service", l.cfg.Auth,
	)
	if l.cfg.Auth == "" {
		log.Debug("forward auth disabled")
		return ""
	}

	log.Debug("looking up forward auth service", "node_services", serviceIDs(instance.Node.Services))
	for _, service := range instance.Node.Services {
		if service.ID == l.cfg.Auth {
			address := GetLocalAddress(state.Nodes[state.Self], service)
			log.Debug("rendering forward auth",
				"auth_node", instance.Node.Name,
				"address", address,
				"port", service.Port,
				"indent", indent,
			)
			text := fmt.Sprintf(`forward_auth %s:%d { 
	uri /api/authz/forward-auth
	copy_headers Remote-User Remote-Groups Remote-Email Remote-Name
}`, address, service.Port)

			pad := strings.Repeat(" ", indent)
			return strings.Replace(text, "\n", "\n"+pad, -1) + "\n"
		}
	}

	log.Warn("forward auth service not found on template node", "node_services", serviceIDs(instance.Node.Services))
	return ""
}

func (l *Listener) tmpl(state *consul.State, definitions map[string]consul.Value, instance Instance) (*template.Template, error) {
	id := instance.Service.ID
	slog.Debug("parsing caddy template",
		"listener", "caddy",
		"service", id,
		"template_node", instance.Node.Name,
		"definition_bytes", len(definitions[id]),
	)
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

func serviceIDs(services []consul.Service) []string {
	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, service.ID)
	}

	slices.Sort(ids)
	return ids
}
