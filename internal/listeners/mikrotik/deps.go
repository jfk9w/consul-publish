package mikrotik

import mtkapi "github.com/jfk9w/consul-publish/internal/mikrotik"

//go:generate mockgen -destination mocks.go -package mikrotik -typed . DNSClient

// DNSClient is the interface the listener uses to manage MikroTik DNS records.
// mtkapi.Client satisfies this interface.
type DNSClient interface {
	CreateDNSRecord(record mtkapi.DNSRecord) (mtkapi.DNSRecord, error)
	UpdateDNSRecord(record mtkapi.DNSRecord) (mtkapi.DNSRecord, error)
	FindDNSRecords(filter mtkapi.DNSRecord) ([]mtkapi.DNSRecord, error)
	DeleteDNSRecord(id string) error
}
