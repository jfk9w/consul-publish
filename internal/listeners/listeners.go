// Package listeners provides shared types and helpers used by all listener implementations.
package listeners

import (
	"github.com/jfk9w/consul-publish/internal/consul"
)

// Domains holds Go templates for deriving DNS names from node and service names.
type Domains struct {
	Node    string `yaml:"node"`
	Service string `yaml:"service"`
}

const (
	LocalIP   = "127.0.0.1"
	Localhost = "localhost"
)

// GetLocalAddress returns LocalIP when the service runs on the same host as self,
// or the service's own address otherwise.
func GetLocalAddress(self consul.Node, service consul.Service) string {
	if service.Address == self.Address {
		return LocalIP
	}

	return service.Address
}
