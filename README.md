# consul-publish

A Go daemon that watches HashiCorp Consul for service and node changes and synchronises this information to multiple targets: the system hosts file, a Caddy reverse-proxy configuration, and MikroTik static DNS records.

## How it works

1. A watcher polls Consul nodes, services, and KV prefixes via blocking queries.
2. Changes are debounced for 5 seconds and compared with the previous state using deep equality.
3. If the state changed, every enabled listener is notified with a snapshot of the new state.

## Targets

### Hosts

Writes `/etc/hosts` (or a custom path) based on the current Consul node and service inventory:

- Each node is mapped to its IP address.
- The local node is mapped to `127.0.0.1`.
- Services that carry a `domain-name` metadata key and are published via `publish-http` get an additional `127.0.0.1` entry for each domain name.

### Caddy

Generates a Caddy reverse-proxy configuration from service definitions stored in Consul KV. Each KV value is a Go template rendered with `[[` / `]]` delimiters. The `ForwardAuth` template function adds Authelia-compatible forward-auth blocks. After a write, an optional shell command (e.g. `caddy reload`) is executed.

### MikroTik

Manages static DNS records in a MikroTik router via its REST API. On every state change the listener reconciles the desired set of records (derived from services that have a `domain-name` metadata key) with the records already present in MikroTik:

- Records that are missing are **created**.
- Records whose address has changed are **updated** in-place.
- Records that are no longer present in Consul are **deleted**.
- Duplicate records for the same domain are pruned, keeping the one with the correct address.

Ownership is tracked through a configurable comment field (default: `consul`). Only records carrying that comment are ever touched.

## Service metadata keys

| Key | Used by | Description |
|-----|---------|-------------|
| `domain-name` | hosts, mikrotik | Space-separated list of DNS names for the service. `http://` / `https://` prefixes are stripped automatically. |
| `publish-http` | hosts, caddy | Group selector — the service is published only when the local node is a member of the named group. |
| `publish-path` | caddy | URL path prefix for the service. |

## Build & install

```bash
mise run generate   # run go generate (regenerate mocks, schemas)
mise run build      # build → bin/consul-publish
mise run test       # run tests
```

## Usage

```bash
consul-publish --config.file=config.yml

consul-publish --dump.schema   # print JSON config schema to stdout
consul-publish --dump.values   # print current config values to stdout
```

## Configuration

A minimal configuration requires only a Consul token:

```yaml
token: "<consul-acl-token>"
```

Full example with all targets enabled:

```yaml
address: 127.0.0.1:8500   # Consul address
token: "<consul-acl-token>"

hosts:
  enabled: true
  path: /etc/hosts
  mode: 0644
  user: root
  group: root

caddy:
  enabled: true
  kv: caddy                # Consul KV prefix that holds service templates
  exec: caddy reload       # command to run after config changes
  service:
    path: /etc/caddy/services.conf
    mode: 0644
    user: root
    group: root
  node:
    path: /etc/caddy/nodes.conf
    mode: 0644
    user: root
    group: root

mikrotik:
  enabled: true
  host: 192.168.88.1       # MikroTik address (host:port or bare host)
  user: admin
  password: "<password>"
  ttl: 5m                  # DNS record TTL
  comment: consul          # ownership tag — only records with this comment are managed
```

The full JSON Schema is available at [`config/schema.json`](config/schema.json).
