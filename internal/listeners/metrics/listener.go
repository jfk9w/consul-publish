// Package metrics exposes consul-publish state as Prometheus metrics.
package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jfk9w/consul-publish/internal/consul"
)

const shutdownTimeout = 5 * time.Second

// Config holds the HTTP exporter settings.
type Config struct {
	Listen string `yaml:"listen" default:"0.0.0.0:9634" doc:"Prometheus metrics listen address"`
	Path   string `yaml:"path"   default:"/metrics"       doc:"Prometheus metrics HTTP path"`
}

// Listener consumes Consul snapshots and implements prometheus.Collector.
type Listener struct {
	cfg Config

	mu         sync.RWMutex
	groups     []string
	ready      bool
	lastUpdate time.Time

	groupDesc      *prometheus.Desc
	readyDesc      *prometheus.Desc
	lastUpdateDesc *prometheus.Desc
	registry       *prometheus.Registry
}

// New creates an isolated Prometheus exporter and registry.
func New(cfg Config) *Listener {
	l := &Listener{
		cfg: cfg,
		groupDesc: prometheus.NewDesc(
			"consul_publish_host_group_info",
			"Static host group membership from Consul node metadata.",
			[]string{"host_group"}, nil,
		),
		readyDesc: prometheus.NewDesc(
			"consul_publish_consul_state_ready",
			"Whether a valid local Consul node state has been received.",
			nil, nil,
		),
		lastUpdateDesc: prometheus.NewDesc(
			"consul_publish_last_update_timestamp_seconds",
			"Unix timestamp of the last valid local Consul node state update.",
			nil, nil,
		),
		registry: prometheus.NewRegistry(),
	}
	l.registry.MustRegister(l)
	return l
}

func (l *Listener) KV() []string { return nil }

// Notify atomically replaces the exported local-node snapshot.
func (l *Listener) Notify(_ context.Context, state *consul.State) error {
	node, ok := state.Nodes[state.Self]
	if !ok {
		return nil
	}

	groups := make([]string, 0, len(node.Groups))
	for group := range node.Groups {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	l.mu.Lock()
	l.groups = groups
	l.ready = true
	l.lastUpdate = time.Now()
	l.mu.Unlock()
	return nil
}

// Describe implements prometheus.Collector.
func (l *Listener) Describe(ch chan<- *prometheus.Desc) {
	ch <- l.groupDesc
	ch <- l.readyDesc
	ch <- l.lastUpdateDesc
}

// Collect implements prometheus.Collector using a consistent state snapshot.
func (l *Listener) Collect(ch chan<- prometheus.Metric) {
	l.mu.RLock()
	groups := append([]string(nil), l.groups...)
	ready := l.ready
	lastUpdate := l.lastUpdate
	l.mu.RUnlock()

	for _, group := range groups {
		ch <- prometheus.MustNewConstMetric(l.groupDesc, prometheus.GaugeValue, 1, group)
	}
	readyValue := 0.0
	if ready {
		readyValue = 1
	}
	ch <- prometheus.MustNewConstMetric(l.readyDesc, prometheus.GaugeValue, readyValue)
	lastUpdateValue := 0.0
	if !lastUpdate.IsZero() {
		lastUpdateValue = float64(lastUpdate.Unix())
	}
	ch <- prometheus.MustNewConstMetric(l.lastUpdateDesc, prometheus.GaugeValue, lastUpdateValue)
}

// Handler returns an HTTP handler that serves metrics only at the configured path.
func (l *Listener) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(l.cfg.Path, promhttp.HandlerFor(l.registry, promhttp.HandlerOpts{}))
	return mux
}

// ListenAndServe runs the metrics server until ctx is cancelled.
func (l *Listener) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", l.cfg.Listen)
	if err != nil {
		return err
	}
	return l.Serve(ctx, listener)
}

// Serve runs the metrics server on listener and shuts it down gracefully with ctx.
func (l *Listener) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: l.Handler()}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-stopped:
		}
	}()

	err := server.Serve(listener)
	close(stopped)
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return nil
	}
	return err
}
