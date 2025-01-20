package hosts

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/jfk9w/consul-resource-sync/internal/api"
	"github.com/jfk9w/consul-resource-sync/internal/log"

	"github.com/pkg/errors"
)

type Config struct {
	File string `yaml:"file" default:"/etc/hosts" doc:"Hosts file path"`
}

type HostNames []string

type Target struct {
	path  string
	hosts map[string]HostNames
}

func New(cfg Config) Target {
	return Target{
		path:  cfg.File,
		hosts: make(map[string]HostNames),
	}
}

func (t Target) Node(ctx context.Context, domain string, local *api.Node, node *api.Node) error {
	address := node.Address
	names := []string{node.Name}
	if local.ID == node.ID {
		address = api.Localhost
		if node.Visibility == api.Public {
			subdomain := node.Name + "." + domain
			names = append(names, subdomain)
		}
	}

	t.hosts[address] = append(t.hosts[address], names...)
	for _, name := range names {
		log.Info(log.With(ctx, "address", address, "name", name), "updated node")
	}

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

	ctx = log.With(ctx, "address", address)
	t.hosts[address] = append(t.hosts[address], service.Domain)
	log.Info(ctx, "updated service")

	return nil
}

func (t Target) Sync(ctx context.Context, domain api.Domain) error {
	file, err := os.CreateTemp("", "hosts")
	if err != nil {
		return errors.Wrap(err, "create temp file")
	}

	defer os.RemoveAll(file.Name())

	ctx = log.With(ctx, "tmp", file.Name())
	log.Debug(ctx, "created temp file")

	for address, names := range t.hosts {
		if _, err := file.WriteString(address + " " + strings.Join(names, " ") + "\n"); err != nil {
			return errors.Wrap(err, "write to temp file")
		}
	}

	if err := file.Close(); err != nil {
		return errors.Wrap(err, "close temp file")
	}

	log.Info(log.With(ctx, "entries", len(t.hosts)), "written temp file")
	ctx = log.With(ctx, "target", t.path)

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
		log.Warn(ctx, "failed to rename temp file, trying to copy", "error", err)
		if err := copyFile(file.Name(), t.path); err != nil {
			return errors.Wrap(err, "copy temp file")
		}
	}

	log.Info(ctx, "updated hosts")
	return nil
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return errors.Wrap(err, "open source file")
	}

	defer source.Close()

	target, err := os.Create(targetPath)
	if err != nil {
		return errors.Wrap(err, "create target file")
	}

	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return errors.Wrap(err, "copy file")
	}

	return nil
}
