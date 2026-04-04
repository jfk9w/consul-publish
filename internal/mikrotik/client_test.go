package mikrotik_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/yaml.v3"

	"github.com/jfk9w/consul-publish/internal/mikrotik"
)

// Duration tests

func TestDuration_MarshalJSON(t *testing.T) {
	cases := []struct {
		d    mikrotik.Duration
		want string
	}{
		{mikrotik.Duration(5 * time.Minute), `"5m0s"`},
		{mikrotik.Duration(1 * time.Hour), `"1h0m0s"`},
		{mikrotik.Duration(90 * time.Second), `"1m30s"`},
		{mikrotik.Duration(24 * time.Hour), `"24h0m0s"`},
		{mikrotik.Duration(25*time.Hour + 5*time.Minute), `"25h5m0s"`},
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
		{`"5m0s"`, mikrotik.Duration(5 * time.Minute)},
		{`"1h0m0s"`, mikrotik.Duration(1 * time.Hour)},
		{`"1m30s"`, mikrotik.Duration(90 * time.Second)},
		{`"24h0m0s"`, mikrotik.Duration(24 * time.Hour)},
		{`"25h5m0s"`, mikrotik.Duration(25*time.Hour + 5*time.Minute)},
	}
	for _, tc := range cases {
		var d mikrotik.Duration
		require.NoError(t, json.Unmarshal([]byte(tc.input), &d))
		assert.Equal(t, tc.want, d)
	}
}

func TestDuration_UnmarshalJSON_Invalid(t *testing.T) {
	var d mikrotik.Duration
	assert.Error(t, json.Unmarshal([]byte(`"notaduration"`), &d))
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

func newMockClient(t *testing.T, mockHTTP *MockHTTPClient) *mikrotik.Client {
	t.Helper()
	return mikrotik.New(
		mikrotik.Config{Host: "router.local", User: "admin", Password: "secret"},
		mikrotik.WithHTTPClient(mockHTTP),
	)
}

func makeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClient_CreateDNSRecord(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPut, req.Method)
			assert.Equal(t, "/rest/ip/dns/static", req.URL.Path)
			user, pass, ok := req.BasicAuth()
			require.True(t, ok)
			assert.Equal(t, "admin", user)
			assert.Equal(t, "secret", pass)

			var rec mikrotik.DNSRecord
			require.NoError(t, json.NewDecoder(req.Body).Decode(&rec))
			assert.Equal(t, "test.local", rec.Name)
			assert.Equal(t, "10.0.0.1", rec.Address)

			return makeResponse(http.StatusCreated,
				`{".id":"*1","name":"test.local","address":"10.0.0.1","comment":"consul"}`), nil
		})

	c := newMockClient(t, mockHTTP)
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
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(makeResponse(http.StatusBadRequest, `bad request`), nil)

	c := newMockClient(t, mockHTTP)
	_, err := c.CreateDNSRecord(mikrotik.DNSRecord{Name: "x"})
	assert.Error(t, err)
}

func TestClient_UpdateDNSRecord(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPatch, req.Method)
			assert.Equal(t, "/rest/ip/dns/static/*1", req.URL.Path)

			var rec mikrotik.DNSRecord
			require.NoError(t, json.NewDecoder(req.Body).Decode(&rec))
			assert.Equal(t, "10.0.0.2", rec.Address)

			return makeResponse(http.StatusOK,
				`{".id":"*1","name":"test.local","address":"10.0.0.2","comment":"consul"}`), nil
		})

	c := newMockClient(t, mockHTTP)
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
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, "/rest/ip/dns/static", req.URL.Path)
			assert.Equal(t, "consul", req.URL.Query().Get("comment"))

			return makeResponse(http.StatusOK,
				`[{".id":"*1","name":"a.local","address":"10.0.0.1","comment":"consul"},`+
					`{".id":"*2","name":"b.local","address":"10.0.0.1","comment":"consul"}]`), nil
		})

	c := newMockClient(t, mockHTTP)
	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Comment: "consul"})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "*1", records[0].ID)
	assert.Equal(t, "*2", records[1].ID)
}

func TestClient_FindDNSRecords_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(makeResponse(http.StatusOK, `[]`), nil)

	c := newMockClient(t, mockHTTP)
	records, err := c.FindDNSRecords(mikrotik.DNSRecord{Comment: "consul"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestClient_DeleteDNSRecord(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		DoAndReturn(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodDelete, req.Method)
			assert.Equal(t, "/rest/ip/dns/static/*1", req.URL.Path)
			return makeResponse(http.StatusNoContent, ``), nil
		})

	c := newMockClient(t, mockHTTP)
	require.NoError(t, c.DeleteDNSRecord("*1"))
}

func TestClient_DeleteDNSRecord_ErrorStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHTTP := NewMockHTTPClient(ctrl)

	mockHTTP.EXPECT().
		Do(gomock.Any()).
		Return(makeResponse(http.StatusNotFound, `not found`), nil)

	c := newMockClient(t, mockHTTP)
	assert.Error(t, c.DeleteDNSRecord("*999"))
}
