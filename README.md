# consul-publish

A Go daemon that watches HashiCorp Consul for service and node changes, synchronises this information to multiple targets, and exposes local node group membership as Prometheus metrics.

## How it works

1. A watcher polls Consul nodes, services, and KV prefixes via blocking queries.
2. Changes are debounced for 5 seconds and compared with the previous state using deep equality.
3. If the state changed, every enabled listener is notified with a snapshot of the new state.

## Targets

### Hosts

Writes `/etc/hosts` (or a custom path) based on the current Consul node and service inventory:

- Each node is mapped to its IP address.
- The local node is mapped to `127.0.0.1`.
- Unique `domain-name` values from node metadata are added as aliases of their nodes.
- Service `domain-name` values are added as aliases when each domain occurs on exactly one node; remote aliases do not depend on whether the local node matches `publish-http`.
- For the local node, all published domain names are added to `127.0.0.1` regardless of uniqueness.
- IP addresses in `domain-name` are not written as aliases; ports are stripped from domain aliases.

### Caddy

Generates a Caddy reverse-proxy configuration from service definitions stored in Consul KV. Each KV value is a Go template rendered with `[[` / `]]` delimiters. The `ForwardAuth` template function adds Authelia-compatible forward-auth blocks. After a write, an optional shell command (e.g. `caddy reload`) is executed.

### Homepage

Generates Homepage's `services.yaml` from service templates stored in Consul KV. A service is included when the local node belongs to one of the groups selected by `publish-homepage` and its `homepage-path` metadata contains a placement in the form `<group>/<service-name>`. Spaces are allowed inside both path elements, and surrounding spaces are ignored. The KV key is the Consul service ID; its value is a Go template rendered with `[[` / `]]` delimiters and the service instances as its data. After the file changes, an optional shell command is executed to reload Homepage.

### MikroTik

Manages static DNS records in a MikroTik router via its REST API. On every state change the listener reconciles the desired set of records (derived from services that have a `domain-name` metadata key) with the records already present in MikroTik:

- Records that are missing are **created**.
- Records whose address has changed are **updated** in-place.
- Records that are no longer present in Consul are **deleted**.
- Duplicate records for the same domain are pruned, keeping the one with the correct address.

Ownership is tracked through a configurable comment field (default: `consul`). Only records carrying that comment are ever touched.

### Prometheus metrics

The built-in HTTP exporter publishes only the local node's Consul metadata groups. For example, `groups = "home mariadb"` produces:

```text
consul_publish_host_group_info{host_group="home"} 1
consul_publish_host_group_info{host_group="mariadb"} 1
```

The exporter also publishes `consul_publish_consul_state_ready` and `consul_publish_last_update_timestamp_seconds`. It intentionally omits host, country, job, and instance labels; Prometheus adds target labels during scraping.

## Service metadata keys

| Key | Used by | Description |
|-----|---------|-------------|
| `domain-name` | hosts, mikrotik | Space-separated list of DNS names for the service. `http://` / `https://` prefixes are stripped automatically. |
| `homepage-path` | homepage | Placement in the form `<group>/<service-name>`; spaces are allowed and surrounding spaces are ignored. Omit to hide the service from Homepage. |
| `publish-http` | hosts, caddy | Group selector — the service is published only when the local node is a member of the named group. |
| `publish-homepage` | homepage | Group selector — the service is added only when the local node is a member of one of the named groups. |
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
  common: |                # Added to every generated Caddy site block
    header >Alt-Svc `h3=":443"; ma=2592000`
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

homepage:
  enabled: true
  kv: homepage             # Consul KV prefix; each key is a service ID
  exec: docker kill --signal SIGHUP homepage
  services:
    path: /app/config/services.yaml
    mode: 0644
    user: root
    group: root

# Consul KV homepage/grafana:
# href: https://grafana.example.com
# widget:
#   type: grafana

# Consul service metadata:
# homepage-path: "Home Automation/Home Assistant"
# publish-homepage: "home"

mikrotik:
  enabled: true
  host: 192.168.88.1       # MikroTik address (host:port or bare host)
  user: admin
  password: "<password>"
  ttl: 5m                  # DNS record TTL
  comment: consul          # ownership tag — only records with this comment are managed

metrics:
  enabled: true
  listen: 0.0.0.0:9634
  path: /metrics
```

One possible Consul service registration for Prometheus Consul service discovery is:

```hcl
services {
  name    = "consul-publish"
  address = "<node-address>"
  port    = 9634

  tags = ["prom"]

  meta {
    metrics-path = "/metrics"
  }
}
```

This matches the existing infrastructure convention: the `prom` service tag and the `metrics-path` Consul metadata key (exposed to Prometheus relabelling as `__meta_consul_service_metadata_metrics_path`). Adjust the address to the node interface reachable by Prometheus.

The full JSON Schema is available at [`config/schema.json`](config/schema.json).
