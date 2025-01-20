package porkbun

import (
	"context"
	"strings"

	"github.com/jfk9w/consul-resource-sync/internal/api"
	"github.com/jfk9w/consul-resource-sync/internal/log"

	"github.com/pkg/errors"
)

const RecordType = "A"

type Credentials struct {
	Key    string `json:"apikey"`
	Secret string `json:"secretapikey"`
}

type Config struct {
	Credentials Credentials `yaml:"-"`
	Dry         bool        `yaml:"dry,omitempty"`
}

type Target struct {
	client *Client
	dry    bool
	names  map[string]bool
}

func New(cfg Config) *Target {
	return &Target{
		client: NewClient(cfg.Credentials),
		dry:    cfg.Dry,
		names:  make(map[string]bool),
	}
}

func (t *Target) Node(ctx context.Context, domain string, local, node *api.Node) error {
	if local.ID != node.ID || node.Visibility < api.Public {
		return nil
	}

	subdomain := node.Name + "." + domain
	t.names[subdomain] = true

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

	t.names[service.Domain] = true
	return nil
}

func (t *Target) Sync(ctx context.Context, domain api.Domain) error {
	ping, err := t.client.Ping(ctx)
	if err != nil {
		return errors.Wrap(err, "ping")
	}

	ctx = log.With(ctx, "address", ping.YourIP)

	records := make(map[string]int64)
	for _, domain := range []string{domain.Node, domain.Service} {
		in := RetrieveRecordsIn{
			Domain: domain,
			Type:   RecordType,
		}

		resp, err := t.client.RetrieveRecords(ctx, in)
		if err != nil {
			return errors.Wrapf(err, "retrieve records for %s", domain)
		}

		for _, record := range resp.Records {
			if record.Content == ping.YourIP {
				records[join(record.Name, domain)] = record.ID
			}
		}
	}

	for subdomain := range t.names {
		if _, ok := records[subdomain]; ok {
			continue
		}

		name, domain := split(subdomain)
		in := UpdateRecordIn{
			Domain:  domain,
			Name:    name,
			Type:    RecordType,
			Content: ping.YourIP,
		}

		if t.dry {
			log.Info(ctx, "adding record", "domain", subdomain)
			continue
		}

		_, err := t.client.UpdateRecord(ctx, in)
		if err != nil {
			return errors.Wrapf(err, "create record for %s", join(name, domain))
		}
	}

	for record, id := range records {
		if _, ok := t.names[record]; ok {
			continue
		}

		_, domain := split(record)
		in := DeleteRecordIn{
			Domain: domain,
			ID:     id,
		}

		if t.dry {
			log.Info(ctx, "removing record", "domain", record)
			continue
		}

		if _, err := t.client.DeleteRecord(ctx, in); err != nil {
			return errors.Wrapf(err, "delete record for %s", record)
		}
	}

	return nil
}

func join(sub, domain string) string {
	return sub + "." + domain
}

func split(subdomain string) (string, string) {
	dot := strings.IndexRune(subdomain, '.')
	if dot > 0 {
		return subdomain[:dot], subdomain[dot+1:]
	}

	return "", subdomain
}
