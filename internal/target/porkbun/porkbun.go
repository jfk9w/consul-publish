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
	Credentials porkbun.Credentials `yaml:"credentials,omitempty"`
	Dry         bool                `yaml:"dry,omitempty"`
}

type Target struct {
	client  *porkbun.Client
	dry     bool
	records map[string]bool
}

func New(cfg Config) *Target {
	return &Target{
		client:  porkbun.NewClient(cfg.Credentials),
		dry:     cfg.Dry,
		records: make(map[string]bool),
	}
}

func (t *Target) Node(ctx context.Context, domain string, local, node *api.Node) error {
	if local.ID != node.ID || node.Visibility < api.Public {
		return nil
	}

	subdomain := api.Subdomain(node.Name, domain)
	t.records[subdomain] = true

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
	t.records[subdomain] = true

	return nil
}

func (t *Target) Commit(ctx context.Context, domain api.Domain) error {
	ping, err := t.client.Ping(ctx)
	if err != nil {
		return errors.Wrap(err, "ping")
	}

	ping.YourIP = "93.183.105.51"
	ctx = log.With(ctx, "address", ping.YourIP)

	records := make(map[string]string)
	for _, domain := range []string{domain.Node, domain.Service} {
		in := porkbun.RetrieveRecordsIn{
			Domain: domain,
			// Type:   RecordType,
		}

		resp, err := t.client.RetrieveRecords(ctx, in)
		if err != nil {
			return errors.Wrapf(err, "retrieve records for %s", domain)
		}

		for _, record := range resp.Records {
			if record.Type != RecordType || record.Content != ping.YourIP {
				continue
			}

			records[record.Name] = record.ID
		}
	}

	for record := range t.records {
		if _, ok := records[record]; ok {
			continue
		}

		name, domain := api.NameDomain(record)
		in := porkbun.UpdateRecordIn{
			Domain:  domain,
			Name:    name,
			Type:    RecordType,
			Content: ping.YourIP,
		}

		if !t.dry {
			if _, err := t.client.UpdateRecord(ctx, in); err != nil {
				return errors.Wrapf(err, "create record for %s", record)
			}
		}

		log.Info(ctx, "added record", "subdomain", record)
	}

	for record, id := range records {
		if _, ok := t.records[record]; ok {
			continue
		}

		_, domain := api.NameDomain(record)
		in := porkbun.DeleteRecordIn{
			Domain: domain,
			ID:     id,
		}

		if !t.dry {
			if _, err := t.client.DeleteRecord(ctx, in); err != nil {
				return errors.Wrapf(err, "delete record for %s", record)
			}
		}

		log.Info(ctx, "removed record", "subdomain", record)
	}

	return nil
}
