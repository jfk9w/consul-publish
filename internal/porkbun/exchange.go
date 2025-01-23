package porkbun

import (
	"encoding/json"
)

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

	path += "/retrieve/" + in.Domain
	if in.ID != "" {
		path += "/" + in.ID
	}

	return path
}

func (in RetrieveRecordsIn) out() (_ RetrieveRecordsOut) { return }

type Record struct {
	ID       string `json:"id"`
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
	ID       string `json:"-"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type" validate:"required"`
	Content  string `json:"content" validate:"required"`
	TTL      int    `json:"ttl,string,omitempty"`
	Priority int    `json:"prio,string,omitempty"`
}

func (in UpdateRecordIn) path() string {
	path := "/dns"
	if in.ID != "" {
		path += "/edit/" + in.Domain + "/" + in.ID
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
	ID     string `json:"-" validate:"required"`
}

func (in DeleteRecordIn) path() string {
	return "/dns/delete/" + in.Domain + "/" + in.ID
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
