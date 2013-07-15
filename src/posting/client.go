package posting

import (
	"document"
	"github.com/gorilla/schema"
	"net/rpc"
	"net/url"
	"registry"
)

var decoder = schema.NewDecoder()

// Client provides an endpoint for talking to a number of posting servers.
type Client struct {
	clients  []*rpc.Client
	registry *registry.Registry
	// configs  []registry.PostingConfig
}

type Query struct {
	Start  uint64 `schema:"start"`
	Limit  int    `schema:"limit"`
	Result ListResult
}

type ListResult struct {
	Success   bool   `json:"success"`
	TotalRows uint64 `json:"totalRows"`
	Rows      []Row  `json:"rows"`
}

type Row struct {
	Hash     uint64    `json:"hash"`
	Bytes    int       `json:"bytes"`
	Doctypes []Doctype `json:"doctypes"`
}

type Doctype struct {
	Doctype uint32   `json:"doctype"`
	Docids  []uint32 `json:"docids"`
	Deltas  []uint32 `json:"deltas"`
}

func NewClient(registry *registry.Registry) (*Client, error) {
	p := &Client{
		registry: registry,
	}
	p.clients = make([]*rpc.Client, len(registry.PostingConfigs))
	var err error
	for i, config := range p.registry.PostingConfigs {
		p.clients[i], err = rpc.Dial("tcp", config.Address)
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *Client) Initialise() error {
	done := make(chan *rpc.Call, len(p.registry.PostingConfigs))
	for i, config := range p.registry.PostingConfigs {
		p.clients[i].Go("Posting.Init", config, nil, done)
	}
	for _, _ = range p.registry.PostingConfigs {
		replyCall := <-done
		if replyCall.Error != nil {
			return replyCall.Error
		}
	}
	return nil
}

func (p *Client) Close() {
	for _, c := range p.clients {
		c.Close()
	}
}

// returns one searchmap for each client (merge step is separate)
func (p *Client) Search(d *document.DocumentArg) (*document.SearchGroup, error) {
	result := make(document.SearchGroup, len(p.clients))
	done := make(chan *rpc.Call, len(p.clients))
	for i, _ := range p.clients {
		p.clients[i].Go("Posting.Search", d, &result[i], done)
	}
	for _, _ = range p.clients {
		replyCall := <-done
		if replyCall.Error != nil {
			return nil, replyCall.Error
		}
	}

	return &result, nil
}

// Don't care about the replies, just check the error
// ie add/delete docs
func (p *Client) CallMultiple(service string, args interface{}) error {
	done := make(chan *rpc.Call, len(p.clients))
	for i, _ := range p.clients {
		p.clients[i].Go(service, args, nil, done)
	}
	for _, _ = range p.clients {
		replyCall := <-done
		if replyCall.Error != nil {
			return replyCall.Error
		}
	}
	return nil
}

// page through the index... (for debugging)
func (p *Client) GetRows(values *url.Values) (*ListResult, error) {
	result := Query{
		Start: 0,
		Limit: 100,
	}
	decoder.Decode(&result, *values)
	for i, _ := range p.clients {
		if err := p.clients[i].Call("Posting.List", result, &result); err != nil {
			return nil, err
		}
		if len(result.Result.Rows) >= result.Limit {
			break
		}
	}
	return &result.Result, nil
}
