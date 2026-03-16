package mikrotik_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/jfk9w/consul-publish/internal/mikrotik"
)

// Duration tests

func TestDuration_MarshalJSON(t *testing.T) {
	cases := []struct {
		d    mikrotik.Duration
		want string
	}{
		{mikrotik.Duration(5 * time.Minute), `"00:05:00"`},
		{mikrotik.Duration(1 * time.Hour), `"01:00:00"`},
		{mikrotik.Duration(90 * time.Second), `"00:01:30"`},
		{mikrotik.Duration(24 * time.Hour), `"1d00:00:00"`},
		{mikrotik.Duration(25*time.Hour + 5*time.Minute), `"1d01:05:00"`},
	}
	for _, tc := range cases {
		data, err := json.Marshal(tc.d)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(data))
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		input string
		want  mikrotik.Duration
	}{
		{`"00:05:00"`, mikrotik.Duration(5 * time.Minute)},
		{`"01:00:00"`, mikrotik.Duration(1 * time.Hour)},
		{`"00:01:30"`, mikrotik.Duration(90 * time.Second)},
		{`"1d00:00:00"`, mikrotik.Duration(24 * time.Hour)},
		{`"1d01:05:00"`, mikrotik.Duration(25*time.Hour + 5*time.Minute)},
	}
	for _, tc := range cases {
		var d mikrotik.Duration
		require.NoError(t, json.Unmarshal([]byte(tc.input), &d))
		assert.Equal(t, tc.want, d)
	}
}

func TestDuration_UnmarshalJSON_Invalid(t *testing.T) {
	var d mikrotik.Duration
	assert.Error(t, json.Unmarshal([]byte(`"5m"`), &d))
}

func TestDuration_MarshalYAML(t *testing.T) {
	type wrapper struct {
		TTL mikrotik.Duration `yaml:"ttl"`
	}
	data, err := yaml.Marshal(wrapper{TTL: mikrotik.Duration(5 * time.Minute)})
	require.NoError(t, err)
	var got wrapper
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, mikrotik.Duration(5*time.Minute), got.TTL)
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	cases := []struct {
		input string
		want  mikrotik.Duration
	}{
		{"5m", mikrotik.Duration(5 * time.Minute)},
		{"1h30m", mikrotik.Duration(90 * time.Minute)},
		{"300s", mikrotik.Duration(5 * time.Minute)},
	}
	for _, tc := range cases {
		type wrapper struct {
			TTL mikrotik.Duration `yaml:"ttl"`
		}
		var w wrapper
		require.NoError(t, yaml.Unmarshal([]byte("ttl: "+tc.input), &w))
		assert.Equal(t, tc.want, w.TTL)
	}
}

// Client tests

func newTestClient(t *testing.T, handler http.Handler) *mikrotik.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return mikrotik.New(mikrotik.Config{
		Host:     srv.Listener.Addr().String(),
		User:     "admin",
		Password: "secret",
	})
}

func checkBasicAuth(t *testing.T, r *http.Request) {
	t.Helper()
	user, pass, ok := r.BasicAuth()
	require.True(t, ok, "missing basic auth")
	assert.Equal(t, "admin", user)
	assert.Equal(t, "secret", pass)
}

func TestClient_CreateDNSRecord(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBasicAuth(t, r)
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/rest/ip/dns/static", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var rec mikrotik.DNSRecord
		require.NoError(t, json.Unmarshal(body, &rec))
		assert.Equal(t, "test.local", rec.Name)
		assert.Equal(t, "10.0.0.1", rec.Address)

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{".id":"*1","name":"test.local","address":"10.0.0.1","comment":"consul"}`))
	}))

	got, err := c.CreateDNSRecord(mikrotik.DNSRecord{
		Name:    "test.local",
		Address: "10.0.0.1",
		TTL:     mikrotik.Duration(5 * time.Minute),
		Comment: "consul",
	})
	require.NoError(t, err)
	assert.Equal(t, "*1", got.ID)
}

func TestClient_CreateDNSRecord_ErrorStatus(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`bad request`))
	}))
	_, err := c.CreateDNSRecord(mikrotik.DNSRecord{Name: "x"})
	assert.Error(t, err)
}

func TestClient_UpdateDNSRecord(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBasicAuth(t, r)
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/rest/ip/dns/static/*1", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var rec mikrotik.DNSRecord
		require.NoError(t, json.Unmarshal(body, &rec))
		assert.Equal(t, "10.0.0.2", rec.Address)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{".id":"*1","name":"test.local","address":"10.0.0.2","comment":"consul"}`))
	}))

	got, err := c.UpdateDNSRecord(mikrotik.DNSRecord{
		ID:      "*1",
		Name:    "test.local",
		Address: "10.0.0.2",
		Comment: "consul",
	})
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", got.Address)
}

func TestClient_FindDNSRecords(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBasicAuth(t, r)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/ip/dns/static", r.URL.Path)
		assert.Equal(t, "consul", r.URL.Query().Get("comment"))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{".id":"*1","name":"a.local","address":"10.0.0.1","comment":"consul"},{".id":"*2","name":"b.local","address":"10.0.0.1","comment":"consul"}]`))
	}))

	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Comment: "consul"})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "*1", records[0].ID)
	assert.Equal(t, "*2", records[1].ID)
}

func TestClient_FindDNSRecords_Empty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Comment: "consul"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestClient_DeleteDNSRecord(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBasicAuth(t, r)
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/rest/ip/dns/static/*1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, c.DeleteDNSRecord("*1"))
}

func TestClient_DeleteDNSRecord_ErrorStatus(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	assert.Error(t, c.DeleteDNSRecord("*999"))
}
