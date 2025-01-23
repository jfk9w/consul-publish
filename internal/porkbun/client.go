package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/pkg/errors"
)

type Credentials struct {
	Key    string `json:"apikey" yaml:"key"`
	Secret string `json:"secretapikey" yaml:"secret"`
}

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

	url := "https://api.porkbun.com/api/json/v3" + in.path()
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

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return none, errors.Wrap(err, "read response body")
	}

	if resp.StatusCode != http.StatusOK {
		return none, errors.New("unexpected status code: " + resp.Status)
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
