# consul-publish

Go application that monitors HashiCorp Consul for service and node changes, then automatically publishes/synchronizes this information to multiple output targets (hosts file, Caddy web server config).

## Build & Run

```bash
make bin        # Build binary to bin/consul-publish
make install    # Copy to /usr/local/bin/
make fmt        # Format code with goimports
make test       # Run tests
make clean      # Remove bin/
```

```bash
./bin/consul-publish --config.file=config.yml
./bin/consul-publish --dump.schema   # Print config schema
./bin/consul-publish --dump.values   # Print default values
./bin/consul-publish --once          # Run once and exit
```

## Architecture

```
cmd/consul-publish/main.go       # Entry point, config init, shutdown signals
internal/consul/
  watcher.go                     # Main watch loop, Consul blocking queries, 5s debounce
  consul.go                      # State, Node, Service, KV data structures
  change.go                      # Change event types
  api.go                         # Listener interface
internal/listeners/
  hosts/listener.go              # Writes /etc/hosts with node/service DNS mappings
  caddy/listener.go              # Generates Caddy reverse proxy config from KV templates
  file.go                        # Atomic file writes with SHA256 change detection
config/
  defaults.yaml                  # Default config values
  schema.yaml                    # JSON Schema for config validation
```

## Key Concepts

**Listener interface** — `KV() []string` (watched KV prefixes) + `Notify(state)` (called on change)

**State change flow:**
1. Watcher polls Consul nodes, services, and KV prefixes via blocking queries
2. Changes are debounced 5 seconds, then compared with deep equality
3. If state changed, all listeners are notified with a deep copy of state

**Caddy listener** — reads service definitions from Consul KV (e.g. `caddy/myservice`), renders Go templates with `[[`/`]]` delimiters, supports Authelia forward auth via `ForwardAuth` template function

**Hosts listener** — maps each node to its IP, local node to 127.0.0.1, uses `domain-name` metadata if present

**File writing** — atomic (temp + rename), skips write if SHA256 unchanged, supports custom mode/owner/group

## Service Metadata Keys

- `domain-name` — custom DNS name for a service/node
- `publish-http` — mark service for HTTP publishing in Caddy
- `publish-path` — URL path prefix for the service

## Configuration

Required fields:
- `token` — Consul authentication token
- `domain.node` / `domain.service` — domain templates

Optional:
- `address` — Consul address (default: `127.0.0.1:8500`)
- `interval` — watch polling interval (default: `1m0s`)
- `hosts.enabled` — enable hosts file listener
- `caddy.enabled` — enable Caddy config listener
