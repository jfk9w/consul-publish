package api

import (
	"context"

	capi "github.com/hashicorp/consul/api"
)

const Localhost = "127.0.0.1"

type Domain struct {
	Node    string `yaml:"node" doc:"Node domain"`
	Service string `yaml:"service" doc:"Service domain"`
}

type Visibility uint8

const (
	Invisible Visibility = iota
	Private
	Public
)

type Node struct {
	ID         string
	Name       string
	Address    string
	Region     string
	Visibility Visibility
	Services   []Service
}

func NewNode(node *capi.Node) *Node {
	return &Node{
		ID:         node.ID,
		Name:       node.Node,
		Address:    node.Address,
		Region:     node.Meta[regionKey],
		Visibility: getVisibility(node.Meta),
	}
}

type Service struct {
	Name       string
	ID         string
	Address    string
	Port       int
	Domain     string
	Visibility Visibility
}

func NewService(service *capi.AgentService) *Service {
	return &Service{
		Name:       service.Service,
		ID:         service.ID,
		Address:    service.Address,
		Port:       service.Port,
		Domain:     service.Meta[domainKey],
		Visibility: getVisibility(service.Meta),
	}
}

type Target interface {
	Node(ctx context.Context, domain string, local, node *Node) error
	Service(ctx context.Context, domain string, local, node *Node, service *Service) error
	Sync(ctx context.Context, domain Domain) error
}
