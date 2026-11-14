package minio

import "sync"

// BaseClientPool holds one base client per distinct connection configuration.
// Each repository is scoped from the base client for its own endpoint and
// credentials, so repositories on different storage are never served through
// another repository's connection. Equal configurations share one client.
type BaseClientPool struct {
	mu      sync.Mutex
	clients map[BaseClientConfig]*Client
}

// NewBaseClientPool creates an empty pool.
func NewBaseClientPool() *BaseClientPool {
	return &BaseClientPool{clients: make(map[BaseClientConfig]*Client)}
}

// Get returns the base client for cfg, creating it on first use. created
// reports whether this call created a new connection.
func (p *BaseClientPool) Get(cfg BaseClientConfig) (client *Client, created bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.clients[cfg]; ok {
		return existing, false, nil
	}
	client, err = NewBaseClient(cfg)
	if err != nil {
		return nil, false, err
	}
	p.clients[cfg] = client
	return client, true, nil
}
