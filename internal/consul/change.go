package consul

import (
	capi "github.com/hashicorp/consul/api"
)

type change interface {
	change(state *State)
}

type serviceChange capi.CatalogNodeServiceList

func (c *serviceChange) change(state *State) {
	services := make([]Service, len(c.Services))
	for i, service := range c.Services {
		tags := make(map[string]bool)
		for _, tag := range service.Tags {
			tags[tag] = true
		}

		services[i] = Service{
			ID:      service.ID,
			Name:    service.Service,
			Address: service.Address,
			Port:    service.Port,
			Tags:    tags,
			Meta:    service.Meta,
		}
	}

	delete(state.Nodes, c.Node.Node)
	state.Nodes[c.Node.Node] = Node{
		ID:       c.Node.ID,
		Name:     c.Node.Node,
		Address:  c.Node.Address,
		Meta:     c.Node.Meta,
		Services: services,
	}
}

type kvChange struct {
	prefix string
	kv     capi.KVPairs
}

func (c kvChange) change(state *State) {
	folder := make(Folder)
	for _, kv := range c.kv {
		key := kv.Key[len(c.prefix):]
		folder[key] = Value(kv.Value)
	}

	state.KV.set(c.prefix, folder)
}
