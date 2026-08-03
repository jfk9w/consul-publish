package hosts

import (
	"slices"
	"testing"

	"github.com/jfk9w/consul-publish/internal/consul"
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

func TestBuildHostsAddsOnlyAliasesUniqueToOneNode(t *testing.T) {
	state := &consul.State{
		Self: "mars",
		Nodes: map[string]consul.Node{
			"mars": {
				ID:      "mars-id",
				Name:    "mars",
				Address: "10.0.0.1",
				Meta: map[string]string{
					"domain-name": "mars.sonc.top http://127.0.0.1:9001",
				},
				Services: []consul.Service{
					{ID: "loki"},
					{ID: "loki"},
					{ID: "alloy"},
					{ID: "logs", Meta: map[string]string{
						"domain-name":  "loki.example.com shared.example.com http://127.0.0.1:9001 https://loki-alt.example.com:9001",
						"publish-http": "all",
					}},
				},
			},
			"venus": {
				ID:      "venus-id",
				Name:    "venus",
				Address: "10.0.0.2",
				Meta: map[string]string{
					"domain-name": "venus.sonc.top",
				},
				Services: []consul.Service{
					{ID: "alloy"},
					{ID: "metrics", Meta: map[string]string{
						"domain-name":  "shared.example.com",
						"publish-http": "venus",
					}},
					{ID: "traces", Meta: map[string]string{
						"domain-name":  "traces.example.com",
						"publish-http": "venus",
					}},
				},
			},
		},
	}

	require.Equal(t, []hostEntry{
		{address: "10.0.0.2", names: []string{"venus", "metrics", "traces", "traces.example.com", "venus.sonc.top"}},
		{address: "127.0.0.1", names: []string{"mars", "alloy", "logs", "loki", "loki-alt.example.com", "loki.example.com", "mars.sonc.top", "shared.example.com"}},
	}, collectHosts(buildHosts(state)))
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
