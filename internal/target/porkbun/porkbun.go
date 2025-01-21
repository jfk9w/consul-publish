package porkbun

import (
	"context"

	"github.com/jfk9w/consul-publish/internal/api"
	"github.com/jfk9w/consul-publish/internal/log"
	"github.com/jfk9w/consul-publish/internal/porkbun"

	"github.com/pkg/errors"
)

const RecordType = "A"

type Config struct {
	Credentials porkbun.Credentials `yaml:"-"`
	Dry         bool                `yaml:"dry,omitempty"`
}

type Target struct {
	client     *porkbun.Client
	dry        bool
	subdomains map[string]bool
}

func New(cfg Config) *Target {
	return &Target{
		client:     porkbun.NewClient(cfg.Credentials),
		dry:        cfg.Dry,
		subdomains: make(map[string]bool),
	}
}

func (t *Target) Node(ctx context.Context, domain string, local, node *api.Node) error {
	if local.ID != node.ID || node.Visibility < api.Public {
		return nil
	}

	subdomain := api.Subdomain(node.Name, domain)
	t.subdomains[subdomain] = true

	return nil
}

func (t *Target) Service(ctx context.Context, domain string, local, node *api.Node, service *api.Service) error {
	switch {
	case service.Domain == "",
		local.Region != node.Region,
		local.Visibility < api.Public,
		service.Visibility < local.Visibility:
		return nil
	}

	subdomain := api.Subdomain(service.Domain, domain)
	t.subdomains[subdomain] = true

	return nil
}

func (t *Target) Commit(ctx context.Context, domain api.Domain) error {
	ping, err := t.client.Ping(ctx)
	if err != nil {
		return errors.Wrap(err, "ping")
	}

	ctx = log.With(ctx, "address", ping.YourIP)

	subdomains := make(map[string]int64)
	for _, domain := range []string{domain.Node, domain.Service} {
		in := porkbun.RetrieveRecordsIn{
			Domain: domain,
			Type:   RecordType,
		}

		resp, err := t.client.RetrieveRecords(ctx, in)
		if err != nil {
			return errors.Wrapf(err, "retrieve records for %s", domain)
		}

		for _, record := range resp.Records {
			if record.Content == ping.YourIP {
				subdomain := api.Subdomain(record.Name, domain)
				subdomains[subdomain] = record.ID
			}
		}
	}

	for subdomain := range t.subdomains {
		if _, ok := subdomains[subdomain]; ok {
			continue
		}

		name, domain := api.NameDomain(subdomain)
		in := porkbun.UpdateRecordIn{
			Domain:  domain,
			Name:    name,
			Type:    RecordType,
			Content: ping.YourIP,
		}

		log.Info(ctx, "adding record", "subdomain", subdomain)
		if t.dry {
			continue
		}

		_, err := t.client.UpdateRecord(ctx, in)
		if err != nil {
			return errors.Wrapf(err, "create record for %s", subdomain)
		}
	}

	for subdomain, id := range subdomains {
		if _, ok := t.subdomains[subdomain]; ok {
			continue
		}

		_, domain := api.NameDomain(subdomain)
		in := porkbun.DeleteRecordIn{
			Domain: domain,
			ID:     id,
		}

		log.Info(ctx, "removing record", "subdomain", subdomain)
		if t.dry {
			continue
		}

		if _, err := t.client.DeleteRecord(ctx, in); err != nil {
			return errors.Wrapf(err, "delete record for %s", subdomain)
		}
	}

	return nil
}
