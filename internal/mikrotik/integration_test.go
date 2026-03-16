//go:build integration

package mikrotik_test

import (
	"os"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/jfk9w/consul-publish/internal/mikrotik"
)

const managedByComment = "consul-publish"

func newIntegrationClient(t *testing.T) *mikrotik.Client {
	t.Helper()
	host := os.Getenv("MIKROTIK_HOST")
	user := os.Getenv("MIKROTIK_USER")
	password := os.Getenv("MIKROTIK_PASSWORD")
	if host == "" || user == "" || password == "" {
		t.Skip("MIKROTIK_HOST, MIKROTIK_USER, MIKROTIK_PASSWORD not set")
	}

	return mikrotik.New(mikrotik.Config{
		Host:     host,
		User:     user,
		Password: password,
	})
}

func TestCreateDNSRecord(t *testing.T) {
	c := newIntegrationClient(t)

	name := os.Getenv("MIKROTIK_TEST_NAME")
	address := os.Getenv("MIKROTIK_TEST_ADDRESS")
	if name == "" || address == "" {
		t.Skip("MIKROTIK_TEST_NAME, MIKROTIK_TEST_ADDRESS not set")
	}

	record, err := c.CreateDNSRecord(mikrotik.DNSRecord{
		Name:    name,
		Address: address,
		TTL:     mikrotik.Duration(5 * time.Minute),
		Comment: managedByComment,
	})
	if err != nil {
		t.Fatalf("create dns record: %v", err)
	}
	if record.ID == "" {
		t.Fatal("expected .id in response")
	}
	t.Logf("created record: id=%s name=%s address=%s comment=%s", record.ID, record.Name, record.Address, record.Comment)
}

func TestDeleteDNSRecord(t *testing.T) {
	c := newIntegrationClient(t)

	name := os.Getenv("MIKROTIK_TEST_NAME")
	address := os.Getenv("MIKROTIK_TEST_ADDRESS")
	if name == "" || address == "" {
		t.Skip("MIKROTIK_TEST_NAME, MIKROTIK_TEST_ADDRESS not set")
	}

	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Name: name})
	if err != nil {
		t.Fatalf("find dns records: %v", err)
	}

	spew.Dump(records)

	if len(records) == 0 {
		t.Log("record not found, creating")
		record, err := c.CreateDNSRecord(mikrotik.DNSRecord{
			Name:    name,
			Address: address,
			TTL:     mikrotik.Duration(5 * time.Minute),
			Comment: managedByComment,
		})
		if err != nil {
			t.Fatalf("create dns record: %v", err)
		}
		records = []mikrotik.DNSRecord{record}
	}

	id := records[0].ID
	t.Logf("deleting record: id=%s name=%s", id, records[0].Name)

	if err := c.DeleteDNSRecord(id); err != nil {
		t.Fatalf("delete dns record: %v", err)
	}

	remaining, err := c.FindDNSRecords(mikrotik.DNSRecord{Name: name})
	if err != nil {
		t.Fatalf("find after delete: %v", err)
	}
	if len(remaining) > 0 {
		t.Fatalf("record still exists after delete: %+v", remaining)
	}
	t.Log("record deleted successfully")
}

func TestListManagedDNSRecords(t *testing.T) {
	c := newIntegrationClient(t)

	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Comment: managedByComment})
	if err != nil {
		t.Fatalf("find managed dns records: %v", err)
	}
	t.Logf("found %d managed record(s)", len(records))
	for _, r := range records {
		t.Logf("  id=%-6s name=%-30s address=%s", r.ID, r.Name, r.Address)
	}
}
