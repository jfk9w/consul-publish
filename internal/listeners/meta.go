package listeners

import (
	"strings"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/lib"
)

// Service metadata keys used by the listeners.
const (
	DomainNameKey  = "domain-name"   // space-separated DNS names; http:// / https:// prefixes are stripped
	PublishHTTPKey = "publish-http"  // group selector — service is published only when the local node is a member
	PublishPathKey = "publish-path"  // URL path prefix for Caddy reverse-proxy entries
)

// GetDomainName returns the raw value of the domain-name metadata key.
func GetDomainName(meta map[string]string) (string, bool) {
	value, ok := meta[DomainNameKey]
	return value, ok
}

// GetDomainNames returns the list of DNS names from the domain-name metadata key.
// http:// and https:// prefixes are stripped from each entry.
func GetDomainNames(meta map[string]string) []string {
	value, ok := meta[DomainNameKey]
	if !ok {
		return nil
	}

	fields := strings.Fields(value)
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimPrefix(field, "https://")
		field = strings.TrimPrefix(field, "http://")
		names = append(names, field)
	}

	return names
}

// GetHTTPDomainNames returns domain names for HTTP-published services.
// It returns nil when the local node is not a member of the publish-http group.
func GetHTTPDomainNames(state *consul.State, meta map[string]string) []string {
	if !state.InGroup(meta, PublishHTTPKey, state.Self) {
		return nil
	}

	return GetDomainNames(meta)
}

// GetGroups returns the set of group names stored in meta[key].
func GetGroups(meta map[string]string, key string) lib.Set[string] {
	return lib.SetOf(strings.Fields(meta[key])...)
}
