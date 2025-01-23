package hosts

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"

	"github.com/pkg/errors"
)

type Config struct {
	File string `yaml:"file" default:"/etc/hosts" doc:"Hosts file path"`
}

type Target struct {
	path  string
	names map[string]map[string]bool
}

func New(cfg Config) Target {
	return Target{
		path:  cfg.File,
		names: make(map[string]map[string]bool),
	}
}

func (t Target) add(ctx context.Context, address string, values ...string) {
	names := t.names[address]
	if names == nil {
		names = make(map[string]bool)
		t.names[address] = names
	}

	ctx = log.With(ctx, "address", address)
	for _, value := range values {
		names[value] = true
		log.Debug(log.With(ctx, "name", value), "updated host")
	}
}

func (t Target) Node(ctx context.Context, domain string, local *api.Node, node *api.Node) error {
	if local.ID != node.ID {
		t.add(ctx, node.Address, node.Name)
		return nil
	}

	t.add(ctx, api.Localhost, node.Name, api.Subdomain(node.Name, domain))

	return nil
}

func (t Target) Service(ctx context.Context, domain string, local, node *api.Node, service *api.Service) error {
	if service.Domain == "" {
		log.Debug(ctx, "service domain is empty")
		return nil
	}

	address := service.Address
	switch {
	case local.ID == node.ID:
		address = api.Localhost
		log.Debug(ctx, "using localhost address")
	case local.Visibility == node.Visibility:
		log.Debug(ctx, "local visibility matches node visibility")
		return nil
	case local.Visibility > service.Visibility:
		log.Debug(ctx, "local visibility is higher than service visibility")
		return nil
	case local.Region != node.Region:
		log.Debug(ctx, "local region does not match node region")
		return nil
	}

	t.add(ctx, address, api.Subdomain(service.Domain, domain))

	return nil
}

func (t Target) Commit(ctx context.Context, domain api.Domain) error {
	file, err := os.CreateTemp(filepath.Dir(t.path), ".consul-publish-hosts-")
	if err != nil {
		return errors.Wrap(err, "create temp file")
	}

	defer os.RemoveAll(file.Name())

	ctx = log.With(ctx, "tmp", file.Name())
	log.Debug(ctx, "created temp file")

	addresses := slices.Collect(maps.Keys(t.names))
	sort.Strings(addresses)
	for _, address := range addresses {
		names := slices.Collect(maps.Keys(t.names[address]))
		sort.Strings(names)
		if _, err := file.WriteString(address + " " + strings.Join(names, " ") + "\n"); err != nil {
			return errors.Wrap(err, "write to temp file")
		}
	}

	if err := file.Close(); err != nil {
		return errors.Wrap(err, "close temp file")
	}

	log.Info(log.With(ctx, "entries", len(t.names)), "written temp file")
	ctx = log.With(ctx, "path", t.path)

	source, err := os.Stat(file.Name())
	if err != nil {
		return errors.Wrap(err, "stat temp file")
	}

	target, err := os.Stat(t.path)
	switch {
	case os.IsNotExist(err):
		break
	case err != nil:
		return errors.Wrap(err, "stat target file")
	default:
		if os.SameFile(source, target) {
			log.Info(ctx, "no changes")
			return nil
		}
	}

	if err := os.Rename(file.Name(), t.path); err != nil {
		return errors.Wrap(err, "rename temp file")
	}

	log.Info(ctx, "updated hosts")
	return nil
}
