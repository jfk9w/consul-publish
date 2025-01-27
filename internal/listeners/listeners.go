package listeners

type Domains struct {
	Node    string `yaml:"node"`
	Service string `yaml:"service"`
}

const (
	LocalIP   = "127.0.0.1"
	Localhost = "localhost"
)
