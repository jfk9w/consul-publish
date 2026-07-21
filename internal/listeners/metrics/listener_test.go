package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/lib"
)

func TestListenerExportsOnlyLocalGroups(t *testing.T) {
	l := New(Config{Path: "/metrics"})
	require.NoError(t, l.Notify(t.Context(), state("self", "mariadb", "home")))

	body := scrape(t, l, "/metrics")
	assert.Contains(t, body, "# HELP consul_publish_host_group_info Static host group membership from Consul node metadata.")
	assert.Contains(t, body, "# TYPE consul_publish_host_group_info gauge")
	assert.Contains(t, body, `consul_publish_host_group_info{host_group="home"} 1`)
	assert.Contains(t, body, `consul_publish_host_group_info{host_group="mariadb"} 1`)
	assert.NotContains(t, body, "remote-group")
	assert.NotContains(t, body, "host_name")
}

func TestListenerOutputIsStableAndRemovesGroups(t *testing.T) {
	l := New(Config{Path: "/metrics"})
	require.NoError(t, l.Notify(t.Context(), state("self", "zeta", "alpha")))
	first := scrape(t, l, "/metrics")
	second := scrape(t, l, "/metrics")
	assert.Equal(t, first, second)
	assert.Less(t, strings.Index(first, `host_group="alpha"`), strings.Index(first, `host_group="zeta"`))

	require.NoError(t, l.Notify(t.Context(), state("self", "alpha")))
	updated := scrape(t, l, "/metrics")
	assert.Contains(t, updated, `host_group="alpha"`)
	assert.NotContains(t, updated, `host_group="zeta"`)
}

func TestListenerReadiness(t *testing.T) {
	l := New(Config{Path: "/metrics"})
	before := scrape(t, l, "/metrics")
	assert.Contains(t, before, "consul_publish_consul_state_ready 0")
	assert.Contains(t, before, "consul_publish_last_update_timestamp_seconds 0")

	missingSelf := &consul.State{Self: "self", Nodes: map[string]consul.Node{}}
	require.NoError(t, l.Notify(t.Context(), missingSelf))
	assert.Contains(t, scrape(t, l, "/metrics"), "consul_publish_consul_state_ready 0")

	require.NoError(t, l.Notify(t.Context(), state("self")))
	after := scrape(t, l, "/metrics")
	assert.Contains(t, after, "consul_publish_consul_state_ready 1")
	assert.NotContains(t, after, "consul_publish_last_update_timestamp_seconds 0")
}

func TestHandlerUsesConfiguredPath(t *testing.T) {
	l := New(Config{Path: "/custom"})
	recorder := httptest.NewRecorder()
	l.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = httptest.NewRecorder()
	l.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/custom", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "consul_publish_consul_state_ready")
}

func TestServeStopsWithContext(t *testing.T) {
	l := New(Config{Path: "/metrics"})
	listener := newBlockingListener()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, listener) }()

	select {
	case <-listener.accepted:
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not start")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not stop")
	}
}

func TestListenAndServeReturnsBindError(t *testing.T) {
	l := New(Config{Listen: "127.0.0.1:invalid", Path: "/metrics"})
	err := l.ListenAndServe(t.Context())
	require.Error(t, err)
}

type blockingListener struct {
	accepted chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{accepted: make(chan struct{}), closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepted) })
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *blockingListener) Addr() net.Addr { return testAddr("metrics") }

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }

func TestConcurrentNotifyAndCollect(t *testing.T) {
	l := New(Config{Path: "/metrics"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			require.NoError(t, l.Notify(t.Context(), state("self", "home", "database")))
		}()
		go func() {
			defer wg.Done()
			response := httptest.NewRecorder()
			l.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			assert.Equal(t, http.StatusOK, response.Code)
		}()
	}
	wg.Wait()
}

func state(self string, groups ...string) *consul.State {
	return &consul.State{
		Self: self,
		Nodes: map[string]consul.Node{
			self: {
				Name:   self,
				Groups: lib.SetOf(groups...),
			},
			"remote": {
				Name:   "remote",
				Groups: lib.SetOf("remote-group"),
			},
		},
	}
}

func scrape(t *testing.T, listener *Listener, path string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	listener.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	body, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	return string(body)
}
