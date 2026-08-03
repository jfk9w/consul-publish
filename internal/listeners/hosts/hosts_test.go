package hosts

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIterCanonicalNamesFirst(t *testing.T) {
	hosts := make(hosts)
	hosts.add("127.0.0.1", "web.example.com")
	hosts.addCanonical("127.0.0.1", "server")
	hosts.add("127.0.0.1", "api.example.com")
	hosts.addCanonical("127.0.0.1", "server")
	hosts.add("127.0.0.1", "server")

	require.Equal(t, []hostEntry{
		{address: "127.0.0.1", names: []string{"server", "api.example.com", "web.example.com"}},
	}, collectHosts(hosts))
}

func TestIterSortsAddresses(t *testing.T) {
	hosts := make(hosts)
	hosts.addCanonical("10.0.0.2", "server-b")
	hosts.addCanonical("10.0.0.1", "server-a")
	hosts.add("10.0.0.1", "alias")

	require.Equal(t, []hostEntry{
		{address: "10.0.0.1", names: []string{"server-a", "alias"}},
		{address: "10.0.0.2", names: []string{"server-b"}},
	}, collectHosts(hosts))
}

type hostEntry struct {
	address string
	names   []string
}

func collectHosts(hosts hosts) []hostEntry {
	return slices.Collect(func(yield func(hostEntry) bool) {
		for address, names := range hosts.iter() {
			if !yield(hostEntry{address: address, names: names}) {
				return
			}
		}
	})
}
