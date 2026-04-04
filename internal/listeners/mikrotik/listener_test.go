package mikrotik_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners/mikrotik"
	mtkapi "github.com/jfk9w/consul-publish/internal/mikrotik"
)

const testComment = "consul"

var testTTL = mtkapi.Duration(5 * time.Minute)

func newMockListener(t *testing.T) (*mikrotik.Listener, *mikrotik.MockDNSClient) {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := mikrotik.NewMockDNSClient(ctrl)
	l := mikrotik.NewListenerWithClient(mikrotik.ListenerConfig{
		TTL:     testTTL,
		Comment: testComment,
	}, m)
	return l, m
}

func stateWithServices(selfAddress string, services ...consul.Service) *consul.State {
	return &consul.State{
		Self: "node1",
		Nodes: map[string]consul.Node{
			"node1": {
				ID:       "node1",
				Name:     "node1",
				Address:  selfAddress,
				Services: services,
			},
		},
	}
}

func service(domainName string) consul.Service {
	return consul.Service{
		ID:   domainName,
		Name: domainName,
		Meta: map[string]string{"domain-name": domainName},
	}
}

func recordOf(id, name, address string) mtkapi.DNSRecord {
	return mtkapi.DNSRecord{ID: id, Name: name, Address: address, Comment: testComment}
}

func TestListener_Notify_CreateRecord(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return(nil, nil)
	m.EXPECT().CreateDNSRecord(mtkapi.DNSRecord{
		Name:    "svc.local",
		Address: "10.0.0.1",
		TTL:     testTTL,
		Comment: testComment,
	}).Return(recordOf("*1", "svc.local", "10.0.0.1"), nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("svc.local"))))
}

func TestListener_Notify_NoOp(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).
		Return([]mtkapi.DNSRecord{recordOf("*1", "svc.local", "10.0.0.1")}, nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("svc.local"))))
}

func TestListener_Notify_UpdateRecord(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).
		Return([]mtkapi.DNSRecord{recordOf("*1", "svc.local", "10.0.0.2")}, nil)
	m.EXPECT().UpdateDNSRecord(mtkapi.DNSRecord{
		ID:      "*1",
		Name:    "svc.local",
		Address: "10.0.0.1",
		TTL:     testTTL,
		Comment: testComment,
	}).Return(recordOf("*1", "svc.local", "10.0.0.1"), nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("svc.local"))))
}

func TestListener_Notify_DeleteStaleRecord(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).
		Return([]mtkapi.DNSRecord{recordOf("*1", "stale.local", "10.0.0.1")}, nil)
	m.EXPECT().DeleteDNSRecord("*1").Return(nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1")))
}

func TestListener_Notify_DeleteDuplicates_WithMatch(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return([]mtkapi.DNSRecord{
		recordOf("*1", "svc.local", "10.0.0.2"), // wrong
		recordOf("*2", "svc.local", "10.0.0.1"), // correct — keep
		recordOf("*3", "svc.local", "10.0.0.3"), // wrong
	}, nil)
	m.EXPECT().DeleteDNSRecord("*1").Return(nil)
	m.EXPECT().DeleteDNSRecord("*3").Return(nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("svc.local"))))
}

func TestListener_Notify_DeleteDuplicates_NoMatch(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return([]mtkapi.DNSRecord{
		recordOf("*1", "svc.local", "10.0.0.2"), // kept, updated
		recordOf("*2", "svc.local", "10.0.0.3"), // deleted
	}, nil)
	m.EXPECT().DeleteDNSRecord("*2").Return(nil)
	m.EXPECT().UpdateDNSRecord(mtkapi.DNSRecord{
		ID:      "*1",
		Name:    "svc.local",
		Address: "10.0.0.1",
		TTL:     testTTL,
		Comment: testComment,
	}).Return(recordOf("*1", "svc.local", "10.0.0.1"), nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("svc.local"))))
}

func TestListener_Notify_MultipleServices(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return(nil, nil)
	m.EXPECT().CreateDNSRecord(mtkapi.DNSRecord{Name: "a.local", Address: "10.0.0.1", TTL: testTTL, Comment: testComment}).
		Return(recordOf("*1", "a.local", "10.0.0.1"), nil)
	m.EXPECT().CreateDNSRecord(mtkapi.DNSRecord{Name: "b.local", Address: "10.0.0.1", TTL: testTTL, Comment: testComment}).
		Return(recordOf("*2", "b.local", "10.0.0.1"), nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1", service("a.local"), service("b.local"))))
}

func TestListener_Notify_NoServices(t *testing.T) {
	l, m := newMockListener(t)

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return(nil, nil)

	require.NoError(t, l.Notify(context.Background(), stateWithServices("10.0.0.1")))
}

func TestListener_Notify_IgnoresOtherNodes(t *testing.T) {
	l, m := newMockListener(t)

	state := &consul.State{
		Self: "node1",
		Nodes: map[string]consul.Node{
			"node1": {
				ID:      "node1",
				Name:    "node1",
				Address: "10.0.0.1",
				Services: []consul.Service{
					service("self.local"),
				},
			},
			"node2": {
				ID:      "node2",
				Name:    "node2",
				Address: "10.0.0.2",
				Services: []consul.Service{
					service("other.local"),
				},
			},
		},
	}

	m.EXPECT().FindDNSRecords(mtkapi.DNSRecord{Comment: testComment}).Return(nil, nil)
	m.EXPECT().CreateDNSRecord(mtkapi.DNSRecord{
		Name:    "self.local",
		Address: "10.0.0.1",
		TTL:     testTTL,
		Comment: testComment,
	}).Return(recordOf("*1", "self.local", "10.0.0.1"), nil)
	// other.local from node2 must NOT be created

	require.NoError(t, l.Notify(context.Background(), state))
}
