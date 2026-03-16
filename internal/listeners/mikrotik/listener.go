package mikrotik

import (
	"context"
	"log/slog"

	"github.com/pkg/errors"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners"
	mtkapi "github.com/jfk9w/consul-publish/internal/mikrotik"
)

// ListenerConfig holds configuration for the MikroTik DNS listener.
type ListenerConfig struct {
	mtkapi.Config `yaml:",inline"`
	TTL           mtkapi.Duration `yaml:"ttl"     default:"5m"    doc:"DNS record TTL"`
	Comment       string          `yaml:"comment" default:"consul" doc:"Comment used to tag records managed by this listener; only records with this comment are reconciled"`
}

type Listener struct {
	cfg    ListenerConfig
	client DNSClient
}

// NewListener creates a Listener backed by a real MikroTik client.
func NewListener(cfg ListenerConfig) *Listener {
	return NewListenerWithClient(cfg, mtkapi.New(cfg.Config))
}

// NewListenerWithClient creates a Listener with the provided DNSClient.
// Intended for testing.
func NewListenerWithClient(cfg ListenerConfig, client DNSClient) *Listener {
	return &Listener{cfg: cfg, client: client}
}

func (l *Listener) KV() []string {
	return nil
}

// Notify reconciles MikroTik static DNS records with the current Consul state.
// For every service that has a "domain-name" metadata key, a DNS record pointing
// to the current node's IP is created or updated. Records with the configured
// comment that are no longer present in Consul are deleted.
func (l *Listener) Notify(ctx context.Context, state *consul.State) error {
	selfNode := state.Nodes[state.Self]

	desired := make(map[string]string)
	for _, node := range state.Nodes {
		for _, service := range node.Services {
			for _, domain := range listeners.GetDomainNames(service.Meta) {
				desired[domain] = selfNode.Address
			}
		}
	}

	existing, err := l.fetchExisting()
	if err != nil {
		return errors.Wrap(err, "fetch existing DNS records")
	}

	for domain, address := range desired {
		if err := l.reconcileDomain(domain, address, existing[domain]); err != nil {
			return errors.Wrapf(err, "reconcile domain %s", domain)
		}
	}

	for domain, records := range existing {
		if _, ok := desired[domain]; ok {
			continue
		}
		for _, r := range records {
			slog.Info("deleting DNS record", "domain", domain, "id", r.ID)
			if err := l.client.DeleteDNSRecord(r.ID); err != nil {
				return errors.Wrapf(err, "delete DNS record %s (id=%s)", domain, r.ID)
			}
		}
	}

	return nil
}

func (l *Listener) fetchExisting() (map[string][]mtkapi.DNSRecord, error) {
	records, err := l.client.FindDNSRecords(mtkapi.DNSRecord{Comment: l.cfg.Comment})
	if err != nil {
		return nil, err
	}
	m := make(map[string][]mtkapi.DNSRecord, len(records))
	for _, r := range records {
		m[r.Name] = append(m[r.Name], r)
	}
	return m, nil
}

// reconcileDomain brings the MikroTik records for a single domain name into the
// desired state: exactly one record pointing to address. If multiple records exist,
// duplicates are deleted; if the address is wrong, the record is updated in-place.
func (l *Listener) reconcileDomain(domain, address string, existing []mtkapi.DNSRecord) error {
	matchIdx := -1
	for i, r := range existing {
		if r.Address == address {
			matchIdx = i
			break
		}
	}

	keepIdx := matchIdx
	if keepIdx < 0 {
		keepIdx = 0
	}
	for i, r := range existing {
		if i == keepIdx {
			continue
		}
		slog.Info("deleting duplicate DNS record", "domain", domain, "id", r.ID)
		if err := l.client.DeleteDNSRecord(r.ID); err != nil {
			return errors.Wrapf(err, "delete duplicate record id=%s", r.ID)
		}
	}

	switch {
	case len(existing) == 0:
		slog.Info("creating DNS record", "domain", domain, "address", address)
		_, err := l.client.CreateDNSRecord(mtkapi.DNSRecord{
			Name:    domain,
			Address: address,
			TTL:     l.cfg.TTL,
			Comment: l.cfg.Comment,
		})
		return errors.Wrap(err, "create DNS record")

	case matchIdx < 0:
		kept := existing[keepIdx]
		slog.Info("updating DNS record", "domain", domain, "id", kept.ID, "old", kept.Address, "new", address)
		_, err := l.client.UpdateDNSRecord(mtkapi.DNSRecord{
			ID:      kept.ID,
			Name:    domain,
			Address: address,
			TTL:     l.cfg.TTL,
			Comment: l.cfg.Comment,
		})
		return errors.Wrap(err, "update DNS record")

	default:
		return nil
	}
}
