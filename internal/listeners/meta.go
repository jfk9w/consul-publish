package listeners

import "github.com/jfk9w/consul-publish/internal/consul"

type Visibility string

const (
	Local  Visibility = "local"
	Public Visibility = "public"
)

const (
	NodeRegionKey       = "region"
	NodeVisibilityKey   = "visibility"
	DomainNameKey       = "domain-name"
	DomainVisibilityKey = "domain-visibility"
)

func GetRegion(node consul.Node) string {
	return node.Meta[NodeRegionKey]
}

func GetVisibility(node consul.Node) Visibility {
	return Visibility(node.Meta[NodeVisibilityKey])
}

func GetDomainName(service consul.Service) (string, bool) {
	value, ok := service.Meta[DomainNameKey]
	return value, ok
}

func GetDomainVisibility(service consul.Service) Visibility {
	return Visibility(service.Meta[DomainVisibilityKey])
}
