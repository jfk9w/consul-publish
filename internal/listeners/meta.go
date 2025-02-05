package listeners

import (
	"strings"

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

func GetGroups(meta map[string]string, key string) lib.Set[string] {
	return lib.SetOf(strings.Fields(meta[key])...)
}
