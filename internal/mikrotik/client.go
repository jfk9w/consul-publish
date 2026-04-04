// Package mikrotik provides a REST API client for MikroTik RouterOS.
package mikrotik

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/pkg/errors"
)

// Duration is a time.Duration that marshals to/from standard Go duration strings (e.g. "5m0s")
// in both JSON and YAML.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return errors.Wrap(err, "parse duration")
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return errors.Wrap(err, "parse duration")
	}
	*d = Duration(v)
	return nil
}

// DNSRecord represents a static DNS entry in MikroTik /ip/dns/static.
// The ID field is populated by MikroTik and used for update/delete operations.
type DNSRecord struct {
	ID      string   `json:".id,omitempty"     url:".id,omitempty"`
	Name    string   `json:"name,omitempty"    url:"name,omitempty"`
	Address string   `json:"address,omitempty" url:"address,omitempty"`
	TTL     Duration `json:"ttl,omitempty"     url:"-"`
	Comment string   `json:"comment,omitempty" url:"comment,omitempty"`
}

// Config holds connection parameters for the MikroTik REST API.
type Config struct {
	Host     string `yaml:"host"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Client is a MikroTik REST API client. Use New to construct one.
type Client struct {
	baseURL  string
	user     string
	password string
	http     HTTPClient
}

// Option is a functional option for Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client used by Client.
func WithHTTPClient(h HTTPClient) Option {
	return func(c *Client) { c.http = h }
}

// New creates a Client that talks to the MikroTik REST API at cfg.Host.
func New(cfg Config, opts ...Option) *Client {
	c := &Client{
		baseURL:  "http://" + cfg.Host + "/rest",
		user:     cfg.User,
		password: cfg.Password,
		http:     http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CreateDNSRecord creates a new static DNS record and returns it with the assigned ID.
func (c *Client) CreateDNSRecord(record DNSRecord) (DNSRecord, error) {
	resp, data, err := c.do("PUT", "/ip/dns/static", record)
	if err != nil {
		return DNSRecord{}, err
	}
	if resp.StatusCode != http.StatusCreated {
		return DNSRecord{}, errors.Errorf("expected 201, got %d: %s", resp.StatusCode, data)
	}
	var created DNSRecord
	if err := json.Unmarshal(data, &created); err != nil {
		return DNSRecord{}, errors.Wrap(err, "unmarshal response")
	}
	return created, nil
}

// UpdateDNSRecord updates the DNS record identified by record.ID.
func (c *Client) UpdateDNSRecord(record DNSRecord) (DNSRecord, error) {
	resp, data, err := c.do("PATCH", fmt.Sprintf("/ip/dns/static/%s", record.ID), record)
	if err != nil {
		return DNSRecord{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return DNSRecord{}, errors.Errorf("expected 200, got %d: %s", resp.StatusCode, data)
	}
	var updated DNSRecord
	if err := json.Unmarshal(data, &updated); err != nil {
		return DNSRecord{}, errors.Wrap(err, "unmarshal response")
	}
	return updated, nil
}

// FindDNSRecords returns all static DNS records matching the non-zero fields of filter.
func (c *Client) FindDNSRecords(filter DNSRecord) ([]DNSRecord, error) {
	params, err := query.Values(filter)
	if err != nil {
		return nil, errors.Wrap(err, "encode filter")
	}
	path := "/ip/dns/static"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	resp, data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("expected 200, got %d: %s", resp.StatusCode, data)
	}
	var records []DNSRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, errors.Wrap(err, "unmarshal response")
	}
	return records, nil
}

// DeleteDNSRecord deletes the static DNS record with the given MikroTik ID (e.g. "*1").
func (c *Client) DeleteDNSRecord(id string) error {
	resp, data, err := c.do("DELETE", fmt.Sprintf("/ip/dns/static/%s", id), nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return errors.Errorf("expected 204, got %d: %s", resp.StatusCode, data)
	}
	return nil
}

func (c *Client) do(method, path string, body any) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, errors.Wrap(err, "marshal body")
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, nil, errors.Wrap(err, "new request")
	}
	req.SetBasicAuth(c.user, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "do request")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read body")
	}
	return resp, data, nil
}
