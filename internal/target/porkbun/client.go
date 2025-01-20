package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

type Client struct {
	httpClient  http.Client
	credentials Credentials
}

func NewClient(credentials Credentials) *Client {
	return &Client{
		httpClient:  http.Client{},
		credentials: credentials,
	}
}

func (c *Client) Ping(ctx context.Context) (PingOut, error) {
	return execute(ctx, c, PingIn{})
}

func (c *Client) RetrieveRecords(ctx context.Context, in RetrieveRecordsIn) (RetrieveRecordsOut, error) {
	return execute(ctx, c, in)
}

func (c *Client) UpdateRecord(ctx context.Context, in UpdateRecordIn) (UpdateRecordOut, error) {
	return execute(ctx, c, in)
}

func (c *Client) DeleteRecord(ctx context.Context, in DeleteRecordIn) (DeleteRecordOut, error) {
	return execute(ctx, c, in)
}

func execute[R any](ctx context.Context, c *Client, in exchange[R]) (none R, _ error) {
	data, err := mergeJSON(in, c.credentials)
	if err != nil {
		return none, errors.Wrap(err, "marshal request body")
	}

	spew.Dump(string(data))

	url := "https://api.porkbun.com/api/json/v3" + in.path()
	spew.Dump(url)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return none, errors.Wrap(err, "create request")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return none, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return none, errors.New("unexpected status code: " + resp.Status)
	}

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return none, errors.Wrap(err, "read response body")
	}

	var status struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(data, &status); err != nil {
		return none, errors.Wrap(err, "unmarshal status response")
	}

	if status.Status != "SUCCESS" {
		return none, errors.New("unexpected status: " + status.Status)
	}

	var result R
	if err := json.Unmarshal(data, &result); err != nil {
		return none, errors.Wrap(err, "unmarshal response")
	}

	return result, nil
}

type exchange[R any] interface {
	path() string
	out() R
}

type PingIn struct{}

func (PingIn) path() string     { return "/ping" }
func (PingIn) out() (_ PingOut) { return }

type PingOut struct {
	YourIP string `json:"yourIp"`
}

type RetrieveRecordsIn struct {
	Domain string `json:"-" validate:"required"`

	Type      string `json:"-"`
	Subdomain string `json:"-"`

	ID string `json:"-"`
}

func (in RetrieveRecordsIn) path() string {
	path := "/dns"
	if in.Type != "" {
		path += "/retrieveByNameType/" + in.Domain + "/" + in.Type
		if in.Subdomain != "" {
			path += "/" + in.Subdomain
		}

		return path
	}

	path += "/retrieve"
	if in.ID != "" {
		path += "/" + in.ID
	}

	return path
}

func (in RetrieveRecordsIn) out() (_ RetrieveRecordsOut) { return }

type Record struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,string"`
	Priority int    `json:"prio,string"`
	Notes    string `json:"notes"`
}

type RetrieveRecordsOut struct {
	Records []Record `json:"records"`
}

type UpdateRecordIn struct {
	Domain   string `json:"-" validate:"required"`
	ID       int64  `json:"-"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type" validate:"required"`
	Content  string `json:"content" validate:"required"`
	TTL      int    `json:"ttl,string,omitempty"`
	Priority int    `json:"prio,string,omitempty"`
}

func (in UpdateRecordIn) path() string {
	path := "/dns"
	if in.ID != 0 {
		path += "/edit/" + in.Domain + "/" + strconv.FormatInt(in.ID, 10)
		return path
	}

	return path + "/create/" + in.Domain
}

func (in UpdateRecordIn) out() (_ UpdateRecordOut) { return }

type UpdateRecordOut struct {
	ID int64 `json:"id,omitempty"`
}

type DeleteRecordIn struct {
	Domain string `json:"-" validate:"required"`
	ID     int64  `json:"-" validate:"required"`
}

func (in DeleteRecordIn) path() string {
	return "/dns/delete/" + in.Domain + "/" + strconv.FormatInt(in.ID, 10)
}

func (in DeleteRecordIn) out() (_ DeleteRecordOut) { return }

type DeleteRecordOut struct{}

func mergeJSON(values ...any) ([]byte, error) {
	result := make(map[string]any)
	for _, value := range values {
		var part map[string]any
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(data, &part); err != nil {
			return nil, err
		}

		for key, value := range part {
			result[key] = value
		}
	}

	return json.Marshal(result)
}
