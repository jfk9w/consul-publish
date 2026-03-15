package listeners

import (
	"strings"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/lib"
)

const (
	DomainNameKey  = "domain-name"
	PublishHTTPKey = "publish-http"
	PublishPathKey = "publish-path"
)

func GetDomainName(meta map[string]string) (string, bool) {
	value, ok := meta[DomainNameKey]
	return value, ok
}

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

func GetHTTPDomainNames(state *consul.State, meta map[string]string) []string {
	if !state.InGroup(meta, PublishHTTPKey, state.Self) {
		return nil
	}

	return GetDomainNames(meta)
}

func GetGroups(meta map[string]string, key string) lib.Set[string] {
	return lib.SetOf(strings.Fields(meta[key])...)
}
