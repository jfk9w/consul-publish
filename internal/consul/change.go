package consul

import (
	"strings"

	capi "github.com/hashicorp/consul/api"

	"github.com/jfk9w/consul-publish/internal/lib"
)

type change interface {
	change(state *State)
}

type nodeChange []*capi.Node

func (c nodeChange) change(state *State) {
	for _, node := range c {
		entry := state.Nodes[node.Node]
		entry.ID = node.ID
		entry.Name = node.Node
		entry.Address = node.Address
		entry.Meta = node.Meta
		entry.Groups = lib.SetOf(strings.Fields(node.Meta[NodeGroupsKey])...)
		state.Nodes[node.Node] = entry
	}

	state.groups = nil
}

type nodeDelete string

func (c nodeDelete) change(state *State) {
	delete(state.Nodes, string(c))
	state.groups = nil
}

type serviceChange capi.CatalogNodeServiceList

func (c *serviceChange) change(state *State) {
	services := make([]Service, len(c.Services))
	for i, service := range c.Services {
		tags := make(map[string]bool)
		for _, tag := range service.Tags {
			tags[tag] = true
		}

		address := service.Address
		if address == "" {
			address = c.Node.Address
		}

		services[i] = Service{
			ID:      service.ID,
			Name:    service.Service,
			Address: address,
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
		Groups:   lib.SetOf(strings.Fields(c.Node.Meta[NodeGroupsKey])...),
		Meta:     c.Node.Meta,
		Services: services,
	}

	state.groups = nil
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
