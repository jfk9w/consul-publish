package listeners

import (
	"github.com/jfk9w/consul-publish/internal/consul"
)

type Domains struct {
	Node    string `yaml:"node"`
	Service string `yaml:"service"`
}

const (
	LocalIP   = "127.0.0.1"
	Localhost = "localhost"
)

func GetLocalAddress(self consul.Node, service consul.Service) string {
	if service.Address == self.Address {
		return LocalIP
	}

	return service.Address
}
