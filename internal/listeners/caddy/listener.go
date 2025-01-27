package caddy

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"text/template"

	"github.com/pkg/errors"

	"github.com/jfk9w/consul-publish/internal/consul"
	. "github.com/jfk9w/consul-publish/internal/listeners"
)

var lineStart = regexp.MustCompile(`(?m)^`)

type Config struct {
	KeyPrefix string `yaml:"keyPrefix"`
	Files     struct {
		HTTP string `yaml:"http"`
	} `yaml:"files"`
	Exec string `yaml:"exec"`
}

type Listener struct {
	cfg  Config
	exec []string
}

func New(cfg Config) (*Listener, error) {
	return &Listener{
		cfg:  cfg,
		exec: strings.Fields(cfg.Exec),
	}, nil
}

func (l *Listener) Keys() []string {
	return []string{
		l.cfg.KeyPrefix,
	}
}

func (l *Listener) Notify(ctx context.Context, state *consul.State) error {
	definitions, ok := state.KV.Get(l.cfg.KeyPrefix).(consul.Folder)
	if !ok {
		return errors.Errorf("%s is not a folder", l.cfg.KeyPrefix)
	}

	self := state.Nodes[state.Self]
	services := make(map[string][]Service)
	for _, node := range state.Nodes {
		for _, service := range node.Services {
			if _, ok := GetDomainName(service.Meta); !ok {
				continue
			}

			if !state.InGroup(service.Meta, PublishCaddyKey, self.Name) {
				continue
			}

			service.Address = GetLocalAddress(self, service)

			services[service.Name] = append(services[service.Name], Service{
				Node:    node,
				Service: service,
			})
		}
	}

	changedHTTP, err := l.writeHTTP(services, definitions)
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
	services map[string][]Service,
	definitions consul.Folder,
) (bool, error) {
	file := File{
		Path:  l.cfg.Files.HTTP,
		Mode:  0o640,
		User:  "root",
		Group: "caddy",
	}

	return file.Write(func(file io.Writer) error {
		for name, definition := range definitions.Values() {
			services, ok := services[name]
			if !ok {
				continue
			}

			definition := strings.Trim(string(definition), " \n\t\v")
			definition = lineStart.ReplaceAllString(definition, "    ")

			tmpl, err := template.New(name).Delims("[[", "]]").Parse(definition)
			if err != nil {
				return errors.Wrapf(err, "parse template for %s", name)
			}

			domain, _ := GetDomainName(services[0].Service.Meta)

			if _, err := fmt.Fprintf(file, "\n%s {\n", domain); err != nil {
				return errors.Wrapf(err, "write start template for %s", name)
			}

			if err := tmpl.Execute(file, services); err != nil {
				return errors.Wrapf(err, "execute template for %s", name)
			}

			if _, err := fmt.Fprintln(file, "\n}"); err != nil {
				return errors.Wrapf(err, "write end template for %s", name)
			}
		}

		return nil
	})
}

type Service struct {
	Node    consul.Node
	Service consul.Service
}
