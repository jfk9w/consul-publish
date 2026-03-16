package mikrotik

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-querystring/query"
	"github.com/pkg/errors"
)

type DNSRecord struct {
	ID      string `json:".id,omitempty"  url:".id,omitempty"`
	Name    string `json:"name,omitempty"  url:"name,omitempty"`
	Address string `json:"address,omitempty" url:"address,omitempty"`
	TTL     string `json:"ttl,omitempty"   url:"ttl,omitempty"`
	Comment string `json:"comment,omitempty" url:"comment,omitempty"`
}

type Config struct {
	Host     string `yaml:"host"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type Client struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

func New(cfg Config) *Client {
	return &Client{
		baseURL:  "http://" + cfg.Host + "/rest",
		user:     cfg.User,
		password: cfg.Password,
		http:     new(http.Client),
	}
}

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
